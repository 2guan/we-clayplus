package messageability

import (
	"errors"
	"testing"

	baseapi "github.com/we-clay/we-clay/gateway/internal/api/base"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"google.golang.org/protobuf/proto"
)

type patAPI struct {
	messageapi.MessageService
	calls int
	resp  *messageapi.SendPatResponse
	err   error
}

func (a *patAPI) Pat(receiver, target string) (*messageapi.SendPatResponse, error) {
	a.calls++
	if receiver != "synthetic_friend" || target != "synthetic_friend" {
		return nil, errors.New("unexpected pat recipient")
	}
	return a.resp, a.err
}

func TestPatRequiresUnambiguousSuccess(t *testing.T) {
	SetOutboundReady(true)
	for _, tc := range []struct {
		name    string
		resp    *messageapi.SendPatResponse
		err     error
		wantErr bool
	}{
		{"confirmed", &messageapi.SendPatResponse{BaseResponse: &baseapi.BaseResponse{Code: proto.Int32(0)}, Id: proto.Uint64(9007199254740993)}, nil, false},
		{"missing-status", &messageapi.SendPatResponse{Id: proto.Uint64(1)}, nil, true},
		{"rejected", &messageapi.SendPatResponse{BaseResponse: &baseapi.BaseResponse{Code: proto.Int32(-7)}}, nil, true},
		{"transport-uncertain", nil, errors.New("synthetic network failure"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &patAPI{resp: tc.resp, err: tc.err}
			resp, err := (&ability{api: api}).Pat("synthetic_friend", "synthetic_friend")
			if (err != nil) != tc.wantErr || api.calls != 1 {
				t.Fatalf("result=%+v err=%v calls=%d", resp, err, api.calls)
			}
			if !tc.wantErr && resp.GetId() != 9007199254740993 {
				t.Fatalf("lost exact pat ID: %d", resp.GetId())
			}
		})
	}
}
