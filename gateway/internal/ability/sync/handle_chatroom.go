package sync

import (
	chatroomability "github.com/we-clay/we-clay/gateway/internal/ability/chatroom"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
)

func (p *processor) handleChatroomMember(members []*messageapi.ModifyChatroomMember) error {
	return p.publishChangeEvents(chatroomability.HandleModifyMembers(members))
}

func (p *processor) handleChatroomMemberDisplayName(displayNames []*messageapi.ModifyChatroomMemberDisplayName) error {
	return p.publishChangeEvents(chatroomability.HandleModifyMemberDisplayNames(displayNames))
}
