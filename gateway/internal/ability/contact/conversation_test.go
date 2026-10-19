// 文件名：conversation_test.go
// 主要功能：验证联系人全量和增量的置顶、免打扰、字段缺失及部分掩码处理。
// 作者：2guan
package contactability

import (
	contactapi "github.com/we-clay/we-clay/gateway/internal/api/contact"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"testing"
)

func TestConversationFlags(t *testing.T) {
	c, err := Build(&contactapi.ModifyContact{Username: wrapperspb.String("fixture@chatroom"), BitMask: proto.Uint32(0xffffffff), BitValue: proto.Uint32(0x802), ChatroomNotify: proto.Uint32(0)})
	if err != nil || !c.GetPinned() || !c.GetMuted() {
		t.Fatal("全量状态未解析")
	}
	c = mergeContact(c, &messageapi.ModifyContact{Username: wrapperspb.String(c.Username), BitMask: proto.Uint32(1), BitValue: proto.Uint32(1), Nickname: wrapperspb.String("新名称")})
	if !c.GetPinned() || !c.GetMuted() {
		t.Fatal("无关增量清除了状态")
	}
	c = mergeContact(c, &messageapi.ModifyContact{Username: wrapperspb.String(c.Username), BitMask: proto.Uint32(0x800), BitValue: proto.Uint32(0), ChatroomNotify: proto.Uint32(1)})
	if c.Pinned == nil || c.GetPinned() || c.Muted == nil || c.GetMuted() {
		t.Fatal("明确关闭未合并")
	}
	peer, _ := Build(&contactapi.ModifyContact{Username: wrapperspb.String("fixture-peer"), ChatroomNotify: proto.Uint32(0)})
	if peer.Muted != nil || peer.Pinned != nil {
		t.Fatal("未知状态或私聊被误判")
	}
}

// 验证真实协议成员计数的传递，以及昵称、群主等部分更新不会清空计数。
func TestGroupMemberCountMerge(t *testing.T) {
	c, err := Build(&contactapi.ModifyContact{Username: wrapperspb.String("fixture@chatroom"), ChatroomInfoCount: proto.Uint32(0), ChatroomMaxCount: proto.Uint32(500), Members: &contactapi.ModifyContact_ChatroomMemberData{Count: proto.Uint32(483)}})
	if err != nil || c.GetChatroom().GetMemberCount() != 483 {
		t.Fatal("群人数未使用实际成员计数")
	}
	next := mergeContact(c, &messageapi.ModifyContact{ChatroomOwner: proto.String("fixture-owner")})
	if next.GetChatroom().GetMemberCount() != 483 {
		t.Fatal("群主增量清空了人数")
	}
	next = mergeContact(next, &messageapi.ModifyContact{Members: &messageapi.ModifyContact_ChatroomMemberData{Count: proto.Uint32(484)}})
	if next.GetChatroom().GetMemberCount() != 484 || c.GetChatroom().GetMemberCount() != 483 {
		t.Fatal("成员变化未更新或修改了旧快照")
	}
}
