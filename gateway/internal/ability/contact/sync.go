package contactability

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/we-clay/we-clay/gateway/internal/ability/change"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	sdk "github.com/we-clay/we-clay/sdk/contact"
	pluginsdk "github.com/we-clay/we-clay/sdk/plugin"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// HandleModifyContacts 处理同步得到的联系人变更。
func HandleModifyContacts(contacts []*messageapi.ModifyContact) []*pluginsdk.ChangeEvent {
	instance.cacheMu.Lock()
	defer instance.cacheMu.Unlock()
	var events []*pluginsdk.ChangeEvent
	for _, item := range contacts {
		if item == nil || item.GetUsername().GetValue() == "" {
			continue
		}

		username := item.GetUsername().GetValue()
		old := instance.cache[username]
		contact := mergeContact(old, item)
		instance.cache[username] = contact
		if old == nil {
			continue
		}

		event, ok, err := change.Detect(change.Subject{
			Domain:  "contact",
			Action:  change.ActionModify,
			Subject: contact.Username,
			Old:     old,
			New:     contact,
		})
		if err != nil {
			slog.Warn("[contact ability] 比对联系人变更失败", "username", contact.Username, "err", err)
			continue
		}
		if ok {
			events = append(events, event)
		}
	}
	return events
}

func mergeContact(old *sdk.Contact, data *messageapi.ModifyContact) *sdk.Contact {
	next := cloneContact(old)
	if next == nil {
		next = &sdk.Contact{}
	}
	if hasField(data, "username") {
		next.Username = data.GetUsername().GetValue()
	}
	if hasField(data, "nickname") {
		next.Nickname = data.GetNickname().GetValue()
	}
	if hasField(data, "remark") {
		next.Remark = data.GetRemark().GetValue()
	}
	if hasField(data, "alias") {
		next.Alias = data.GetAlias()
	}
	if hasField(data, "big_avatar_url") && data.GetBigAvatarUrl() != "" {
		next.Avatar = data.GetBigAvatarUrl()
	} else if hasField(data, "small_avatar_url") {
		next.Avatar = data.GetSmallAvatarUrl()
	}
	if hasField(data, "chatroom_owner") {
		next.Type = sdk.ContactType_CONTACT_TYPE_CHATROOM
		next.Data = &sdk.Contact_Chatroom{Chatroom: &sdk.Chatroom{Owner: data.GetChatroomOwner()}}
		return next
	}
	if hasField(data, "gender") || hasField(data, "country") || hasField(data, "province") || hasField(data, "city") || hasField(data, "signature") {
		friend := next.GetFriend()
		if friend == nil {
			friend = &sdk.Friend{}
		} else {
			friend = proto.Clone(friend).(*sdk.Friend)
		}
		if hasField(data, "gender") {
			friend.Gender = GetGender(data.GetGender())
		}
		if hasField(data, "country") {
			friend.Country = data.GetCountry()
		}
		if hasField(data, "province") {
			friend.Province = data.GetProvince()
		}
		if hasField(data, "city") {
			friend.City = data.GetCity()
		}
		if hasField(data, "signature") {
			friend.Signature = data.GetSignature()
		}
		next.Data = &sdk.Contact_Friend{Friend: friend}
	}
	if next.Type == sdk.ContactType_CONTACT_TYPE_UNSPECIFIED {
		next.Type = getTypeFromUsername(next.Username)
	}
	return next
}

func cloneContact(contact *sdk.Contact) *sdk.Contact {
	if contact == nil {
		return nil
	}
	return proto.Clone(contact).(*sdk.Contact)
}

func getTypeFromUsername(username string) sdk.ContactType {
	if username == "" {
		return sdk.ContactType_CONTACT_TYPE_UNSPECIFIED
	}
	if slices.Contains(specials, username) {
		return sdk.ContactType_CONTACT_TYPE_SPECIAL
	}
	if strings.HasPrefix(username, "gh_") {
		return sdk.ContactType_CONTACT_TYPE_OFFICIAL
	}
	return sdk.ContactType_CONTACT_TYPE_FRIEND
}

// UpdateSelf 更新当前登录账号信息并发布变更事件。
func UpdateSelf(next *sdk.SelfInfo) *pluginsdk.ChangeEvent {
	if next == nil || next.Username == "" {
		return nil
	}
	old := instance.GetSelf()
	instance.setSelf(next)
	if old == nil {
		return nil
	}
	event, ok, err := change.Detect(change.Subject{
		Domain:  "user",
		Action:  change.ActionModify,
		Subject: next.Username,
		Old:     old,
		New:     next,
	})
	if err != nil {
		slog.Warn("[contact ability] 比对当前用户变更失败", "username", next.Username, "err", err)
		return nil
	}
	if ok {
		return event
	}
	return nil
}

func ApplySelfInfo(info *messageapi.ModifyUserInfo, ext *messageapi.UserInfoExtend) (*pluginsdk.ChangeEvent, error) {
	old := instance.GetSelf()
	if old == nil && info == nil {
		return nil, fmt.Errorf("当前用户信息未初始化")
	}

	next := cloneSelfInfo(old)
	if next == nil {
		next = &sdk.SelfInfo{}
	}
	if info != nil {
		if hasField(info, "user_name") {
			next.Username = info.GetUserName().GetValue()
		}
		if hasField(info, "nick_name") {
			next.Nickname = info.GetNickName().GetValue()
		}
		if hasField(info, "alias") {
			next.Alias = info.GetAlias()
		}
		if hasField(info, "uin") {
			next.Uin = info.GetUin()
		}
		if hasField(info, "email") {
			next.Email = info.GetEmail().GetValue()
		}
		if hasField(info, "mobile") {
			next.Mobile = info.GetMobile().GetValue()
		}
	}
	if ext != nil {
		if hasField(ext, "big_avatar_url") && ext.GetBigAvatarUrl() != "" {
			next.Avatar = ext.GetBigAvatarUrl()
		} else if hasField(ext, "small_avatar_url") && ext.GetSmallAvatarUrl() != "" {
			next.Avatar = ext.GetSmallAvatarUrl()
		}
	}
	return UpdateSelf(next), nil
}

func hasField(message interface{ ProtoReflect() protoreflect.Message }, name protoreflect.Name) bool {
	if message == nil {
		return false
	}
	reflectMessage := message.ProtoReflect()
	field := reflectMessage.Descriptor().Fields().ByName(name)
	return field != nil && reflectMessage.Has(field)
}
