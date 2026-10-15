// 文件名：bridge.go
// 主要功能：把控制台的有限操作适配到现有 SDK，保持 core 与插件内部实现隔离。
// 作者：2guan
package console

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	messageability "github.com/we-clay/we-clay/gateway/internal/ability/message"
	"github.com/we-clay/we-clay/gateway/internal/api"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	pluginmanager "github.com/we-clay/we-clay/gateway/internal/plugin"
	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/chatroom"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/message"
	"github.com/we-clay/we-clay/sdk/moments"
)

type Bridge struct {
	contactSync      atomic.Bool
	contactSyncMu    sync.Mutex
	contactSyncWG    sync.WaitGroup
	closing          bool
	groupSyncPending map[string]bool
	groupSyncAttempt map[string]time.Time
	groupSyncRunning bool
	restartMu        sync.RWMutex
	restartLogin     func(context.Context) error
	Ready            atomic.Bool
	Dashboard        *dashboard.Server
}

// SetLoginRestarter 在 Run 完成初始化后安装回调，避免 HTTP 请求与生命周期赋值竞争。
func (b *Bridge) SetLoginRestarter(restart func(context.Context) error) {
	b.restartMu.Lock()
	defer b.restartMu.Unlock()
	b.restartLogin = restart
}

func (b *Bridge) Execute(ctx context.Context, c control.Command) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.Operation == "login-restart" {
		b.restartMu.RLock()
		restart := b.restartLogin
		b.restartMu.RUnlock()
		if restart == nil {
			return nil, fmt.Errorf("当前 core 不由控制台管理，无法在此重启")
		}
		if err := restart(ctx); err != nil {
			return nil, err
		}
		return map[string]bool{"restarting": true}, nil
	}
	if c.Operation == "login-view" {
		var result json.RawMessage
		err := api.GetCore().Get("/api/login/view").WithContext(ctx).DoJson(&result)
		return result, err
	}
	// 插件配置属于本地宿主管理，不要求微信在线，也不接收 core 路径。
	switch c.Operation {
	case "plugin-list":
		return pluginmanager.ConsolePlugins(), nil
	case "plugin-config-read":
		return pluginmanager.ConsoleConfiguration(c.ID, nil, "")
	case "plugin-config-save":
		var q struct {
			Text     string `json:"text"`
			Revision string `json:"revision"`
		}
		if err := json.Unmarshal(c.Params, &q); err != nil {
			return nil, fmt.Errorf("插件配置请求无效")
		}
		return pluginmanager.ConsoleConfiguration(c.ID, &q.Text, q.Revision)
	}
	if !b.Ready.Load() {
		return nil, fmt.Errorf("微信尚未就绪，请完成登录")
	}
	switch c.Operation {
	case "message-thumbnail":
		// 转发文章的 cdnthumburl 使用缩略图通道；高清图片通道会被 CDN 拒绝。
		// 复用 core 既有封面下载接口，票据由控制台已归档消息提供。
		var q struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		}
		if err := json.Unmarshal(c.Params, &q); err != nil || q.ID == "" || q.Key == "" {
			return nil, fmt.Errorf("缩略图参数无效")
		}
		return api.GetCore().Get("/api/cdn/download/video/cover").WithContext(ctx).Query("id", q.ID, "key", q.Key).DoBinary()
	case "friend-verify":
		var q struct {
			V1    string `json:"v1"`
			V2    string `json:"v2"`
			Scene int    `json:"scene"`
		}
		if json.Unmarshal(c.Params, &q) != nil || q.V1 == "" || q.V2 == "" || q.Scene < 0 || q.Scene > 1000 {
			return nil, fmt.Errorf("好友验证参数无效")
		}
		return nil, contact.Instance.VerifyFriend(q.V1, q.V2, q.Scene)
	case "message-media":
		return incomingMedia(ctx, c)
	case "payment-claim":
		return claimPayment(c)
	case "send":
		m := &message.Message{Type: message.TypeText, Receiver: &contact.Contact{Username: c.Target}, Content: c.Content}
		var p struct {
			Mentions []string `json:"mentions"`
			Media    string   `json:"media"`
			MIME     string   `json:"mime"`
			Kind     string   `json:"kind"`
			QuoteXML string   `json:"quote_xml"`
			EmojiMD5 string   `json:"emoji_md5"`
		}
		if len(c.Params) > 0 {
			if err := json.Unmarshal(c.Params, &p); err != nil {
				return nil, err
			}
		}
		if len(p.Mentions) > 0 {
			if !strings.HasSuffix(c.Target, "@chatroom") || len(p.Mentions) > 50 {
				return nil, fmt.Errorf("@ 参数仅用于群聊")
			}
			for _, id := range p.Mentions {
				if id == "" || len(id) > 256 || strings.ContainsAny(id, ",&<>\r\n\t ") || id == "notify@all" {
					return nil, fmt.Errorf("@ 成员 ID 无效")
				}
			}
			m.Data = &message.Message_Text{Text: &message.TextData{Content: c.Content, Reminds: p.Mentions}}
		}
		if p.QuoteXML != "" {
			m.Type = message.TypeAppQuote
			extend := ""
			if len(p.Mentions) > 0 {
				extend = "<msgsource><atuserlist>" + strings.Join(p.Mentions, ",") + "</atuserlist><bizflag>0</bizflag></msgsource>"
			}
			m.Data = &message.Message_App{App: &message.AppData{SubType: 57, Xml: p.QuoteXML, ExtendXml: extend}}
		}
		if p.Kind == "emoji" {
			m.Type = message.TypeEmoji
			m.Data = &message.Message_Emoji{Emoji: &message.EmojiData{Media: &message.Media{Md5: p.EmojiMD5}}}
		}
		var duration uint32
		if p.Media != "" {
			data, err := base64.StdEncoding.DecodeString(p.Media)
			if err != nil {
				return nil, err
			}
			media := &message.Media{Data: data, Size: uint32(len(data))}
			switch p.Kind {
			case "emoji":
				media.Md5 = p.EmojiMD5
				m.Type = message.TypeEmoji
				m.Data = &message.Message_Emoji{Emoji: &message.EmojiData{Media: media}}
			case "image":
				m.Type = message.TypeImage
				m.Data = &message.Message_Image{Image: &message.ImageData{Media: media}}
			case "audio":
				encoded, millis, err := encodeVoice(ctx, data)
				if err != nil {
					return nil, err
				}
				duration = millis
				format := int32(4)
				m.Type = message.TypeVoice
				m.Data = &message.Message_Voice{Voice: &message.VoiceData{Media: &message.Media{Data: encoded, Size: uint32(len(encoded))}, Duration: millis, Format: &format}}
			case "file":
				m.Type = message.TypeFile
				m.Data = &message.Message_File{File: &message.FileData{Filename: c.Content, Media: media}}
			default:
				return nil, fmt.Errorf("此媒体类型需要完成微信编解码契约验证后开放")
			}
		}
		receipt, err := message.Instance.Send(m)
		if err != nil {
			return nil, err
		}
		out := map[string]string{"id": strconv.FormatUint(receipt.GetNewId(), 10), "duration": strconv.FormatFloat(float64(duration)/1000, 'f', -1, 64)}
		if client, original, created, ok := messageability.ReceiptFields(c.Target, receipt.GetNewId()); ok {
			if original != "" {
				out["original_client_id"] = original
			} else {
				out["client_id"] = strconv.FormatUint(client, 10)
			}
			out["create_time"] = strconv.FormatUint(created, 10)
			out["account"] = contact.Instance.GetSelf().GetUsername()
		}
		return out, nil
	case "revoke":
		id, err := strconv.ParseUint(c.ID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("消息 ID 无效")
		}
		// 仅从控制台归档恢复完整回执，并绑定当前微信账号；不构造替代字段。
		if len(c.Params) > 0 {
			var receipt control.RevokeReceipt
			if json.Unmarshal(c.Params, &receipt) != nil || receipt.Account != contact.Instance.GetSelf().GetUsername() {
				return nil, fmt.Errorf("撤回回执与当前账号不匹配")
			}
			client, e1 := strconv.ParseUint(receipt.ClientID, 10, 64)
			created, e2 := strconv.ParseUint(receipt.Created, 10, 64)
			if e2 != nil || created == 0 || (receipt.OriginalClientID == "" && (e1 != nil || client == 0)) || (receipt.OriginalClientID != "" && receipt.ClientID != "") {
				return nil, fmt.Errorf("原始撤回回执不完整")
			}
			age := time.Now().Unix() - int64(created)
			if age < 0 || age > 120 {
				return nil, fmt.Errorf("原始发送时间已超过两分钟，不能撤回")
			}
			if receipt.OriginalClientID != "" {
				// 字符串 ID 走专用协议字段，不用消息 ID 或当前时间补造回执。
				var response messageapi.RevokeMessageResponse
				err := api.GetCore().Post("/api/message/revoke").WithContext(ctx).Body(map[string]any{"receiver": c.Target, "new_id": c.ID, "string_client_id": receipt.OriginalClientID, "create_time": receipt.Created}).DoProto(&response)
				if err != nil {
					return nil, err
				}
				if response.GetBaseResponse() == nil || response.GetBaseResponse().Code == nil || response.GetBaseResponse().GetCode() != 0 {
					return nil, fmt.Errorf("撤回回执未确认成功")
				}
				return &message.Revoke_Response{Code: 0}, nil
			}
			messageability.RememberReceipt(c.Target, id, client, created)
		}
		return message.Instance.Revoke(c.Target, id)
	case "remark":
		return nil, contact.Instance.SetRemark(c.Target, c.Content)
	case "group-name":
		return chatroom.Instance.SetName(c.Target, c.Content)
	case "announcement":
		return chatroom.Instance.SetAnnouncement(c.Target, c.Content)
	case "room-info":
		return b.roomInfo(c.Target)
	case "person-info":
		return b.personInfo(c.Target)
	case "moments-like", "moments-comment":
		return b.interactMoment(c)
	case "moments-cover":
		target := c.Target
		if target == "" && contact.Instance != nil {
			if self := contact.Instance.GetSelf(); self != nil {
				target = self.Username
			}
		}
		return map[string]string{"cover": momentCover(target)}, nil
	case "moments-page":
		return b.momentPage(c.Target, c.ID)
	case "moments-notifications":
		return b.momentNotifications()
	case "moments-publish":
		return b.publishMoment(c.Params)
	case "moment-refresh":
		return nil, b.SyncMoments(c.ID)
	case "moment-like", "moment-unlike":
		id, err := strconv.ParseUint(c.ID, 10, 64)
		if err != nil {
			return nil, err
		}
		if c.Operation == "moment-unlike" {
			return moments.Instance.Unlike(id)
		}
		return moments.Instance.Like(id)
	case "moment-comment":
		id, err := strconv.ParseUint(c.ID, 10, 64)
		if err != nil {
			return nil, err
		}
		return moments.Instance.Comment(id, c.Content, 2, 0)
	case "moment-post":
		return nil, fmt.Errorf("朋友圈发布需要对象描述，当前真实模式暂不接受纯文本直传")
	case "refresh":
		b.startContactSync(false)
		return nil, nil
	default:
		return nil, fmt.Errorf("当前操作尚未接通真实微信接口")
	}
}
func (b *Bridge) SyncContacts() error {
	items := []dashboard.Contact{}
	for _, c := range contact.Instance.List() {
		// 只把 SDK 明确标记的好友显示为好友，群、公众号及未知联系人不能混入好友筛选。
		kind := "contact"
		switch c.GetType() {
		case contact.ContactType_CONTACT_TYPE_FRIEND:
			kind = "friend"
		case contact.ContactType_CONTACT_TYPE_CHATROOM:
			kind = "group"
		case contact.ContactType_CONTACT_TYPE_OFFICIAL:
			kind = "official"
		case contact.ContactType_CONTACT_TYPE_SPECIAL:
			kind = "system"
		}
		avatar := ""
		if c.GetAvatar() != "" {
			avatar = c.GetAvatar()
		}
		name := c.Nickname
		if name == "" {
			name = c.Username
		}
		items = append(items, dashboard.Contact{Members: int(c.GetChatroom().GetMemberCount()), WechatPinned: c.Pinned, Muted: c.Muted, Pinned: c.GetPinned(), ID: c.Username, Name: name, Remark: c.Remark, Avatar: avatar, Alias: c.GetAlias(), Signature: c.GetFriend().GetSignature(), Region: strings.TrimSpace(c.GetFriend().GetProvince() + " " + c.GetFriend().GetCity()), Kind: kind, Tags: []string{}, Mode: "paused"})
	}
	return b.Dashboard.SetContacts(items)
}
func (b *Bridge) Online() error {
	self := contact.Instance.GetSelf()
	if self == nil {
		return fmt.Errorf("微信账号信息为空")
	}
	b.Ready.Store(true)
	if err := b.Dashboard.SetAccount(dashboard.Account{Name: self.Nickname, ID: self.Username, Avatar: "我", Photo: self.GetAvatar(), Status: "online"}); err != nil {
		return err
	}
	// 不让长通讯录同步阻塞消息订阅；每批将成功资料更新到控制台。
	b.startContactSync(true)
	return b.SyncContacts()
}
func (b *Bridge) Record(m *message.Message) error {
	// 状态同步只有会话游标，没有用户发出的文字，不进入聊天归档和 AI。
	if m.GetType().GetCode() == 51 {
		return nil
	}
	self := contact.Instance.GetSelf()
	if self == nil {
		return nil
	}
	// 验证票据仅在内存中传给受规则约束的插件，不进入公共聊天归档。
	if m.GetType().GetCode() == 37 {
		var v struct {
			XMLName   xml.Name `xml:"msg"`
			Applicant string   `xml:"fromusername,attr"`
			V1        string   `xml:"encryptusername,attr"`
			V2        string   `xml:"ticket,attr"`
			Comment   string   `xml:"content,attr"`
			Scene     int      `xml:"scene,attr"`
		}
		if len(m.GetContent()) > 64<<10 || xml.Unmarshal([]byte(m.GetContent()), &v) != nil {
			return nil // 不重放格式不受支持的系统事件，也不把票据写入日志。
		}
		if v.Applicant == "" || v.V1 == "" || v.V2 == "" || v.Scene < 0 || v.Scene > 1000 {
			return nil
		}
		return b.Dashboard.HandleFriend(dashboard.FriendRequest{ID: strconv.FormatUint(uint64(m.GetId()), 10), Applicant: v.Applicant, Comment: v.Comment, V1: v.V1, V2: v.V2, Scene: v.Scene})
	}
	own := m.GetSender().GetUsername() == self.Username
	target := m.GetSender().GetUsername()
	if own {
		target = m.GetReceiver().GetUsername()
	}
	// 先完成消息归档，再异步补齐新群资料，避免阻塞接收或覆盖会话预览。
	defer func() { b.startGroupSync(target) }()
	senderID := m.GetSender().GetUsername()
	sender := m.GetSender().GetNickname()
	if m.GetMember() != nil {
		sender = m.GetMember().GetNickname()
		senderID = m.GetMember().GetUsername()
		// 群消息可能以群为发送端，成员标识才是实际发言人。
		if senderID == self.Username {
			own = true
		}
	}
	kind := "text"
	switch m.GetType().GetCode() {
	case 3:
		kind = "image"
	case 34:
		kind = "audio"
	case 47:
		kind = "emoji"
	case 48:
		kind = "location"
	case 43:
		kind = "video"
	case 4906, 6:
		kind = "file"
	case 9999, 10000, 10002:
		kind = "system"
	}
	record := dashboard.Message{SenderID: senderID, ID: strconv.FormatUint(uint64(m.GetId()), 10), Target: target, Sender: sender, Content: m.GetContent(), Kind: kind, Self: own, Source: "wechat", Status: "sent", Time: time.Unix(int64(m.GetTimestamp()), 0).Format(time.RFC3339)}
	record.SenderAvatar = m.GetSender().GetAvatar()
	if m.GetMember() != nil {
		record.SenderAvatar = m.GetMember().GetAvatar()
	}
	raw := m.GetContent()
	// SDK 对媒体 Content 已做摘要，展示解析必须使用原始 XML；不将原始票据回传浏览器。
	if kind == "image" || kind == "audio" || kind == "emoji" || kind == "location" {
		var original struct {
			Content struct {
				Value string `json:"value"`
			} `json:"content"`
		}
		if json.Unmarshal([]byte(m.GetRaw()), &original) == nil && original.Content.Value != "" {
			raw = original.Content.Value
		}
	}
	if app := m.GetApp(); app != nil {
		kind = "app"
		record.Kind = "app"
		raw = app.GetXml()
	}

	// SDK 已提取标题，但链接与图文列表仍在 AppData 中，归档不能只保存 Content。
	if app := m.GetApp(); app != nil && (app.GetSubType() == 5 || app.GetSubType() == 2000 || app.GetSubType() == 2001 || app.GetSubType() == 57 || app.GetUrl() != "") {
		record.Kind = "link"
		raw = app.GetXml()
		if app.GetUrl() != "" {
			record.Articles = []dashboard.MessageArticle{{Title: app.GetTitle(), Description: app.GetDesc(), URL: app.GetUrl()}}
		}
	}
	record, keep := dashboard.ParseWechatMessage(record, raw)
	if !keep {
		return nil
	}
	if err := b.Dashboard.RememberQuoteSource(record, raw); err != nil {
		return err
	}
	if kind == "image" || kind == "audio" || (record.Kind == "file" && m.GetType().GetCode() == message.TypeAppFileAttach.Code) {
		descriptor, err := protojson.Marshal(m)
		if err != nil {
			return err
		}
		return b.Dashboard.RecordMedia(record, descriptor)
	}
	return b.Dashboard.Record(record)
}

// startContactSync 合并重复刷新请求，避免长同步占住浏览器请求或启动多轮扫描。
func (b *Bridge) startContactSync(missingOnly bool) {
	b.contactSyncMu.Lock()
	if b.closing || !b.contactSync.CompareAndSwap(false, true) {
		b.contactSyncMu.Unlock()
		return
	}
	b.contactSyncWG.Add(1)
	b.contactSyncMu.Unlock()

	b.Dashboard.SetContactSync("正在同步通讯录…")
	go func() {
		defer b.contactSyncWG.Done()
		defer b.contactSync.Store(false)
		refresh := contactability.RefreshWithProgress
		if missingOnly {
			refresh = contactability.RefreshMissingWithProgress
		}
		err := refresh(func() error {
			err := b.SyncContacts()
			b.Dashboard.SetContactSync(fmt.Sprintf("正在同步通讯录，已获取 %d 个联系人…", len(contact.Instance.List())))
			return err
		}, b.Dashboard.ConversationIDs()...)
		if err != nil {
			slog.Warn("通讯录同步未完成，可在通讯录页重试", "err", err)
			b.Dashboard.SetContactSync("通讯录同步未完成，已保留成功获取的资料，请点击刷新重试")
		} else {
			b.Dashboard.SetContactSync("")
		}
	}()
}

// WaitContactSync 在宿主取消 core 请求后等待同步退出，避免后台任务跨账号读写缓存。
func (b *Bridge) WaitContactSync() {
	b.contactSyncMu.Lock()
	b.closing = true
	b.contactSyncMu.Unlock()
	b.contactSyncWG.Wait()
}
