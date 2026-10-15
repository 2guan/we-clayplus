package messageapi

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/api/base"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"google.golang.org/protobuf/proto"
	"io"
	"net/http"
	"strings"
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
			_, _ = io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0},"new_id":"9007199254740993","create_time":42}}`)
		case "/api/message/forward/image", "/api/message/forward/video", "/api/message/forward/file":
			var q map[string]string
			if e := json.NewDecoder(r.Body).Decode(&q); e != nil {
				t.Error(e)
			}
			if q["xml"] != "<msg>synthetic</msg>" || q["receiver"] != "test-peer" {
				t.Error("wrong forward request")
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0}}}`)
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
			_, _ = io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0}}}`)
		case "/api/message/download/file":
			var q struct {
				AttachID     string `json:"attach_id"`
				Size, Offset uint32
				ChunkSize    uint32 `json:"chunk_size"`
			}
			if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
				t.Error(err)
			}
			if q.AttachID != "test-attach" || q.Size != 40000 || (q.Offset != 0 && q.Offset != 32768) || q.ChunkSize != min(uint32(32768), q.Size-q.Offset) {
				t.Error("wrong attachment request")
			}
			data := bytes.Repeat([]byte{byte(q.Offset/32768 + 1)}, int(q.ChunkSize))
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": &DownloadFileResponse{BaseResponse: &baseapi.BaseResponse{Code: proto.Int32(0)}, Size: proto.Uint32(q.Size), Offset: proto.Uint32(q.Offset), ChunkSize: proto.Uint32(q.ChunkSize), Chunk: &baseapi.Buffer{Size: proto.Uint32(q.ChunkSize), Data: data}}})
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
	file, e := w.DownloadFile("test-peer", "test-attach", "", 40000)
	if e != nil {
		t.Fatal(e)
	}
	want := append(bytes.Repeat([]byte{1}, 32768), bytes.Repeat([]byte{2}, 7232)...)
	if !bytes.Equal(file.GetChunk().GetData(), want) || file.GetOffset() != 0 {
		t.Fatal("file chunks were not assembled")
	}
	i, e := w.DownloadImg("test-peer", "test-file-id", "test-key", 5)
	if e != nil || string(i.GetChunk().GetData()) != "media" {
		t.Fatal(e)
	}
	v, e := w.DownloadVideo("test-peer", "test-file-id", "test-key", 5)
	if e != nil || string(v.GetChunk().GetData()) != "media" {
		t.Fatal(e)
	}
	if len(paths) != 9 {
		t.Fatalf("unexpected request count %d", len(paths))
	}
	if _, e = w.DownloadFile("test-peer", "test-attach", "wrong-key", 5); e == nil {
		t.Fatal("invalid attachment accepted")
	}
	if len(paths) != 9 {
		t.Fatal("invalid input reached core")
	}
}

func TestLargeAttachmentChunkUsesBoundedCoreRequest(t *testing.T) {
	const total uint32 = 300 << 20
	const offset uint32 = 131072
	const chunkSize uint32 = 64 << 10
	requests := 0
	s := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/message/download/file" {
			t.Errorf("wrong core route %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var q struct {
			AppID     string `json:"app_id"`
			AttachID  string `json:"attach_id"`
			Username  string `json:"username"`
			Size      uint32 `json:"size"`
			Offset    uint32 `json:"offset"`
			ChunkSize uint32 `json:"chunk_size"`
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil || q.AppID != "synthetic-app" || q.AttachID != "test-attach" ||
			q.Username != "test-peer" || q.Size != total || q.Offset != offset || q.ChunkSize != chunkSize {
			t.Errorf("wrong large attachment request: err=%v size=%d offset=%d chunk=%d", err, q.Size, q.Offset, q.ChunkSize)
		}
		data := make([]byte, chunkSize)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": &DownloadFileResponse{
			BaseResponse: &baseapi.BaseResponse{Code: proto.Int32(0)}, Size: proto.Uint32(total),
			Offset: proto.Uint32(offset), ChunkSize: proto.Uint32(chunkSize),
			Chunk: &baseapi.Buffer{Size: proto.Uint32(chunkSize), Data: data},
		}})
	}))
	defer s.Close()
	c, _ := api.NewGRPCClient(context.Background(), s.Address, "")
	w := remote{client: c}
	resp, err := w.DownloadFileChunk("test-peer", "test-attach", "synthetic-app", total, offset, chunkSize)
	if err != nil || len(resp.GetChunk().GetData()) != int(chunkSize) || requests != 1 {
		t.Fatalf("large chunk failed: err=%v requests=%d", err, requests)
	}
	if _, err := w.DownloadFileChunk("test-peer", "test-attach", "synthetic-app", total, total-1, chunkSize); err == nil || requests != 1 {
		t.Fatalf("invalid final range reached core: err=%v requests=%d", err, requests)
	}
	if _, err := w.DownloadFileChunk("test-peer", "test-attach", "synthetic-app", total, offset, chunkSize+1); err == nil || requests != 1 {
		t.Fatalf("CGI chunk over 64 KiB reached core: err=%v requests=%d", err, requests)
	}
}

func TestCDNAttachmentChunkUsesBinaryCoreRoute(t *testing.T) {
	const offset uint32 = 256 << 10
	const chunkSize uint32 = 256 << 10
	bigCDNID := "7f" + strings.Repeat("00", 113)
	normalCDNID := "05" + strings.Repeat("00", 113)
	requests := 0
	s := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/api/cdn/download/attachment/chunk" {
			t.Errorf("wrong CDN attachment route: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var q struct {
			FileID    string `json:"file_id"`
			AESKey    string `json:"aes_key"`
			Offset    uint32 `json:"offset"`
			ChunkSize uint32 `json:"chunk_size"`
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil || q.FileID != bigCDNID ||
			q.AESKey != "00112233445566778899aabbccddeeff" || q.Offset != offset || q.ChunkSize != chunkSize {
			t.Errorf("wrong CDN chunk request: err=%v offset=%d chunk=%d", err, q.Offset, q.ChunkSize)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("synthetic chunk"))
	}))
	defer s.Close()
	c, err := api.NewGRPCClient(context.Background(), s.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	w := remote{client: c}
	data, err := w.DownloadCDNAttachmentChunk(bigCDNID, "00112233445566778899aabbccddeeff", offset, chunkSize)
	if err != nil || string(data) != "synthetic chunk" || requests != 1 {
		t.Fatalf("CDN chunk failed: bytes=%d err=%v requests=%d", len(data), err, requests)
	}
	if _, err := w.DownloadCDNAttachmentChunk(bigCDNID, "invalid-key", 0, 1); err == nil || requests != 1 {
		t.Fatalf("invalid key reached core: err=%v requests=%d", err, requests)
	}
	if _, err := w.DownloadCDNAttachmentChunk(bigCDNID, "00112233445566778899aabbccddeeff", 0, 6659); err == nil || requests != 1 {
		t.Fatalf("unaligned encrypted range reached core: err=%v requests=%d", err, requests)
	}
	if _, err := w.DownloadCDNAttachmentChunk(bigCDNID, "00112233445566778899aabbccddeeff", 0, chunkSize+16); err == nil || requests != 1 {
		t.Fatalf("CDN chunk over 256 KiB reached core: err=%v requests=%d", err, requests)
	}
	if _, err := w.DownloadCDNAttachmentChunk(normalCDNID, "00112233445566778899aabbccddeeff", 0, chunkSize); err == nil || requests != 1 {
		t.Fatalf("ordinary CDN chunk over 64 KiB reached core: err=%v requests=%d", err, requests)
	}
}

func TestCDNAttachmentChunkLimitRequiresExactType7ID(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
		want uint32
	}{
		{"type7", "7f" + strings.Repeat("00", 113), 256 << 10},
		{"type5", "05" + strings.Repeat("00", 113), 64 << 10},
		{"wrong length", "7f" + strings.Repeat("00", 112), 64 << 10},
		{"invalid hex", "7f" + strings.Repeat("00", 112) + "zz", 64 << 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CDNAttachmentChunkLimit(tc.id); got != tc.want {
				t.Fatalf("chunk limit=%d, want %d", got, tc.want)
			}
		})
	}
}

func TestCDNAttachmentAllowsFinalPaddingAtOneGiB(t *testing.T) {
	const offset uint32 = maxAttachmentDownloadSize - 16
	const chunkSize uint32 = 32
	requests := 0
	s := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var q struct {
			Offset    uint32 `json:"offset"`
			ChunkSize uint32 `json:"chunk_size"`
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil || q.Offset != offset || q.ChunkSize != chunkSize {
			t.Errorf("wrong final encrypted range: offset=%d chunk=%d err=%v", q.Offset, q.ChunkSize, err)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("final"))
	}))
	defer s.Close()
	c, err := api.NewGRPCClient(context.Background(), s.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	w := remote{client: c}
	id := "7f" + strings.Repeat("00", 113)
	key := strings.Repeat("00", 16)
	if _, err := w.DownloadCDNAttachmentChunk(id, key, offset, chunkSize); err != nil || requests != 1 {
		t.Fatalf("final padding range rejected: err=%v requests=%d", err, requests)
	}
	if _, err := w.DownloadCDNAttachmentChunk(id, key, offset, chunkSize+16); err == nil || requests != 1 {
		t.Fatalf("range beyond ciphertext cap reached core: err=%v requests=%d", err, requests)
	}
}
