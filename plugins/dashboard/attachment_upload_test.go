package dashboard

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/sdk/control"
)

type attachmentZeros struct{}

func (attachmentZeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func attachmentRequest(s *Server, body io.Reader, size int64) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/attachments/send?target=mock-contact-0&name=test.bin", body)
	r.ContentLength = size
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestAttachment500MiBBoundaryAndConfig(t *testing.T) {
	s := newServer(t, true)
	if attachmentLimit(s.store.snapshot()) != 500<<20 {
		t.Fatal("默认附件上限不是 500 MiB")
	}
	if e := validateSettings("attachments", map[string]any{"max_file_mb": float64(500)}); e != nil {
		t.Fatal(e)
	}
	for _, n := range []float64{0, 1025, 1.5} {
		if validateSettings("attachments", map[string]any{"max_file_mb": n}) == nil {
			t.Fatal("允许了无效附件大小", n)
		}
	}
	if w := attachmentRequest(s, strings.NewReader(""), (500<<20)+1); w.Code != 413 {
		t.Fatal("超过默认边界仍接受", w.Code)
	}
	before := len(s.store.snapshot().Messages)
	w := attachmentRequest(s, io.LimitReader(attachmentZeros{}, 500<<20), 500<<20)
	if w.Code != 200 {
		t.Fatal("500 MiB 流式上传失败", w.Code, w.Body.String())
	}
	messages := s.store.snapshot().Messages
	if len(messages) != before+1 || messages[before].Status != "sent" || messages[before].Kind != "file" {
		t.Fatal("Mock 附件没有正确记录")
	}
	p := filepath.Join(s.cfg.DataDir, "media", mediaName(messages[before].Media))
	info, e := os.Stat(p)
	if e != nil || info.Size() != 500<<20 {
		t.Fatal("落盘大小不完整", e)
	}
	_ = s.store.change(func(st *State) error { st.Messages[before].Status = "unknown"; return nil })
	if e = s.resend(context.Background(), actionRequest{ID: messages[before].ID, Target: "mock-contact-0", Enabled: true}); e != nil {
		t.Fatal("500 MiB 归档重发仍经过小媒体限制", e)
	}
	retried := s.store.snapshot().Messages
	if len(retried) != before+1 || retried[before].Media != messages[before].Media || retried[before].Status != "sent" {
		t.Fatal("重发没有复用原文件或没有替换原失败记录")
	}
	if e = s.store.change(func(st *State) error { st.Settings["attachments.max_file_mb"] = float64(1); return nil }); e != nil {
		t.Fatal(e)
	}
	if w = attachmentRequest(s, io.LimitReader(attachmentZeros{}, (1<<20)+1), -1); w.Code != 413 {
		t.Fatal("未声明大小时绕过新上限", w.Code)
	}
	if e = s.send(context.Background(), actionRequest{Target: "mock-contact-0", Group: "file", Content: "inline.bin", Media: base64.StdEncoding.EncodeToString(make([]byte, (1<<20)+1))}, "manual"); e == nil {
		t.Fatal("旧 JSON 入口绕过配置")
	}
	if e = s.resend(context.Background(), actionRequest{ID: messages[before].ID, Target: "mock-contact-0"}); e == nil {
		t.Fatal("已发送消息允许直接重发")
	}
	_ = s.store.change(func(st *State) error { st.Messages[before].Status = "unknown"; return nil })
	if e = s.resend(context.Background(), actionRequest{ID: retried[before].ID, Target: "mock-contact-0", Enabled: true}); e == nil {
		t.Fatal("重发绕过调低后的配置")
	}
	parts, _ := filepath.Glob(filepath.Join(s.cfg.DataDir, ".download-upload-*.part"))
	if len(parts) != 0 {
		t.Fatal("失败上传遗留临时文件")
	}
}

func TestAttachmentEndpointRetainsAuthAndOriginChecks(t *testing.T) {
	s := newServer(t, false)
	if w := attachmentRequest(s, strings.NewReader("synthetic"), 9); w.Code != 401 {
		t.Fatal("未登录可上传附件", w.Code)
	}
	s = newServer(t, true)
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/attachments/send?target=mock-contact-0&name=test.bin", strings.NewReader("synthetic"))
	r.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("跨站可上传附件", w.Code)
	}
}

type attachmentPreflightHost struct{ calls []string }

func (h *attachmentPreflightHost) Execute(_ context.Context, c control.Command) (any, error) {
	h.calls = append(h.calls, c.Operation)
	return nil, fmt.Errorf("当前 core 发送链路最多支持 32 MB")
}

func TestAttachmentCorePreflightAndInterruptedUpload(t *testing.T) {
	s := newServer(t, true)
	s.cfg.Mock = false
	h := &attachmentPreflightHost{}
	s.host = h
	before := len(s.store.snapshot().Messages)
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/attachments/send?target=mock-contact-0&name=test.bin", strings.NewReader(""))
	r.ContentLength = 33 << 20
	w := httptest.NewRecorder()
	s.sendAttachment(w, r)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "32 MB") || len(h.calls) != 1 || h.calls[0] != "file-send-check" || len(s.store.snapshot().Messages) != before {
		t.Fatal("不支持的 core 仍尝试上传或发送", w.Code, h.calls)
	}
	s.cfg.Mock = true
	w = attachmentRequest(s, bytes.NewReader([]byte("partial")), 100)
	if w.Code != 400 || len(s.store.snapshot().Messages) != before {
		t.Fatal("未完整上传仍发送")
	}
	parts, _ := filepath.Glob(filepath.Join(s.cfg.DataDir, ".download-upload-*.part"))
	if len(parts) != 0 {
		t.Fatal("中断上传遗留临时文件")
	}
}
