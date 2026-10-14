package sync

import (
	messageability "github.com/we-clay/we-clay/gateway/internal/ability/message"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/contact"
	sdkmessage "github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"testing"
)

type auditMissingContacts struct{ contact.Ability }

func (auditMissingContacts) Get(string) *contact.Contact { return nil }
func TestUncachedSystemSenderRetainsIdentity(t *testing.T) {
	old := contact.Instance
	contact.Instance = auditMissingContacts{}
	defer func() { contact.Instance = old }()
	messageability.SetOutboundReady(true)
	defer messageability.SetOutboundReady(false)
	calls := 0
	var sender, raw string
	p := processor{hooks: Hooks{WithoutPlugins: true, Message: func(m *sdkmessage.Message) error {
		calls++
		sender = m.GetSender().GetUsername()
		raw = m.GetRaw()
		return nil
	}}}
	e := p.handle(&messageapi.SyncResult{NewMessage: []*messageapi.NewMessage{{Sender: wrapperspb.String("synthetic-system"), Receiver: wrapperspb.String("synthetic-self"), Type: proto.Int32(10002), Content: wrapperspb.String(`<sysmsg type="ClientCheckGetExtInfo"><ReportContext>1</ReportContext></sysmsg>`), NewId: proto.Int64(101)}}})
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("OBS type10002 delivered=%v, sender identity lost on cache miss=%v, raw retained=%v", calls == 1, sender == "", raw != "")
	if calls != 1 || sender != "synthetic-system" || raw == "" {
		t.Fatal("system sender lost")
	}
}
