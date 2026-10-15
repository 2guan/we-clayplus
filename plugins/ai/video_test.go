// 文件名：video_test.go
// 主要功能：验证视频创建、轮询、下载的真实 HTTP 契约，以及超时、取消、错误状态与认证边界。
// 作者：2guan
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestVideoLifecycle(t *testing.T) {
	var posts, polls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("缺少服务端认证")
		}
		switch r.URL.Path {
		case "/videos":
			posts.Add(1)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["seconds"] != "8" || body["size"] != "720x1280" || body["model"] != "fixture-video" {
				t.Errorf("参数未传递 %v", body)
			}
			ref, ok := body["input_reference"].(map[string]any)
			if !ok || !strings.HasPrefix(fmt.Sprint(ref["image_url"]), "data:image/png;base64,") {
				t.Error("参考图未传递")
			}
			fmt.Fprint(w, `{"id":"video_test","status":"queued"}`)
		case "/videos/video_test":
			polls.Add(1)
			fmt.Fprint(w, `{"id":"video_test","status":"completed"}`)
		case "/videos/video_test/content":
			w.Header().Set("Content-Type", "video/mp4")
			w.Write([]byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom"))
		default:
			t.Errorf("错误路径 %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()
	result, err := Execute(context.Background(), Config{Provider: "openai", Endpoint: remote.URL, Key: "fixture", Model: "fixture-video", Timeout: 5 * time.Second}, Request{Kind: "video_generation", Prompt: "湖面", Size: "720x1280", Seconds: "8", Media: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jDe8AAAAASUVORK5CYII="}, false)
	if err != nil || result.MIME != "video/mp4" || len(result.Data) == 0 || posts.Load() != 1 || polls.Load() != 1 {
		t.Fatalf("生命周期失败 %+v %v", result, err)
	}
}

func TestVideoUnknownAndFailedStates(t *testing.T) {
	for _, status := range []string{"queued", "failed", "invalid", "completed"} {
		t.Run(status, func(t *testing.T) {
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/content") {
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprint(w, "bad")
					return
				}
				fmt.Fprintf(w, `{"id":"video_test","status":%q}`, status)
			}))
			defer remote.Close()
			_, err := Execute(context.Background(), Config{Provider: "openai", Endpoint: remote.URL, Key: "fixture", Model: "fixture", Timeout: 30 * time.Millisecond}, Request{Kind: "video_generation", Prompt: "test"}, false)
			if err == nil || VideoJobPending(err) != (status != "failed") {
				t.Fatalf("状态分类错误 %v", err)
			}
		})
	}
	// 取消必须立即返回，不等待下一次轮询。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Execute(ctx, Config{Provider: "openai", Endpoint: "https://fixture.invalid", Key: "fixture", Model: "fixture"}, Request{Kind: "video_generation", Prompt: "test"}, false)
	if !VideoJobPending(err) {
		t.Fatalf("取消未保留状态未知语义 %v", err)
	}
}
