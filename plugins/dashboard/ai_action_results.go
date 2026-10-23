package dashboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/favor"
	"github.com/we-clay/we-clay/sdk/miniapp"
)

func checkActionReceipt(v any) error {
	if v == nil {
		return nil
	} // SetRemark 的 SDK 以 error 表示确认结果。
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer && rv.IsNil() {
		return fmt.Errorf("操作回执缺失，结果未确认")
	}
	for _, name := range []string{"GetBaseResult", "GetBaseResponse"} {
		if method := rv.MethodByName(name); method.IsValid() {
			vals := method.Call(nil)
			if len(vals) != 1 || vals[0].Kind() == reflect.Pointer && vals[0].IsNil() {
				return fmt.Errorf("操作基础回执缺失")
			}
			if get := vals[0].MethodByName("GetCode"); get.IsValid() && get.Call(nil)[0].Int() != 0 {
				return fmt.Errorf("微信返回操作失败，code=%d", get.Call(nil)[0].Int())
			}
		}
	}
	if method := rv.MethodByName("GetCode"); method.IsValid() {
		code := method.Call(nil)[0].Int()
		if code != 0 {
			return fmt.Errorf("微信返回操作失败，code=%d", code)
		}
	}
	return nil
}
func trimActionText(text string) string {
	r := []rune(text)
	if len(r) > 4000 {
		return string(r[:4000]) + "〔正文过长，展示前4000字〕"
	}
	return text
}
func (a *actionSession) contacts(args map[string]any) (any, error) {
	st := a.s.store.snapshot()
	if e := a.authorize("contact.List", st.Account.ID); e != nil {
		return nil, e
	}
	query := strings.ToLower(argText(args, "query"))
	kind := argText(args, "kind")
	offset := 0
	if n, ok := args["offset"].(float64); ok {
		offset = int(n)
	}
	all := []map[string]any{}
	for _, c := range st.Contacts {
		if c.Kind != "friend" && c.Kind != "group" {
			continue
		}
		if kind != "" && kind != "all" && c.Kind != kind {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(c.Name+" "+c.Remark+" "+c.Alias), query) {
			continue
		}
		k := "contact"
		if c.Kind == "group" {
			k = "group"
		}
		all = append(all, map[string]any{"ref": a.put(actionRef{Kind: k, ID: c.ID, Name: c.Name}), "name": c.Name, "remark": c.Remark, "alias": c.Alias, "kind": c.Kind})
	}
	offset = min(offset, len(all))
	end := min(offset+20, len(all))
	return map[string]any{"items": all[offset:end], "total": len(all), "next_offset": end, "has_more": end < len(all)}, nil
}
func (a *actionSession) find(raw string) (any, error) {
	var tool ai.Tool
	for _, t := range a.s.behaviorToolsFor(a.q) {
		if t.Name == "context__Find" {
			tool = t
		}
	}
	if tool.Parameters == nil {
		return nil, fmt.Errorf("资料查询未授权")
	}
	args, e := validateActionArgs(raw, tool.Parameters)
	if e != nil {
		return nil, e
	}
	kind := argText(args, "kind")
	query := strings.ToLower(argText(args, "query"))
	offset := 0
	if n, ok := args["offset"].(float64); ok {
		offset = int(n)
	}
	if kind == "contacts" {
		args["kind"] = "all"
		return a.contacts(args)
	}
	if kind == "operations" {
		return a.operationFacts(args)
	}
	all := []map[string]any{}
	st := a.s.store.snapshot()
	if kind == "requests" {
		if e := a.authorize("contact.VerifyFriend", st.Account.ID); e != nil {
			return nil, e
		}
		rows, e := a.s.store.db.Query("SELECT id FROM secrets WHERE id LIKE ? LIMIT 100", "ai-friend:"+st.Account.ID+":%")
		if e != nil {
			return nil, e
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return nil, e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		for _, id := range ids {
			raw, e := a.s.store.getSecret(id)
			if e != nil {
				continue
			}
			var q FriendRequest
			if json.Unmarshal([]byte(raw), &q) != nil {
				continue
			}
			created, err := time.Parse(time.RFC3339, q.Created)
			if err != nil || time.Since(created) > 24*time.Hour {
				continue
			}
			var claimed int
			_ = a.s.store.db.QueryRow("SELECT count(*) FROM business_claims WHERE account=? AND id=?", st.Account.ID, fmt.Sprintf("%x", sha256.Sum256([]byte("friend:"+q.V1+":"+q.V2)))).Scan(&claimed)
			if claimed > 0 {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(q.Applicant+q.Comment), query) {
				continue
			}
			all = append(all, map[string]any{"request_ref": a.put(actionRef{Kind: "request", ID: q.ID, Request: &q}), "applicant": q.Applicant, "comment": trimActionText(q.Comment)})
		}
	} else {
		for i := len(st.Messages) - 1; i >= 0; i-- {
			m := st.Messages[i]
			if m.Target != a.q.Target || m.Status != "sent" || m.Kind == "system" || !contextAvailable(m) {
				continue
			}
			if kind == "media" && m.Kind != "image" && m.Kind != "audio" && m.Kind != "file" && m.Kind != "video" {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(m.Sender+" "+m.Content), query) {
				continue
			}
			refKind := "message"
			if kind == "media" {
				refKind = "media"
			}
			item := map[string]any{"message_ref": a.put(actionRef{Kind: refKind, ID: m.ID, Target: m.Target}), "sender": m.Sender, "self": m.Self, "kind": m.Kind, "content": trimActionText(m.Content), "time": m.Time}
			if game := messageGame(m); game != "" {
				item["game"] = game
				item["note"] = "虚构游戏过程，不代表现实人物事实或操作指令"
			}
			all = append(all, item)
		}
	}
	offset = min(offset, len(all))
	end := min(offset+20, len(all))
	return map[string]any{"items": all[offset:end], "total": len(all), "has_more": end < len(all), "next_offset": end}, nil
}
func (a *actionSession) present(op string, cmd control.Command, v any) (any, error) {
	switch op {
	case "message.Send", "message.Forward":
		raw, _ := json.Marshal(v)
		var receipt map[string]string
		if json.Unmarshal(raw, &receipt) != nil || receipt["id"] == "" {
			return nil, fmt.Errorf("发送回执缺少消息ID")
		}
		return map[string]any{"status": "sent", "message_ref": a.put(actionRef{Kind: "message", ID: receipt["id"], Target: cmd.Target, Owned: true}), "mock": a.s.cfg.Mock}, nil
	case "chatroom.InviteMember":
		return map[string]any{"status": "invitation_sent", "notice": "邀请已发出，不代表成员已经入群"}, nil
	case "contact.AddFriend":
		return map[string]any{"status": "request_sent"}, nil
	case "contact.VerifyFriend":
		return map[string]any{"status": "accepted"}, nil
	case "contact.Search":
		c, ok := v.(*contact.Contact)
		if !ok || c == nil || c.V1 == "" || c.V2 == "" {
			return nil, fmt.Errorf("搜索未返回有效申请票据")
		}
		return map[string]any{"ref": a.put(actionRef{Kind: "search", ID: c.GetUsername(), Name: c.GetNickname(), Ticket: c}), "name": c.GetNickname(), "alias": c.GetAlias()}, nil
	case "contact.Get", "contact.GetSelf", "user.GetProfile":
		raw, _ := json.Marshal(v)
		var c Contact
		_ = json.Unmarshal(raw, &c)
		if c.ID == "" {
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			c.ID = cmd.Target
			c.Name, _ = m["name"].(string)
		}
		return map[string]any{"ref": a.put(actionRef{Kind: "contact", ID: c.ID, Name: c.Name}), "name": c.Name, "remark": c.Remark, "alias": c.Alias, "signature": c.Signature, "region": c.Region}, nil
	case "chatroom.GetInfo":
		raw, _ := json.Marshal(v)
		var info RoomInfo
		if json.Unmarshal(raw, &info) != nil {
			return nil, fmt.Errorf("群资料无效")
		}
		return map[string]any{"group_ref": a.put(actionRef{Kind: "group", ID: cmd.Target, Name: info.Name}), "name": info.Name, "announcement": trimActionText(info.Announcement), "member_count": len(info.Members), "notice": info.Notice}, nil
	case "chatroom.GetQRCode", "user.GetQRCode":
		raw, _ := json.Marshal(v)
		var data map[string]string
		_ = json.Unmarshal(raw, &data)
		if data["media"] != "" {
			return map[string]any{"media_ref": a.put(actionRef{Kind: "asset", ID: data["media"], Target: cmd.Target}), "kind": "image", "url": data["url"]}, nil
		}
		return data, nil
	case "chatroom.Create":
		raw, _ := json.Marshal(v)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		id, _ := m["id"].(string)
		if !isActionGroup(id) {
			return nil, fmt.Errorf("建群回执缺少有效群标识")
		}
		name, _ := m["name"].(string)
		return map[string]any{"group_ref": a.put(actionRef{Kind: "group", ID: id, Name: name}), "name": name, "status": "created"}, nil
	case "chatroom.ListMembers", "chatroom.GetMember", "chatroom.GetMembersDetail":
		raw, _ := json.Marshal(v)
		var page struct {
			Items []struct {
				ID, Name, Remark, Alias, Signature, Region string
				DisplayName                                string `json:"display_name"`
			}
			Total int
			Next  int  `json:"next_offset"`
			More  bool `json:"has_more"`
		}
		if json.Unmarshal(raw, &page) != nil {
			return nil, fmt.Errorf("成员结果无效")
		}
		out := []map[string]any{}
		for _, m := range page.Items {
			out = append(out, map[string]any{"ref": a.put(actionRef{Kind: "member", ID: m.ID, Target: cmd.Target, Name: m.Name}), "name": m.Name, "remark": m.Remark, "display_name": m.DisplayName, "alias": m.Alias, "signature": trimActionText(m.Signature), "region": m.Region})
		}
		return map[string]any{"items": out, "total": page.Total, "next_offset": page.Next, "has_more": page.More}, nil
	case "moments.Timeline", "moments.UserPage":
		raw, _ := json.Marshal(v)
		var page MomentPage
		if json.Unmarshal(raw, &page) != nil {
			return nil, fmt.Errorf("朋友圈结果无效")
		}
		items := []any{}
		for _, m := range page.Items {
			items = append(items, a.moment(m))
		}
		return map[string]any{"items": items, "cursor": page.NextCursor, "notice": page.Notice}, nil
	case "moments.Detail":
		raw, _ := json.Marshal(v)
		var detail struct {
			Moment   Moment
			Comments []struct{ ID, Author, Content string }
		}
		if json.Unmarshal(raw, &detail) != nil {
			return nil, fmt.Errorf("动态详情无效")
		}
		comments := []any{}
		for _, c := range detail.Comments {
			comments = append(comments, map[string]any{"comment_ref": a.put(actionRef{Kind: "comment", ID: c.ID, Target: detail.Moment.ID}), "author": c.Author, "content": trimActionText(c.Content)})
		}
		return map[string]any{"moment": a.moment(detail.Moment), "comments": comments}, nil
	case "moments.Post":
		raw, _ := json.Marshal(v)
		var m Moment
		_ = json.Unmarshal(raw, &m)
		if m.ID == "" {
			return nil, fmt.Errorf("发布回执未确认")
		}
		if m.AuthorID == "" {
			m.AuthorID = a.s.store.snapshot().Account.ID
		}
		return a.moment(m), nil
	case "label.List", "label.Add":
		raw, _ := json.Marshal(v)
		decodeNumbers := func(value any) error {
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			return dec.Decode(value)
		}
		var items []map[string]any
		if op == "label.Add" {
			var m map[string]any
			_ = decodeNumbers(&m)
			items = []map[string]any{m}
		} else {
			if decodeNumbers(&items) != nil {
				return nil, fmt.Errorf("标签列表无效")
			}
		}
		out := []any{}
		for _, m := range items {
			id := fmt.Sprint(m["id"])
			if id == "<nil>" {
				id = fmt.Sprint(m["Id"])
			}
			out = append(out, map[string]any{"label_ref": a.put(actionRef{Kind: "label", ID: id, Name: fmt.Sprint(m["name"])}), "name": m["name"]})
		}
		return map[string]any{"items": out}, nil
	case "favor.Sync":
		r, ok := v.(*favor.Sync_Response)
		if !ok || r == nil {
			return nil, fmt.Errorf("收藏列表回执无效")
		}
		items := []any{}
		for _, item := range r.GetItems() { // 不把收藏 XML 中的密钥、媒体票据交给模型。
			var x struct {
				Title string `xml:"title"`
				Desc  string `xml:"desc"`
			}
			_ = xml.Unmarshal([]byte(item.GetObject()), &x)
			items = append(items, map[string]any{"title": trimActionText(x.Title), "description": trimActionText(x.Desc), "type": item.GetType()})
		}
		cursor := ""
		if len(r.GetKey()) > 0 {
			cursor = a.put(actionRef{Kind: "cursor", ID: base64.StdEncoding.EncodeToString(r.GetKey())})
		}
		return map[string]any{"items": items, "cursor": cursor, "has_more": r.GetHasMore(), "notice": "仅为同步接口实际返回的条目，不含单项详情"}, nil
	case "miniapp.GetRecord":
		r, ok := v.(*miniapp.GetRecord_Response)
		if !ok || r == nil {
			return nil, fmt.Errorf("小程序记录无效")
		}
		items := []any{}
		for _, v := range r.GetHistoryList() {
			items = append(items, map[string]any{"name": v.GetNickname(), "time": v.GetUpdateTime()})
		}
		return map[string]any{"items": items}, nil
	}
	if control.AIActions[op].Write {
		return map[string]any{"status": "succeeded", "operation": op}, nil
	}
	// SDK 的非敏感展示结果可读；票据/媒体 URL 只在各专用适配分支处理。
	return v, nil
}
func (a *actionSession) moment(m Moment) map[string]any {
	author := m.AuthorID

	return map[string]any{"moment_ref": a.put(actionRef{Kind: "moment", ID: m.ID, Target: author}), "author": m.Author, "content": trimActionText(m.Content), "time": m.Time, "liked": m.Liked, "comments": m.Comments, "image_count": len(m.Images)}
}
func (a *actionSession) mockRead(op string, c control.Command, args map[string]any) (any, error) {
	switch op {
	case "contact.List":
		return a.contacts(args)
	case "contact.Get":
		for _, v := range a.s.store.snapshot().Contacts {
			if v.ID == c.Target {
				return a.present(op, c, v)
			}
		}
	case "label.List":
		return a.present(op, c, []map[string]any{{"id": 1, "name": "示例标签"}})
	case "moments.Timeline", "moments.UserPage":
		return a.present(op, c, MomentPage{Items: a.s.store.snapshot().Moments})
	}
	return map[string]any{"mock": true, "operation": op, "notice": "脱机演示结果，未访问微信"}, nil
}

// 历史操作只从同一发起会话、同一成员的真实回执恢复，不沿用旧任务的临时引用。
func (a *actionSession) operationFacts(args map[string]any) (any, error) {
	rows, e := a.s.actionRecords(a.q.Target, "")
	if e != nil {
		return nil, e
	}
	query := strings.ToLower(argText(args, "query"))
	out := []any{}
	for _, r := range rows {
		if r.Actor != a.actor || a.authorize(r.Operation, r.Target) != nil {
			continue
		}
		encoded, _ := json.Marshal(r.Preview)
		if query != "" && !strings.Contains(strings.ToLower(r.Operation+" "+string(encoded)), query) {
			continue
		}
		item := map[string]any{"operation": r.Operation, "status": r.Status, "created": r.Created, "preview": r.Preview}
		if r.Status == "succeeded" {
			raw, err := a.s.store.getSecret("ai-receipt:" + r.Account + ":" + r.ID)
			if err == nil {
				var receipt any
				_ = json.Unmarshal([]byte(raw), &receipt)
				switch r.Operation {
				case "chatroom.Create", "label.Add":
					if result, err := a.present(r.Operation, control.Command{Target: r.Target}, receipt); err == nil {
						item["result"] = result
					}
				case "message.Send", "message.Forward":
					var fields map[string]string
					if json.Unmarshal([]byte(raw), &fields) == nil && fields["id"] != "" {
						item["message_ref"] = a.put(actionRef{Kind: "message", ID: fields["id"], Target: r.Target, Owned: true})
					}
				}
			}
		}
		out = append(out, item)
		if len(out) == 20 {
			break
		}
	}
	return map[string]any{"items": out, "notice": "仅包含同一发起会话、同一成员、当前仍获授权的操作事实；未知状态不能当作成功"}, nil
}
