package sync

import (
	chatroomability "github.com/we-clay/we-clay/gateway/internal/ability/chatroom"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
)

func handleChatroomMember(members []*messageapi.ModifyChatroomMember) {
	publishChangeEvents(chatroomability.HandleModifyMembers(members))
}

func handleChatroomMemberDisplayName(displayNames []*messageapi.ModifyChatroomMemberDisplayName) {
	publishChangeEvents(chatroomability.HandleModifyMemberDisplayNames(displayNames))
}
