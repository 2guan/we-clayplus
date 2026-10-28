package messageapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
)

func TestTranscribeVoiceCoreRequestAndError(t *testing.T) {
	const id = uint64(9007199254740993)
	calls := 0
	s := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/message/transcribe/voice" || r.Method != "POST" {
			t.Errorf("wrong core route: %s %s", r.Method, r.URL.Path)
		}
		var q struct {
			MessageID  uint32 `json:"msg_id"`
			NewID      string `json:"new_id"`
			Size       uint32 `json:"size"`
			Format     int32  `json:"voice_format"`
			ChatroomID string `json:"chatroom_id"`
			BufferID   int64  `json:"buffer_id"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&q); err != nil || q.MessageID != 7 || q.NewID != "9007199254740993" || q.Size != 4 || q.Format != 4 || q.ChatroomID != "test-room@chatroom" || q.BufferID != 19 {
			t.Errorf("voice identity/size changed: %+v: %v", q, err)
		}
		if calls == 1 {
			_, _ = io.WriteString(w, `{"code":0,"data":{"text":"离线转写"}}`)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":-1,"message":"转写失败"}`)
	}))
	defer s.Close()
	c, err := api.NewGRPCClient(context.Background(), s.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := remote{client: c}
	input := VoiceTranscribeInput{MessageID: 7, NewID: id, Size: 4, Format: 4, ChatroomID: "test-room@chatroom", BufferID: 19}
	got, err := r.TranscribeVoice(context.Background(), input)
	if err != nil || got != "离线转写" {
		t.Fatalf("text=%q err=%v", got, err)
	}
	if _, err = r.TranscribeVoice(context.Background(), input); err == nil || !strings.Contains(err.Error(), "转写失败") {
		t.Fatalf("core error lost: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}
