// 文件名：ai_actions.go
// 主要功能：将已授权的有限 AI 参数适配到现有 SDK；不调用占位接口，不修改 core。
package console

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	chatroomability "github.com/we-clay/we-clay/gateway/internal/ability/chatroom"
	messageability "github.com/we-clay/we-clay/gateway/internal/ability/message"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/chatroom"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/favor"
	"github.com/we-clay/we-clay/sdk/label"
	"github.com/we-clay/we-clay/sdk/message"
	"github.com/we-clay/we-clay/sdk/miniapp"
	"github.com/we-clay/we-clay/sdk/moments"
	"github.com/we-clay/we-clay/sdk/user"
	"google.golang.org/protobuf/encoding/protojson"
)

func (b *Bridge) executeAIAction(ctx context.Context, c control.Command) (any, error) {
	if _, ok := control.AIActions[c.ID]; !ok {
		return nil, fmt.Errorf("未开放的 AI 操作")
	}
	var p control.ActionParams
	if json.Unmarshal(c.Params, &p) != nil {
		return nil, fmt.Errorf("行为参数无效")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch c.ID {
	case "contact.Get":
		return b.personInfo(c.Target)
	case "user.GetProfile":
		r, e := user.Instance.GetProfile()
		if e != nil {
			return nil, e
		}
		if r == nil || r.GetBaseResult() == nil || r.GetBaseResult().GetCode() != 0 || r.GetProfile() == nil {
			return nil, fmt.Errorf("本人资料回执无效")
		}
		p := r.GetProfile()
		return dashboard.Contact{ID: p.GetUsername(), Name: p.GetNickname(), Alias: p.GetAlias(), Signature: p.GetSignature(), Region: strings.TrimSpace(p.GetProvince() + " " + p.GetCity())}, nil
	case "contact.GetSelf":
		self := contact.Instance.GetSelf()
		if self == nil {
			return nil, fmt.Errorf("微信未就绪")
		}
		return map[string]any{"name": self.GetNickname(), "id": self.GetUsername(), "alias": self.GetAlias()}, nil
	case "contact.Search":
		v := contact.Instance.Search(c.Content, 0, 0)
		if v == nil {
			return nil, fmt.Errorf("没有搜索结果")
		}
		// V1/V2 只传给本地执行会话保存，不进入模型或普通日志。
		return v, nil
	case "contact.AddFriend":
		return map[string]string{"status": "request_sent"}, contact.Instance.AddFriend(p.V1, p.V2, c.Content, 2, p.Scene)
	case "contact.VerifyFriend":
		return map[string]string{"status": "accepted"}, contact.Instance.VerifyFriend(p.V1, p.V2, p.Scene)
	case "chatroom.Create":
		if len(p.Members) < 2 || len(p.Members) > 50 {
			return nil, fmt.Errorf("建群需明确选择2至50位好友")
		}
		r, e := chatroom.Instance.Create(p.Members)
		if e != nil {
			return nil, e
		}
		if r == nil || r.GetChatroom() == "" {
			return nil, fmt.Errorf("建群回执缺少群ID")
		}

		return map[string]any{"id": r.GetChatroom(), "name": r.GetName(), "status": "created"}, nil
	case "chatroom.GetInfo":
		return b.roomInfo(c.Target)
	case "chatroom.GetQRCode":
		r, e := chatroom.Instance.GetQRCode(c.Target)
		if e != nil {
			return nil, e
		}
		if r == nil {
			return nil, fmt.Errorf("二维码回执为空")
		}
		return b.aiQRCode(r.GetQrcode(), r.GetQrcodeUrl())
	case "chatroom.ListMembers", "chatroom.GetMember", "chatroom.GetMembersDetail":
		if e := chatroomability.Refresh(c.Target); e != nil {
			return nil, e
		}
		list := chatroom.Instance.ListMembers(c.Target)
		if c.ID != "chatroom.ListMembers" {
			for _, id := range p.Members {
				found := false
				for _, m := range list {
					found = found || m.GetUsername() == id
				}
				if !found {
					return nil, fmt.Errorf("指定人员不在目标群")
				}
			}
			list = chatroom.Instance.GetMembersDetail(c.Target, p.Members)
			if len(list) != len(p.Members) {
				return nil, fmt.Errorf("指定成员详情未完整返回")
			}
		}
		out := []map[string]any{}
		for _, m := range list {
			if p.Query != "" && !strings.Contains(strings.ToLower(m.GetNickname()+" "+m.GetDisplayName()+" "+m.GetRemark()), strings.ToLower(p.Query)) {
				continue
			}
			if len(p.Members) > 0 && !containsID(p.Members, m.GetUsername()) {
				continue
			}
			out = append(out, map[string]any{"id": m.GetUsername(), "name": m.GetNickname(), "display_name": m.GetDisplayName(), "remark": m.GetRemark(), "alias": m.GetAlias(), "signature": m.GetSignature(), "region": strings.TrimSpace(m.GetProvince() + " " + m.GetCity())})
		}
		start := min(p.Offset, len(out))
		end := min(start+20, len(out))
		if c.ID != "chatroom.ListMembers" {
			end = len(out)
		}
		return map[string]any{"items": out[start:end], "total": len(out), "next_offset": end, "has_more": end < len(out)}, nil
	case "chatroom.InviteMember":
		r, e := chatroom.Instance.InviteMember(c.Target, p.Members)
		if e != nil {
			return nil, e
		}
		if r == nil {
			return nil, fmt.Errorf("邀请回执为空")
		}
		return map[string]any{"status": "invitation_sent", "notice": "邀请已发出，不代表成员已经入群", "member_count": r.GetMemberCount()}, nil
	case "chatroom.SetRemark":
		return chatroom.Instance.SetRemark(c.Target, c.Content)
	case "chatroom.Save":
		return chatroom.Instance.Save(c.Target, p.Save)
	case "chatroom.SetAdmin":
		return chatroom.Instance.SetAdmin(c.Target, p.Members)
	case "message.Pat":
		return message.Instance.Pat(c.Target, p.Person)
	case "message.Forward":
		var m message.Message
		if len(p.Descriptor) == 0 || protojson.Unmarshal(p.Descriptor, &m) != nil {
			return nil, fmt.Errorf("原消息描述缺失")
		}
		r, e := message.Instance.Forward(&m, c.Target)
		if e != nil {
			return nil, e
		}
		if r == nil || r.GetNewId() == 0 {
			return nil, fmt.Errorf("转发回执无效，结果未知")
		}
		return b.aiSendReceipt(c.Target, r.GetNewId()), nil
	case "message.Send":
		return b.aiSend(ctx, c, p)
	case "moments.Timeline":
		return b.momentPage("", p.Cursor)
	case "moments.UserPage":
		return b.momentPage(c.Target, p.Cursor)
	case "moments.Detail":
		id, e := strconv.ParseUint(c.Content, 10, 64)
		if e != nil {
			return nil, e
		}
		r, e := moments.Instance.Detail(id)
		if e != nil {
			return nil, e
		}
		if r == nil || r.GetBaseResult() == nil || r.GetBaseResult().GetCode() != 0 || r.GetObject() == nil {
			return nil, fmt.Errorf("朋友圈详情未确认")
		}
		comments := []map[string]any{}
		for _, v := range r.GetObject().GetCommentUserList() {
			comments = append(comments, map[string]any{"id": strconv.FormatUint(uint64(v.GetCommentId()), 10), "author": v.GetNickname(), "content": v.GetContent()})
		}
		return map[string]any{"moment": mapMomentItems([]*moments.Moment{r.GetObject()})[0], "comments": comments}, nil
	case "moments.Like", "moments.Unlike", "moments.Comment":
		id, e := strconv.ParseUint(p.Cursor, 10, 64)
		if e != nil {
			return nil, fmt.Errorf("朋友圈标识无效")
		}
		if c.ID == "moments.Like" {
			return moments.Instance.Like(id)
		}
		if c.ID == "moments.Unlike" {
			return moments.Instance.Unlike(id)
		}
		return moments.Instance.Comment(id, c.Content, 2, p.ReplyComment)
	case "moments.Post":
		raw, _ := json.Marshal(dashboard.MomentPost{Content: c.Content, Images: p.Images, Blacklist: p.Blacklist, WithUsers: p.WithUsers})
		return b.publishMoment(raw)
	case "label.List":
		return label.Instance.List()
	case "label.Add":
		list, e := label.Instance.List()
		if e != nil {
			return nil, e
		}
		for _, v := range list {
			if v.GetName() == c.Content {
				return map[string]any{"id": v.GetId(), "name": v.GetName(), "existing": true}, nil
			}
		}
		return label.Instance.Add(c.Content)
	case "label.Delete", "label.Update":
		if len(p.Labels) != 1 {
			return nil, fmt.Errorf("需要指定一个标签")
		}
		if c.ID == "label.Delete" {
			return label.Instance.Delete(strconv.FormatUint(uint64(p.Labels[0]), 10))
		}
		return label.Instance.Update(p.Labels[0], c.Content)
	case "label.ModifyContactLabels":
		return b.modifyAILabels(p)
	case "favor.GetInfo":
		return favor.Instance.GetInfo()
	case "favor.Sync":
		key, e := base64.StdEncoding.DecodeString(p.Cursor)
		if e != nil {
			return nil, fmt.Errorf("收藏游标无效")
		}
		return favor.Instance.Sync(key)
	case "miniapp.GetRecord":
		return miniapp.Instance.GetRecord()
	case "user.GetQRCode":
		r, e := user.Instance.GetQRCode(0)
		if e != nil {
			return nil, e
		}
		if r == nil || r.GetBaseResult() == nil || r.GetBaseResult().GetCode() != 0 {
			return nil, fmt.Errorf("二维码读取未确认")
		}
		return b.aiQRCode(r.GetQrcode().GetQrcode().GetData(), r.GetQrcode().GetQrcodeUrl())
	case "user.UploadAvatar":
		return user.Instance.UploadAvatar(bytes.NewReader(p.Media))
	}
	return nil, fmt.Errorf("操作未适配")
}
func containsID(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
func (b *Bridge) aiQRCode(data []byte, url string) (any, error) {
	out := map[string]string{"url": safeWebURL(url)}
	if len(data) > 0 {
		if b.Dashboard == nil {
			return nil, fmt.Errorf("媒体存储未就绪")
		}
		path, e := b.Dashboard.StoreActionMedia(data, "image/png")
		if e != nil {
			return nil, e
		}
		out["media"] = path
	}
	if out["url"] == "" && out["media"] == "" {
		return nil, fmt.Errorf("没有可用二维码")
	}
	return out, nil
}
func (b *Bridge) aiSendReceipt(target string, id uint64) map[string]string {
	out := map[string]string{"id": strconv.FormatUint(id, 10)}
	if client, original, created, ok := messageability.ReceiptFields(target, id); ok {
		out["account"] = contact.Instance.GetSelf().GetUsername()
		out["create_time"] = strconv.FormatUint(created, 10)
		if original != "" {
			out["original_client_id"] = original
		} else {
			out["client_id"] = strconv.FormatUint(client, 10)
		}
	}
	return out
}

func (b *Bridge) aiSend(ctx context.Context, c control.Command, p control.ActionParams) (any, error) {
	if !messageability.OutboundReady() {
		return nil, fmt.Errorf("基础资料尚未就绪")
	}
	if p.Kind == "" || p.Kind == "text" || p.Kind == "image" || p.Kind == "audio" || p.Kind == "file" {
		raw, _ := json.Marshal(map[string]any{"kind": p.Kind, "media": base64.StdEncoding.EncodeToString(p.Media), "mentions": p.Members})
		return b.Execute(ctx, control.Command{Operation: "send", Target: c.Target, Content: c.Content, Params: raw})
	}
	if p.Kind == "card" {
		person := contact.Instance.Get(p.Person)
		if person == nil {
			return nil, fmt.Errorf("名片联系人不存在")
		}
		r, e := messageapi.Get().SendCard(c.Target, person.GetUsername(), person.GetNickname(), person.GetAlias())
		if e != nil {
			return nil, e
		}
		if r == nil || len(r.GetList()) != 1 || r.GetList()[0].GetCode() != 0 || r.GetList()[0].GetNewId() == 0 {
			return nil, fmt.Errorf("名片发送回执未确认，勿重复发送")
		}
		item := r.GetList()[0]
		messageability.RememberReceipt(c.Target, item.GetNewId(), item.GetClientId(), uint64(item.GetCreateTime()))
		return b.aiSendReceipt(c.Target, item.GetNewId()), nil
	}
	m := &message.Message{Receiver: &contact.Contact{Username: c.Target}, Content: c.Content}
	switch p.Kind {
	case "link":
		if safeWebURL(p.URL) == "" {
			return nil, fmt.Errorf("链接无效")
		}
		m.Type = message.TypeAppLink
		m.Data = &message.Message_App{App: &message.AppData{Title: c.Content, Url: p.URL}}
	case "location":
		m.Type = message.TypeLocation
		m.Data = &message.Message_Location{Location: &message.LocationData{Label: c.Content, Latitude: float64(p.Latitude), Longitude: float64(p.Longitude), Scale: 15}}
	case "video":
		thumb, duration, e := prepareAIVideo(ctx, p.Media)
		if e != nil {
			return nil, e
		}
		m.Type = message.TypeVideo
		m.Data = &message.Message_Video{Video: &message.VideoData{Media: &message.Media{Data: p.Media, Size: uint32(len(p.Media))}, Thumb: thumb, Duration: duration}}
	default:
		return nil, fmt.Errorf("消息类型未接通")
	}
	r, e := message.Instance.Send(m)
	if e != nil {
		return nil, e
	}
	if r == nil || r.GetNewId() == 0 {
		return nil, fmt.Errorf("发送回执未确认")
	}
	return b.aiSendReceipt(c.Target, r.GetNewId()), nil
}
