// 文件名：ai_action_session.go
// 主要功能：会话级目标引用、参数校验和行为授权；模型不能提交原始票据或任意 core 路径。
package dashboard

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/protobuf/encoding/protojson"
)

type actionRef struct {
	Kind, ID, Target, Name string
	Owned                  bool
	Ticket                 *contact.Contact
	Request                *FriendRequest
}
type actionSession struct {
	s                 *Server
	task              string
	q                 ai.Request
	actor             string
	refs              map[string]actionRef
	sentCurrent       bool
	verifiedOperation bool
	writeStarted      bool
	pending           bool
}

func newActionSession(s *Server, task string, q ai.Request) *actionSession {
	actor := ""
	if q.Conversation != nil {
		actor = q.Conversation.Current.SenderID
	}
	a := &actionSession{s: s, task: task, q: q, actor: actor, refs: map[string]actionRef{}}
	kind := "contact"
	if isActionGroup(q.Target) {
		kind = "group"
	}
	a.refs["current"] = actionRef{Kind: kind, ID: q.Target}
	return a
}
func isActionGroup(id string) bool {
	return strings.HasSuffix(id, "@chatroom") || strings.HasSuffix(id, "@im.chatroom")
}
func actionPolicy(st State, op string) BehaviorPolicy {
	for _, p := range st.Policies {
		if p.Operation == op {
			return p
		}
	}
	return BehaviorPolicy{}
}
func actionActorAllowed(st State, p BehaviorPolicy, actor string, legacy bool) bool {
	if len(p.Actors) > 0 {
		return actor != "" && slices.Contains(p.Actors, actor)
	}
	return legacy || actor != "" && actor == st.Account.ID
}
func actionAvailable(st State, cfg control.Config, p BehaviorPolicy) bool {
	parts := strings.Split(p.Operation, ".")
	return len(parts) == 2 && slices.Contains(cfg.Capabilities[parts[0]].Enabled, parts[1]) && p.Enabled
}
func (s *Server) behaviorToolInventory(q ai.Request) []ai.Tool {
	st := s.store.snapshot()
	actor := ""
	if q.Conversation != nil {
		actor = q.Conversation.Current.SenderID
	}
	out := []ai.Tool{}
	extended := false
	for _, p := range st.Policies {
		meta, ok := control.AIActions[p.Operation]
		if !ok || !actionAvailable(st, s.cfg, p) || !actionActorAllowed(st, p, actor, meta.Legacy) {
			continue
		}
		if p.Operation == "message.Send" && len(p.Actors) == 0 && actor != st.Account.ID {
			continue
		}
		target := q.Target
		if meta.Scope == "self" {
			target = st.Account.ID
		}
		if behaviorAllowed(st, p.Operation, target) != nil && !p.AllowCross {
			continue
		}
		if !meta.Legacy {
			extended = true
		}
		out = append(out, ai.Tool{Name: strings.ReplaceAll(p.Operation, ".", "__"), Description: p.Operation + "。" + p.Prompt, Parameters: meta.Parameters})
	}
	if extended {
		out = append(out, ai.Tool{Name: "context__Find", Description: "查找本会话消息、媒体和本任务操作记录。通讯录及待处理申请仍须对应能力授权。目标不明确先查找，同名需询问。", Parameters: map[string]any{"type": "object", "properties": map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"messages", "media", "contacts", "requests", "operations"}}, "query": map[string]any{"type": "string", "maxLength": 200}, "offset": map[string]any{"type": "integer", "minimum": 0, "maximum": 100000}}, "required": []string{"kind"}, "additionalProperties": false}})
	}
	slices.SortFunc(out, func(a, b ai.Tool) int { return strings.Compare(a.Name, b.Name) })
	return out
}
func (a *actionSession) put(r actionRef) string {
	for id, old := range a.refs {
		if old.Kind == r.Kind && old.ID == r.ID && old.Target == r.Target {
			old.Owned = old.Owned || r.Owned
			if r.Ticket != nil {
				old.Ticket = r.Ticket
			}
			if r.Request != nil {
				old.Request = r.Request
			}
			if r.Name != "" {
				old.Name = r.Name
			}
			a.refs[id] = old
			return id
		}
	}
	id := "ref_" + randomID()
	a.refs[id] = r
	return id
}

type actionReferenceError struct{}

func (actionReferenceError) Error() string { return "引用无效或类型不匹配，请先查询" }

func (a *actionSession) get(id string, kinds ...string) (actionRef, error) {
	r, ok := a.refs[id]
	if !ok || !slices.Contains(kinds, r.Kind) {
		return r, actionReferenceError{}
	}
	return r, nil
}
func validateActionArgs(raw string, schema map[string]any) (map[string]any, error) {
	if len(raw) > 32<<10 {
		return nil, fmt.Errorf("工具参数过长")
	}
	var args map[string]any
	dec := json.NewDecoder(strings.NewReader(raw))
	if dec.Decode(&args) != nil || args == nil {
		return nil, fmt.Errorf("工具参数必须是对象")
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("工具参数存在多余内容")
	}
	props := schema["properties"].(map[string]any)
	req, _ := schema["required"].([]string)
	for _, key := range req {
		if _, ok := args[key]; !ok {
			return nil, fmt.Errorf("缺少参数 %s", key)
		}
	}
	for key, v := range args {
		def, ok := props[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("未知参数 %s", key)
		}
		switch def["type"] {
		case "string":
			val, ok := v.(string)
			if !ok || len(val) > 8000 {
				return nil, fmt.Errorf("参数 %s 无效", key)
			}
			if enum, ok := def["enum"].([]string); ok && !slices.Contains(enum, val) {
				return nil, fmt.Errorf("参数 %s 不在允许值内", key)
			}
		case "array":
			vs, ok := v.([]any)
			if !ok || len(vs) > 50 {
				return nil, fmt.Errorf("参数 %s 数量无效", key)
			}
			seen := map[string]bool{}
			for _, v := range vs {
				str, ok := v.(string)
				if !ok || str == "" || len(str) > 256 || seen[str] {
					return nil, fmt.Errorf("参数 %s 引用无效或重复", key)
				}
				seen[str] = true
			}
		case "boolean":
			if _, ok := v.(bool); !ok {
				return nil, fmt.Errorf("参数 %s 不是布尔值", key)
			}
		case "integer", "number":
			n, ok := v.(float64)
			if !ok || def["type"] == "integer" && n != float64(int(n)) {
				return nil, fmt.Errorf("参数 %s 数值无效", key)
			}
			for _, bound := range []string{"minimum", "maximum"} {
				if v, ok := def[bound]; ok {
					raw, _ := json.Marshal(v)
					var limit float64
					_ = json.Unmarshal(raw, &limit)
					if bound == "minimum" && n < limit || bound == "maximum" && n > limit {
						return nil, fmt.Errorf("参数 %s 超出范围", key)
					}
				}
			}
		}
	}
	return args, nil
}
func argText(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}
func argRefs(args map[string]any, key string) []string {
	out := []string{}
	for _, v := range arrayArg(args, key) {
		out = append(out, v.(string))
	}
	return out
}
func arrayArg(args map[string]any, key string) []any { v, _ := args[key].([]any); return v }
func (a *actionSession) authorize(op, target string) error {
	st := a.s.store.snapshot()
	meta, ok := control.AIActions[op]
	if !ok {
		return fmt.Errorf("操作未开放")
	}
	p := actionPolicy(st, op)
	if !actionAvailable(st, a.s.cfg, p) || !pluginEnabled(st, "ai") || a.s.cfg.DisableAI {
		return fmt.Errorf("此行为未获得AI授权")
	}
	if !actionActorAllowed(st, p, a.actor, meta.Legacy) {
		return fmt.Errorf("发起人未获得此行为授权")
	}
	if target != a.q.Target && meta.Scope != "self" && meta.Scope != "origin" && (!p.AllowCross || !actionActorAllowed(st, p, a.actor, false)) {
		return fmt.Errorf("未允许跨会话操作")
	}
	if e := behaviorAllowed(st, op, target); e != nil {
		return e
	}
	if st.Settings["reply.enabled"] != true || !replyTargetAllowed(st, a.q.Target) || !slices.ContainsFunc(st.Contacts, func(c Contact) bool { return c.ID == a.q.Target && c.Mode == "auto" }) {
		return fmt.Errorf("发起会话已暂停自动处理")
	}
	if a.q.AccountID != "" && (st.Account.ID != a.q.AccountID || st.AccountEpoch != a.q.AccountEpoch) {
		return fmt.Errorf("账号已切换")
	}
	return nil
}
func (a *actionSession) live() error {
	if a.q.Conversation != nil {
		return a.s.validateReplyEvidence(a.q)
	}
	return nil
}
func (a *actionSession) messageRef(id string) (Message, error) {
	r, e := a.get(id, "message", "media")
	if e != nil {
		return Message{}, e
	}
	for _, m := range a.s.store.snapshot().Messages {
		if m.ID == r.ID && (m.Target == a.q.Target || r.Owned && m.Self) && m.Target == r.Target && m.Status == "sent" {
			return m, nil
		}
	}
	return Message{}, fmt.Errorf("原消息已失效或撤回")
}
func (a *actionSession) execute(ctx context.Context, call ai.ToolCall) (any, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if e := a.live(); e != nil {
		return nil, e
	}
	if call.Name == "context__Find" {
		return a.find(call.Arguments)
	}
	op := strings.ReplaceAll(call.Name, "__", ".")
	meta, ok := control.AIActions[op]
	if !ok {
		return nil, fmt.Errorf("操作未开放")
	}
	args, e := validateActionArgs(call.Arguments, meta.Parameters)
	if e != nil {
		return nil, e
	}
	target := a.q.Target
	if meta.Scope == "self" {
		target = a.s.store.snapshot().Account.ID
	}
	if id := argText(args, "target_ref"); id != "" {
		kinds := []string{"contact", "group"}
		if op == "contact.AddFriend" {
			kinds = append(kinds, "search")
		}
		r, e := a.get(id, kinds...)
		if e != nil {
			return nil, e
		}
		target = r.ID
	}
	if op == "label.ModifyContactLabels" {
		refs := argRefs(args, "member_refs")
		if len(refs) != 1 {
			return nil, fmt.Errorf("每次只修改一位好友标签")
		}
		ref, err := a.get(refs[0], "contact")
		if err != nil {
			return nil, err
		}
		if id := argText(args, "target_ref"); id != "" && target != ref.ID {
			return nil, fmt.Errorf("标签目标不一致")
		}
		target = ref.ID
	}
	if op == "message.Revoke" {
		m, err := a.messageRef(argText(args, "message_ref"))
		if err != nil {
			return nil, err
		}
		if id := argText(args, "target_ref"); id != "" && target != m.Target {
			return nil, fmt.Errorf("撤回目标与原消息不一致")
		}
		target = m.Target
	}
	if id := argText(args, "moment_ref"); id != "" {
		r, e := a.get(id, "moment")
		if e != nil {
			return nil, e
		}
		target = r.Target
		if target == "" {
			if op != "moments.Detail" {
				return nil, fmt.Errorf("动态作者尚未核实，请先查询详情")
			}
			target = a.s.store.snapshot().Account.ID
		}
	}
	if e = a.authorize(op, target); e != nil {
		return nil, e
	}
	if strings.HasPrefix(op, "chatroom.") && op != "chatroom.Create" && !isActionGroup(target) {
		return nil, fmt.Errorf("目标必须是群聊")
	}
	if strings.HasPrefix(op, "contact.") && meta.Scope == "target" && isActionGroup(target) {
		return nil, fmt.Errorf("目标必须是联系人")
	}
	p := control.ActionParams{Query: argText(args, "query")}
	if v, ok := args["offset"].(float64); ok {
		p.Offset = int(v)
	}
	content, _ := args["content"].(string)
	if op == "contact.Search" {
		content = argText(args, "query")
		if content == "" {
			return nil, fmt.Errorf("搜索条件不能为空")
		}
	}
	if v, ok := args["save"].(bool); ok {
		p.Save = v
	}
	p.LabelMode = argText(args, "mode")
	p.Kind = argText(args, "kind")
	p.URL = argText(args, "url")
	p.Cursor = argText(args, "cursor")
	if v, ok := args["latitude"].(float64); ok {
		p.Latitude = float32(v)
	}
	if v, ok := args["longitude"].(float64); ok {
		p.Longitude = float32(v)
	}
	resolve := func(key string, kinds ...string) ([]string, error) {
		ids := []string{}
		for _, ref := range argRefs(args, key) {
			r, e := a.get(ref, kinds...)
			if e != nil {
				return nil, e
			}
			if r.Kind == "member" && r.Target != target {
				return nil, fmt.Errorf("成员不属于目标群")
			}
			ids = append(ids, r.ID)
		}
		return ids, nil
	}
	if p.Members, e = resolve("member_refs", "contact", "member"); e != nil {
		return nil, e
	}
	if op == "chatroom.Create" || op == "chatroom.InviteMember" || op == "label.ModifyContactLabels" {
		for _, id := range p.Members {
			if !slices.ContainsFunc(a.s.store.snapshot().Contacts, func(c Contact) bool { return c.ID == id && c.Kind == "friend" }) {
				return nil, fmt.Errorf("选定对象不是已确认好友")
			}
		}
	}
	if op == "chatroom.GetMember" && len(p.Members) != 1 {
		return nil, fmt.Errorf("查询单个成员需选择一位成员")
	}
	if op == "chatroom.Create" && len(p.Members) < 2 {
		return nil, fmt.Errorf("建群至少选定两位好友")
	}
	if slices.Contains([]string{"chatroom.InviteMember", "chatroom.SetAdmin", "chatroom.GetMember", "chatroom.GetMembersDetail", "label.ModifyContactLabels"}, op) && len(p.Members) == 0 {
		return nil, fmt.Errorf("必须明确选择成员")
	}
	if op == "chatroom.SetAdmin" {
		for _, ref := range argRefs(args, "member_refs") {
			r, _ := a.get(ref, "member")
			if r.Kind != "member" || r.Target != target {
				return nil, fmt.Errorf("管理员必须是已查询的本群成员")
			}
		}
	}
	if id := argText(args, "person_ref"); id != "" {
		r, e := a.get(id, "contact", "member")
		if e != nil {
			return nil, e
		}
		if r.Kind == "member" && r.Target != target {
			return nil, fmt.Errorf("拍一拍或名片对象范围错误")
		}
		if op == "message.Pat" && isActionGroup(target) && r.Kind != "member" {
			return nil, fmt.Errorf("群内拍一拍必须先查询本群成员")
		}
		if op == "message.Send" && p.Kind == "card" && r.Kind != "contact" {
			return nil, fmt.Errorf("名片必须选择已查询联系人")
		}
		p.Person = r.ID
	}
	if op == "message.Pat" && p.Person == "" {
		if isActionGroup(target) {
			return nil, fmt.Errorf("群内拍一拍必须指定成员")
		}
		p.Person = target
	}
	if op == "message.Send" && p.Kind == "location" {
		if _, ok := args["latitude"]; !ok {
			return nil, fmt.Errorf("缺少纬度")
		}
		if _, ok := args["longitude"]; !ok {
			return nil, fmt.Errorf("缺少经度")
		}
	}
	if op == "message.Send" && p.Kind == "card" && p.Person == "" {
		return nil, fmt.Errorf("缺少名片联系人")
	}
	if op == "message.Send" && len(p.Members) > 0 {
		if !isActionGroup(target) || p.Kind != "" && p.Kind != "text" {
			return nil, fmt.Errorf("仅群聊文字支持@")
		}
		for _, ref := range argRefs(args, "member_refs") {
			r, _ := a.get(ref, "member")
			if r.Kind != "member" || r.Target != target {
				return nil, fmt.Errorf("@对象必须是已查询的本群成员")
			}
		}
	}
	if id := argText(args, "media_ref"); id != "" {
		ref, ok := a.refs[id]
		if !ok {
			return nil, fmt.Errorf("媒体引用无效")
		}
		if ref.Kind == "asset" {
			p.Media, _, e = a.s.replyMediaBytes(ref.ID)
		} else {
			m, err := a.messageRef(id)
			if err != nil {
				return nil, err
			}
			if op == "user.UploadAvatar" && m.Kind != "image" {
				return nil, fmt.Errorf("头像必须使用图片")
			}
			if op == "message.Send" && p.Kind != "file" && m.Kind != p.Kind {
				return nil, fmt.Errorf("素材类型与发送类型不匹配")
			}
			p.Media, _, e = a.s.loadMessageMedia(ctx, m)
		}
		if e != nil {
			return nil, e
		}
	}

	if op == "message.Send" && slices.Contains([]string{"image", "audio", "video", "file"}, p.Kind) && len(p.Media) == 0 {
		return nil, fmt.Errorf("缺少可用媒体")
	}
	for _, ref := range argRefs(args, "media_refs") {
		m, e := a.messageRef(ref)
		if e != nil || m.Kind != "image" {
			return nil, fmt.Errorf("朋友圈素材必须是已读取图片")
		}
		data, _, e := a.s.loadMessageMedia(ctx, m)
		if e != nil {
			return nil, e
		}
		p.Images = append(p.Images, data)
	}
	if len(p.Images) > 9 {
		return nil, fmt.Errorf("朋友圈最多9张图片")
	}
	if p.Blacklist, e = resolve("blacklist_refs", "contact"); e != nil {
		return nil, e
	}
	if p.WithUsers, e = resolve("with_refs", "contact"); e != nil {
		return nil, e
	}
	if id := argText(args, "moment_ref"); id != "" {
		r, e := a.get(id, "moment")
		if e != nil {
			return nil, e
		}
		resolved := r.Target
		if resolved == "" && op == "moments.Detail" {
			resolved = a.s.store.snapshot().Account.ID
		}
		if e = a.authorize(op, resolved); e != nil {
			return nil, e
		}
		target = resolved
		p.Cursor = r.ID
		if op == "moments.Detail" {
			content = r.ID
		}
	}
	if id := argText(args, "comment_ref"); id != "" {
		r, e := a.get(id, "comment")
		if e != nil || r.Target != p.Cursor {
			return nil, fmt.Errorf("评论不属于指定动态")
		}
		n, e := strconv.ParseInt(r.ID, 10, 32)
		if e != nil || n <= 0 {
			return nil, fmt.Errorf("评论编号无效")
		}
		p.ReplyComment = int32(n)
	}
	labelRefs := argRefs(args, "label_refs")
	if id := argText(args, "label_ref"); id != "" {
		labelRefs = append(labelRefs, id)
	}
	for _, id := range labelRefs {
		r, e := a.get(id, "label")
		if e != nil {
			return nil, e
		}
		n, e := strconv.ParseUint(r.ID, 10, 32)
		if e != nil {
			return nil, e
		}
		p.Labels = append(p.Labels, uint32(n))
	}
	if op == "favor.Sync" && p.Cursor != "" {
		r, e := a.get(p.Cursor, "cursor")
		if e != nil {
			return nil, e
		}
		p.Cursor = r.ID
	}
	if op == "contact.AddFriend" {
		r, e := a.get(argText(args, "target_ref"), "search")
		if e != nil || r.Ticket == nil {
			return nil, fmt.Errorf("缺少有效搜索票据")
		}
		p.V1, p.V2 = r.Ticket.V1, r.Ticket.V2
		p.Scene = 3
	}
	if op == "contact.VerifyFriend" {
		r, e := a.get(argText(args, "request_ref"), "request")
		if e != nil || r.Request == nil {
			return nil, fmt.Errorf("申请引用无效")
		}
		p.V1, p.V2, p.Scene = r.Request.V1, r.Request.V2, r.Request.Scene
	}
	if op == "contact.List" {
		return a.contacts(args)
	}
	if id := argText(args, "message_ref"); id != "" {
		m, e := a.messageRef(id)
		if e != nil {
			return nil, e
		}
		if op == "message.Download" {
			if m.Target != a.q.Target {
				return nil, fmt.Errorf("媒体下载仅允许当前会话资料")
			}
			data, mime, e := a.s.loadMessageMedia(ctx, m)
			if e != nil {
				return nil, e
			}
			path, e := a.s.saveMedia(data, mime)
			return map[string]any{"media_ref": a.put(actionRef{Kind: "media", ID: m.ID, Target: m.Target}), "media": path, "mime": mime}, e
		}
		if op == "message.Revoke" {
			if !m.Self || m.RevokeReceipt == nil || m.RevokeReceipt.Account != a.s.store.snapshot().Account.ID {
				return nil, fmt.Errorf("消息缺少本人原始回执")
			}
			created, e := strconv.ParseInt(m.RevokeReceipt.Created, 10, 64)
			if e != nil || time.Now().Unix()-created < 0 || time.Now().Unix()-created > 120 {
				return nil, fmt.Errorf("超过撤回时限")
			}
			raw, _ := json.Marshal(m.RevokeReceipt)
			cmd := control.Command{Operation: "revoke", Target: m.Target, ID: m.ID, Params: raw}
			return a.perform(ctx, op, cmd, args)
		}
		content = m.Content
		p.Kind = m.Kind
		p.Descriptor, e = a.descriptor(m)
		if e != nil {
			return nil, e
		}
	}
	raw, _ := json.Marshal(p)
	cmd := control.Command{Operation: "ai-action", Target: target, ID: op, Content: content, Params: raw}
	if meta.Legacy {
		cmd.Operation = control.AIOperations[op]
	}
	return a.perform(ctx, op, cmd, args)
}
func (a *actionSession) descriptor(m Message) ([]byte, error) {
	if raw, e := a.s.store.getSecret(a.s.messageMediaKey(m)); e == nil {
		return []byte(raw), nil
	}
	if m.Kind == "file" {
		raw, e := a.s.fileDescriptor(m)
		return []byte(raw), e
	}
	msg := &message.Message{Content: m.Content, Type: message.TypeText, Sender: &contact.Contact{Username: m.Target}}
	switch m.Kind {
	case "text":
	case "link", "app", "quote":
		raw, e := a.s.store.getSecret("quote:" + a.s.messageMediaKey(m))
		if e != nil {
			return nil, fmt.Errorf("原消息票据缺失")
		}
		var item struct {
			App struct {
				Type  int    `xml:"type"`
				Title string `xml:"title"`
				Desc  string `xml:"des"`
				URL   string `xml:"url"`
				Thumb string `xml:"thumburl"`
			} `xml:"appmsg"`
		}
		if xml.Unmarshal([]byte(raw), &item) != nil {
			return nil, fmt.Errorf("原消息格式无效")
		}
		switch item.App.Type {
		case 5:
			msg.Type = message.TypeAppLink
			msg.Data = &message.Message_App{App: &message.AppData{Title: item.App.Title, Desc: item.App.Desc, Url: item.App.URL, Xml: item.App.Thumb, SubType: 5}}
		case 57:
			msg.Type = message.TypeAppQuote
			msg.Data = &message.Message_App{App: &message.AppData{Xml: raw, SubType: 57}}
		default:
			return nil, fmt.Errorf("此历史消息类型缺少可用转发描述")
		}
	default:
		return nil, fmt.Errorf("原消息缺少可转发描述")
	}
	return protojson.Marshal(msg)
}
func (a *actionSession) perform(ctx context.Context, op string, cmd control.Command, args map[string]any) (any, error) {
	meta := control.AIActions[op]
	if meta.Write {
		return a.write(ctx, op, cmd, args)
	}
	if a.s.cfg.Mock {
		return a.mockRead(op, cmd, args)
	}
	if a.s.host == nil {
		return nil, fmt.Errorf("微信未就绪")
	}
	result, e := a.s.host.Execute(ctx, cmd)
	if e != nil {
		return nil, e
	}
	if e = checkActionReceipt(result); e != nil {
		return nil, e
	}
	return a.present(op, cmd, result)
}

// 对外保存媒体的窄入口；不允许读取任意文件。
func (s *Server) StoreActionMedia(data []byte, mime string) (string, error) {
	return s.saveMedia(data, mime)
}

// 常规轮次只提供短目录；模型按需发现参数，避免每条闲聊携带全部操作的完整 schema。
func (s *Server) behaviorToolsFor(q ai.Request) []ai.Tool {
	inventory := s.behaviorToolInventory(q)
	out := []ai.Tool{}
	names := []string{}
	for _, t := range inventory {
		op := strings.ReplaceAll(t.Name, "__", ".")
		if control.AIActions[op].Legacy || t.Name == "context__Find" {
			out = append(out, t)
		} else {
			names = append(names, t.Name)
		}
	}
	if len(names) > 0 {
		out = append(out, ai.Tool{Name: "actions__Discover", Description: "按本轮需要接入工具参数。可选择：" + strings.Join(names, "、") + "。普通聊天不需要操作。", Parameters: map[string]any{"type": "object", "properties": map[string]any{"operations": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": names}, "maxItems": 8, "uniqueItems": true}}, "required": []string{"operations"}, "additionalProperties": false}})
	}
	return out
}
