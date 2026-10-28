package ability_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/we-clay/we-clay/sdk/cdn"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestMomentsVideoCDNThroughCoreAndPlugin(t *testing.T) {
	video := []byte("synthetic video")
	thumb := []byte("synthetic thumbnail")
	media := bytes.Repeat([]byte("synthetic video bytes"), 300000) // 超过 gRPC 默认的 4 MiB 接收限制。
	const videoURL = "https://example.invalid/video?token=a+b/c==&part=2"
	const encKey = uint64(9007199254740999)
	var uploads, downloads atomic.Int32
	withCore(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cdn/upload/moments/video":
			uploads.Add(1)
			if r.Method != http.MethodPost {
				t.Error("wrong upload method")
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				return
			}
			if len(r.MultipartForm.File) != 2 || len(r.MultipartForm.Value) != 0 {
				t.Error("unexpected multipart fields")
			}
			for name, want := range map[string][]byte{"video": video, "thumb": thumb} {
				file, _, err := r.FormFile(name)
				if err != nil {
					t.Error(err)
					continue
				}
				got, err := io.ReadAll(file)
				file.Close()
				if err != nil || !bytes.Equal(got, want) {
					t.Errorf("%s upload changed: %v", name, err)
				}
			}
			io.WriteString(w, `{"code":0,"data":{"file_url":"https://example.invalid/video","thumb_url":"https://example.invalid/thumb","file_id":"test-video-id","file_key":"test-file-key"}}`)
		case "/api/cdn/download/moments/video":
			downloads.Add(1)
			if r.Method != http.MethodGet || r.URL.Query().Get("url") != videoURL || r.URL.Query().Get("key") != fmt.Sprint(encKey) {
				t.Error("SNS video download parameters changed")
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(media)
		default:
			t.Errorf("unexpected route: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.MaxRecvMsgSize(64 << 20))
	cdn.RegisterCDNServiceServer(server, &cdn.Server{Impl: cdn.Instance})
	go server.Serve(listener)
	t.Cleanup(func() { server.Stop(); listener.Close() })
	conn, err := grpc.NewClient("passthrough:///moments-video-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, client := range []cdn.Ability{cdn.Instance, cdn.GRPCClient{Client: cdn.NewCDNServiceClient(conn)}} {
		out, err := client.UploadMomentsVideo(video, thumb)
		if err != nil || out.GetFileUrl() != "https://example.invalid/video" || out.GetThumbUrl() != "https://example.invalid/thumb" || out.GetFileId() != "test-video-id" || out.GetFileKey() != "test-file-key" {
			t.Fatalf("SNS video upload receipt changed: %v, %v", out, err)
		}
		got, err := client.DownloadMomentsVideo(videoURL, encKey)
		if err != nil || !bytes.Equal(got, media) {
			t.Fatalf("SNS video download changed: %d bytes, %v", len(got), err)
		}
	}
	if uploads.Load() != 2 || downloads.Load() != 2 {
		t.Fatalf("unexpected request count: %d uploads, %d downloads", uploads.Load(), downloads.Load())
	}
}

func TestMomentsVideoCDNRejectsUnconfirmedResults(t *testing.T) {
	var response, contentType string
	var calls atomic.Int32
	withCore(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", contentType)
		io.WriteString(w, response)
	})
	for _, body := range []string{
		`{"code":0,"data":null}`,
		`{"code":0,"data":{}}`,
		`{"code":0,"data":{"file_url":"https://example.invalid/video","thumb_url":"https://example.invalid/thumb","file_id":"test-id"}}`,
		`{"code":-5103015,"message":"upload rejected"}`,
	} {
		response, contentType = body, "application/json"
		before := calls.Load()
		if result, err := cdn.Instance.UploadMomentsVideo([]byte("video"), []byte("thumb")); err == nil || result != nil {
			t.Fatalf("unconfirmed upload accepted: %v, %v", result, err)
		}
		if calls.Load() != before+1 {
			t.Fatal("upload request retried")
		}
	}
	for _, tc := range []struct{ body, kind string }{
		{`{"code":0,"data":{"video":"not-media"}}`, "application/json"},
		{"", "application/octet-stream"},
	} {
		response, contentType = tc.body, tc.kind
		before := calls.Load()
		if result, err := cdn.Instance.DownloadMomentsVideo("https://example.invalid/video", 42); err == nil || result != nil {
			t.Fatalf("invalid download accepted: %d bytes, %v", len(result), err)
		}
		if calls.Load() != before+1 {
			t.Fatal("download request retried")
		}
	}
}
