package dashboard

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/sdk/control"
)

type fileStreamHost struct {
	reader io.ReadCloser
	calls  int
}

func (h *fileStreamHost) Execute(_ context.Context, c control.Command) (any, error) {
	if c.Operation != "message-file" {
		return nil, errors.New("unexpected operation")
	}
	h.calls++
	return h.reader, nil
}

type trackedFileReader struct {
	io.Reader
	closed bool
}

func (r *trackedFileReader) Close() error { r.closed = true; return nil }

type failingFileReader struct{}

func (failingFileReader) Read([]byte) (int, error) {
	return 0, errors.New("fixture checksum failure")
}

func setupFileStream(t *testing.T, reader io.Reader) (*Server, Message, *fileStreamHost, *trackedFileReader) {
	t.Helper()
	s := newServer(t, false)
	r := &trackedFileReader{Reader: reader}
	h := &fileStreamHost{reader: r}
	s.host = h
	m := Message{ID: "fixture-file", Target: "fixture-peer", Kind: "file", Content: "report.bin"}
	if err := s.store.change(func(st *State) error {
		st.Account.ID = "fixture-self"
		st.Messages = []Message{m}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.putSecret(s.messageMediaKey(m), `{}`); err != nil {
		t.Fatal(err)
	}
	return s, m, h, r
}

func TestLargeFileDownloadAndCachedRange(t *testing.T) {
	data := strings.Repeat("attachment", (17<<20)/10+1)
	s, m, host, reader := setupFileStream(t, strings.NewReader(data))
	w := httptest.NewRecorder()
	request := "/api/message-media?target=" + m.Target + "&id=" + m.ID
	s.messageMedia(w, httptest.NewRequest("GET", request, nil))
	if w.Code != 200 || w.Body.String() != data || !reader.closed {
		t.Fatalf("large download failed: status=%d length=%d closed=%v", w.Code, w.Body.Len(), reader.closed)
	}
	if host.calls != 1 || s.store.snapshot().Messages[0].Media == "" {
		t.Fatal("download was not cached")
	}
	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", request, nil)
	r.Header.Set("Range", "bytes=7-25")
	s.messageMedia(w, r)
	if w.Code != 206 || w.Body.String() != data[7:26] || host.calls != 1 {
		t.Fatal("cached range downloaded again or returned wrong bytes")
	}
	info, err := os.Stat(filepath.Join(s.cfg.DataDir, "media", filepath.Base(s.store.snapshot().Messages[0].Media)))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("cached file permissions", err)
	}
}

func TestFileDownloadFailureDiscardsPartialCache(t *testing.T) {
	s, m, _, reader := setupFileStream(t, io.MultiReader(strings.NewReader("partial"), failingFileReader{}))
	w := httptest.NewRecorder()
	s.messageMedia(w, httptest.NewRequest("GET", "/api/message-media?target="+m.Target+"&id="+m.ID, nil))
	if w.Code != 502 || !strings.Contains(w.Body.String(), "fixture checksum failure") || !reader.closed {
		t.Fatal("stream failure was not returned or reader leaked")
	}
	if s.store.snapshot().Messages[0].Media != "" {
		t.Fatal("partial download marked as complete")
	}
	parts, _ := filepath.Glob(filepath.Join(s.cfg.DataDir, ".download-*"))
	files, _ := filepath.Glob(filepath.Join(s.cfg.DataDir, "media", "*"))
	if len(parts)+len(files) != 0 {
		t.Fatal("partial file survived")
	}
}

func TestFileDownloadCancelledBeforePublication(t *testing.T) {
	s, m, _, reader := setupFileStream(t, strings.NewReader("complete"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.cacheMessageFile(ctx, m); !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation", err)
	}
	if !reader.closed || s.store.snapshot().Messages[0].Media != "" {
		t.Fatal("cancelled download leaked or became available")
	}
}
