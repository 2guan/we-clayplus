package messageapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"io"
	"net/http"
	"testing"
)

func TestGRPCMediaAdapters(t *testing.T) {
	const id = uint64(9007199254740993)
	var paths []string
	s := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/message/file":
			f, h, e := r.FormFile("file")
			if e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
			defer f.Close()
			b, _ := io.ReadAll(f)
			if h.Filename != "示例.txt" || string(b) != "test-file" || r.FormValue("receiver") != "test-peer" {
				t.Error("wrong file request")
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"new_id":"9007199254740993","create_time":42}}`)
		case "/api/message/forward/image", "/api/message/forward/video", "/api/message/forward/file":
			var q map[string]string
			if e := json.NewDecoder(r.Body).Decode(&q); e != nil {
				t.Error(e)
			}
			if q["xml"] != "<msg>synthetic</msg>" || q["receiver"] != "test-peer" {
				t.Error("wrong forward request")
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{}}`)
		case "/api/message/download/voice":
			var q struct {
				NewID      uint64 `json:"new_id"`
				ChatroomID string `json:"chatroom_id"`
				Length     uint32 `json:"length"`
			}
			d := json.NewDecoder(r.Body)
			d.DisallowUnknownFields()
			if e := d.Decode(&q); e != nil || q.NewID != id || q.Length != 123 {
				t.Error("voice identity lost", e)
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{}}`)
		case "/api/message/download/file":
			var q map[string]json.RawMessage
			_ = json.NewDecoder(r.Body).Decode(&q)
			if string(q["attach_id"]) != `"test-attach"` || string(q["chunk_size"]) != "32768" {
				t.Error("wrong attachment request")
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{}}`)
		case "/api/cdn/download/image", "/api/cdn/download/video":
			if r.URL.Query().Get("id") != "test-file-id" || r.URL.Query().Get("key") != "test-key" {
				t.Error("wrong CDN parameters")
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "media")
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	c, _ := api.NewGRPCClient(context.Background(), s.Address, "")
	old := api.GetCore
	api.GetCore = func() *api.Client { return c }
	t.Cleanup(func() { api.GetCore = old })
	w := remote{}
	result, e := w.SendFile("test-peer", "示例.txt", bytes.NewBufferString("test-file"))
	if e != nil || result.GetNewId() != id {
		t.Fatal("file response ID lost", e)
	}
	if _, e = w.ForwardImage("test-peer", bytes.NewBufferString("<msg>synthetic</msg>")); e != nil {
		t.Fatal(e)
	}
	if _, e = w.ForwardVideo("test-peer", bytes.NewBufferString("<msg>synthetic</msg>")); e != nil {
		t.Fatal(e)
	}
	if _, e = w.ForwardFile("test-peer", "<msg>synthetic</msg>"); e != nil {
		t.Fatal(e)
	}
	if _, e = w.DownloadVoice("test-room", "9007199254740993", "", 123, 5); e != nil {
		t.Fatal(e)
	}
	if _, e = w.DownloadFile("test-peer", "test-attach", "", 40000); e != nil {
		t.Fatal(e)
	}
	i, e := w.DownloadImg("test-peer", "test-file-id", "test-key", 5)
	if e != nil || string(i.GetChunk().GetData()) != "media" {
		t.Fatal(e)
	}
	v, e := w.DownloadVideo("test-peer", "test-file-id", "test-key", 5)
	if e != nil || string(v.GetChunk().GetData()) != "media" {
		t.Fatal(e)
	}
	if len(paths) != 8 {
		t.Fatalf("unexpected request count %d", len(paths))
	}
	if _, e = w.DownloadFile("test-peer", "test-attach", "wrong-key", 5); e == nil {
		t.Fatal("invalid attachment accepted")
	}
	if len(paths) != 8 {
		t.Fatal("invalid input reached core")
	}
}
