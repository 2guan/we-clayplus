package contactability

import (
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	sdk "github.com/we-clay/we-clay/sdk/contact"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"sync"
	"testing"
)

func TestConcurrentContactReadsAndSyncUseSnapshots(t *testing.T) {
	instance.cache = map[string]*sdk.Contact{"test-peer": {Username: "test-peer", Nickname: "initial"}}
	var workers sync.WaitGroup
	workers.Go(func() {
		for range 100 {
			HandleModifyContacts([]*messageapi.ModifyContact{{Username: wrapperspb.String("test-peer"), Nickname: wrapperspb.String("server")}})
		}
	})
	workers.Go(func() {
		for range 100 {
			if c := instance.Get("test-peer"); c != nil {
				c.Nickname = "caller"
			}
			for _, c := range instance.List() {
				c.Nickname = "caller"
			}
		}
	})
	workers.Wait()
	if instance.Get("test-peer").Nickname != "server" {
		t.Fatal("caller modified cached contact")
	}
}
