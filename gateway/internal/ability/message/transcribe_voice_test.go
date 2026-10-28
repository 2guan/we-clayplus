package messageability

import (
	"context"
	"errors"
	"testing"

	api "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/contact"
	sdk "github.com/we-clay/we-clay/sdk/message"
)

type transcribeAPI struct {
	api.MessageService
	input api.VoiceTranscribeInput
	calls int
	err   error
}

func (f *transcribeAPI) TranscribeVoice(_ context.Context, p api.VoiceTranscribeInput) (string, error) {
	f.calls++
	f.input = p
	return "离线转写", f.err
}

func TestTranscribeVoiceUsesOriginalVoiceIDsAndPropagatesError(t *testing.T) {
	f := &transcribeAPI{}
	a := &ability{api: f}
	m := &sdk.Message{
		Id: 9007199254740993, Type: sdk.TypeVoice,
		Sender: &contact.Contact{Username: "test-room@chatroom"},
		Raw:    `{"id":7,"new_id":9007199254740993}`,
		Data:   &sdk.Message_Voice{Voice: &sdk.VoiceData{Media: &sdk.Media{Size: 4}}},
	}
	got, err := a.TranscribeVoice(context.Background(), m)
	if err != nil || got != "离线转写" || f.calls != 1 || f.input.MessageID != 7 || f.input.NewID != 9007199254740993 || f.input.Size != 4 || f.input.Format != 4 || f.input.ChatroomID != "test-room@chatroom" {
		t.Fatalf("text=%q input=%+v calls=%d err=%v", got, f.input, f.calls, err)
	}
	f.err = errors.New("synthetic core failure")
	if _, err := a.TranscribeVoice(context.Background(), m); !errors.Is(err, f.err) {
		t.Fatalf("core error lost: %v", err)
	}
	if f.calls != 2 {
		t.Fatal("core mock was not called")
	}
}

func TestTranscribeVoiceRejectsInvalidMessageBeforeCore(t *testing.T) {
	f := &transcribeAPI{}
	a := &ability{api: f}
	m := &sdk.Message{
		Id: 9007, Type: sdk.TypeVoice, Raw: `{"id":7,"new_id":9007}`,
		Data: &sdk.Message_Voice{Voice: &sdk.VoiceData{Media: &sdk.Media{Size: 4}}},
	}
	for _, item := range []*sdk.Message{
		nil,
		{Id: 9007, Type: sdk.TypeText},
		{Id: 9007, Type: sdk.TypeVoice, Data: &sdk.Message_Voice{Voice: &sdk.VoiceData{Media: &sdk.Media{}}}},
		{Id: 9007, Type: sdk.TypeVoice, Raw: `{"id":7,"new_id":9008}`, Data: &sdk.Message_Voice{Voice: &sdk.VoiceData{Media: &sdk.Media{Size: 4}}}},
		{Id: 9007, Type: sdk.TypeVoice, Raw: `{"id":7,"new_id":9007}`, Data: &sdk.Message_Voice{Voice: &sdk.VoiceData{Media: &sdk.Media{Size: 4}, Format: func() *int32 { v := int32(2); return &v }()}}},
	} {
		if _, err := a.TranscribeVoice(context.Background(), item); err == nil {
			t.Fatalf("accepted invalid message: %+v", item)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.TranscribeVoice(ctx, m); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
	if f.calls != 0 {
		t.Fatalf("invalid message reached core: %d", f.calls)
	}
}
