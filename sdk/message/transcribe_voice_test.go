package message

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type transcribeRecorder struct {
	Ability
	message *Message
	err     error
}

func (r *transcribeRecorder) TranscribeVoice(_ context.Context, m *Message) (string, error) {
	r.message = m
	return "离线转写", r.err
}

func TestTranscribeVoicePluginGRPC(t *testing.T) {
	r := &transcribeRecorder{}
	c := Client{Client: dialMessage(t, &Server{Impl: r}, false)}
	m := &Message{Id: 9007, Type: TypeVoice, Data: &Message_Voice{Voice: &VoiceData{Media: &Media{Size: 4}}}}
	text, err := c.TranscribeVoice(context.Background(), m)
	if err != nil || text != "离线转写" || r.message.GetId() != 9007 {
		t.Fatalf("text=%q message=%+v err=%v", text, r.message, err)
	}
	r.err = errors.New("synthetic transcription failure")
	if _, err := c.TranscribeVoice(context.Background(), m); status.Code(err) != codes.Unknown {
		t.Fatalf("backend error lost: %v", err)
	}
	if _, err := c.Client.TranscribeVoice(context.Background(), &TranscribeVoice_Request{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing voice accepted: %v", err)
	}
}

func TestVoiceTranscriptionDoesNotRequireLegacyAbilityImplementations(t *testing.T) {
	type legacyAbility struct{ Ability }
	var old Ability = legacyAbility{}
	_, err := (Server{Impl: old}).TranscribeVoice(context.Background(), &TranscribeVoice_Request{Message: &Message{Type: TypeVoice}})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("old ability should remain valid and report unsupported extension: %v", err)
	}
}
