package chatroomability

import (
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	sdk "github.com/we-clay/we-clay/sdk/chatroom"
	"google.golang.org/protobuf/proto"
	"sync"
	"testing"
)

func TestConcurrentGroupReadsAndSyncUseSnapshots(t *testing.T) {
	const room = "test-room@chatroom"
	instance.cache = map[string]map[string]*sdk.Member{room: {"test-peer": {Username: "test-peer"}}}
	var workers sync.WaitGroup
	workers.Go(func() {
		for range 100 {
			HandleModifyMemberDisplayNames([]*messageapi.ModifyChatroomMemberDisplayName{{Chatroom: proto.String(room), Username: proto.String("test-peer"), DisplayName: proto.String("server")}})
		}
	})
	workers.Go(func() {
		for range 100 {
			instance.GetMember(room, "test-peer").DisplayName = "caller"
			for _, m := range instance.ListMembers(room) {
				m.DisplayName = "caller"
			}
			for _, m := range instance.Members(room) {
				m.DisplayName = "caller"
			}
		}
	})
	workers.Wait()
	if instance.GetMember(room, "test-peer").DisplayName != "server" {
		t.Fatal("caller modified cached member")
	}
}
