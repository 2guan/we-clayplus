package api

import (
	"bytes"
	"context"
	baseapi "github.com/we-clay/we-clay/gateway/internal/api/base"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGRPCEnvelopeFailureAndNoRetry(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantErr    bool
	}{
		{"success", `{"code":0,"data":{"code":0,"message":"ok"}}`, 200, false},
		{"rejected", `{"code":-1,"message":"rejected"}`, 200, true},
		{"server error", `{"code":0}`, 500, true},
		{"invalid response", `<html>login</html>`, 200, true},
		{"missing status", `{"data":{}}`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			s := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/api/test" {
					t.Errorf("wrong path: %s", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "Bearer test-only" {
					t.Error("missing auth")
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer s.Close()
			c, err := NewGRPCClient(context.Background(), s.Address, "test-only")
			if err != nil {
				t.Fatal(err)
			}
			var out baseapi.OperateResponse
			err = c.Post("/api/test").Body(map[string]string{"value": "test"}).DoProto(&out)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v", err)
			}
			if calls.Load() != 1 {
				t.Fatal("mutation was retried")
			}
		})
	}
}
func TestMultipartFilenameAndRawDownload(t *testing.T) {
	payload := []byte(`{"code":1,"this":"is file data"}`)
	s := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			f, h, e := r.FormFile("file")
			if e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
			defer f.Close()
			b, _ := io.ReadAll(f)
			if h.Filename != "测试 文件.txt" || !bytes.Equal(b, payload) || r.FormValue("receiver") != "test-peer" {
				t.Error("multipart corrupted")
			}
			_, _ = io.WriteString(w, `{"code":0,"data":null}`)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	defer s.Close()
	c, _ := NewGRPCClient(context.Background(), s.Address, "")
	if _, e := c.Post("/api/file").MultipartFiles([]FilePart{{Name: "file", Filename: "测试 文件.txt", Data: payload}}, map[string]string{"receiver": "test-peer"}).Do(); e != nil {
		t.Fatal(e)
	}
	b, e := c.Get("/api/download").Do()
	if e != nil || !bytes.Equal(b, payload) {
		t.Fatal("raw file interpreted as envelope", e)
	}
}
func TestNoRedirectOrRequestAfterEncodingFailure(t *testing.T) {
	var targetCalls atomic.Int32
	target := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	s := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://"+target.Address, 307) }))
	defer s.Close()
	c, _ := NewGRPCClient(context.Background(), s.Address, "")
	if _, e := c.Post("/api/send").Body(map[string]string{"text": "test"}).Do(); e == nil {
		t.Fatal("redirect accepted")
	}
	if _, e := c.Post("/api/send").Body(make(chan int)).Do(); e == nil {
		t.Fatal("invalid body accepted")
	}
	if targetCalls.Load() != 0 {
		t.Fatal("redirect followed")
	}
}

func TestCoreErrorDetailIsRetainedWithoutDumpingBody(t *testing.T) {
	_, err := decodeResponse(400, "application/json", []byte(`{"code":-1,"message":"missing original client_id","data":{"private":"not-for-errors"}}`))
	if err == nil || !strings.Contains(err.Error(), "missing original client_id") || strings.Contains(err.Error(), "not-for-errors") {
		t.Fatal("error envelope detail was lost or body leaked")
	}
	_, err = decodeResponse(500, "text/html", []byte(`<html>not-for-errors</html>`))
	if err == nil || strings.Contains(err.Error(), "not-for-errors") {
		t.Fatal("raw error body leaked")
	}
}
