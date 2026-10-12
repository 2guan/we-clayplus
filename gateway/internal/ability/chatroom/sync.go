package chatroomability

import (
	"log/slog"

	"github.com/duke-git/lancet/v2/maputil"
	"github.com/we-clay/we-clay/gateway/internal/ability/change"
	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	sdk "github.com/we-clay/we-clay/sdk/chatroom"
	pluginsdk "github.com/we-clay/we-clay/sdk/plugin"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// HandleModifyMembers 处理同步得到的群成员资料变更。
func HandleModifyMembers(members []*messageapi.ModifyChatroomMember) []*pluginsdk.ChangeEvent {
	instance.cacheMu.Lock()
	defer instance.cacheMu.Unlock()
	var events []*pluginsdk.ChangeEvent
	for _, item := range members {
		if item == nil || item.GetUsername().GetValue() == "" {
			continue
		}
		chatrooms := memberChatrooms(item.GetUsername().GetValue())
		for _, chatroom := range chatrooms {
			next := mergeMember(instance.cache[chatroom][item.GetUsername().GetValue()], item)
			if event := buildMemberChange(chatroom, next.Username, instance.cache[chatroom][next.Username], next, "chatroom_member"); event != nil {
				events = append(events, event)
			}
			instance.cache[chatroom][next.Username] = next
		}
	}
	return events
}

// HandleModifyMemberDisplayNames 处理同步得到的群成员显示名称变更。
func HandleModifyMemberDisplayNames(displayNames []*messageapi.ModifyChatroomMemberDisplayName) []*pluginsdk.ChangeEvent {
	instance.cacheMu.Lock()
	defer instance.cacheMu.Unlock()
	var events []*pluginsdk.ChangeEvent
	for _, item := range displayNames {
		if item == nil || item.GetChatroom() == "" || item.GetUsername() == "" {
			continue
		}
		cache := maputil.GetOrSet(instance.cache, item.GetChatroom(), map[string]*sdk.Member{})
		old := cache[item.GetUsername()]
		next := cloneMember(old)
		if next == nil {
			next = &sdk.Member{Username: item.GetUsername()}
		}
		next.DisplayName = item.GetDisplayName()
		if event := buildMemberChange(item.GetChatroom(), item.GetUsername(), old, next, "chatroom_member_display_name"); event != nil {
			events = append(events, event)
		}
		cache[item.GetUsername()] = next
	}
	return events
}

func memberChatrooms(username string) []string {
	var chatrooms []string
	for chatroom, members := range instance.cache {
		if _, ok := members[username]; ok {
			chatrooms = append(chatrooms, chatroom)
		}
	}
	return chatrooms
}

func mergeMember(old *sdk.Member, item *messageapi.ModifyChatroomMember) *sdk.Member {
	next := cloneMember(old)
	if next == nil {
		next = &sdk.Member{}
	}
	if hasField(item, "username") {
		next.Username = item.GetUsername().GetValue()
	}
	if hasField(item, "nickname") {
		next.Nickname = item.GetNickname().GetValue()
	}
	if hasField(item, "remark") {
		next.Remark = item.GetRemark().GetValue()
	}
	if hasField(item, "alias") {
		next.Alias = item.GetAlias()
	}
	if hasField(item, "small_avatar_url") {
		next.Avatar = item.GetSmallAvatarUrl()
	}
	if hasField(item, "big_avatar_url") && next.Avatar == "" {
		next.Avatar = item.GetBigAvatarUrl()
	}
	if hasField(item, "gender") {
		next.Gender = contactability.GetGender(item.GetGender())
	}
	if hasField(item, "country") {
		next.Country = item.GetCountry()
	}
	if hasField(item, "province") {
		next.Province = item.GetProvince()
	}
	if hasField(item, "city") {
		next.City = item.GetCity()
	}
	if hasField(item, "signature") {
		next.Signature = item.GetSignature()
	}
	return next
}

func buildMemberChange(chatroom, username string, oldMember, nextMember *sdk.Member, domain string) *pluginsdk.ChangeEvent {
	if oldMember == nil {
		return nil
	}
	event, ok, err := change.Detect(change.Subject{
		Domain:  domain,
		Action:  change.ActionModify,
		Subject: username,
		Parent:  chatroom,
		Old:     oldMember,
		New:     nextMember,
	})
	if err != nil {
		slog.Warn("[chatroom ability] 比对群成员变更失败", "chatroom", chatroom, "username", username, "err", err)
		return nil
	}
	if ok {
		return event
	}
	return nil
}

func cloneMember(member *sdk.Member) *sdk.Member {
	if member == nil {
		return nil
	}
	return proto.Clone(member).(*sdk.Member)
}

func hasField(message interface{ ProtoReflect() protoreflect.Message }, name protoreflect.Name) bool {
	if message == nil {
		return false
	}
	reflectMessage := message.ProtoReflect()
	field := reflectMessage.Descriptor().Fields().ByName(name)
	return field != nil && reflectMessage.Has(field)
}
