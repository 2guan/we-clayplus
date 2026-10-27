package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const imageFixture = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jDe8AAAAASUVORK5CYII="

func TestImage415NegotiatesJSONAndDownloadsSameOrigin(t *testing.T) {
	calls := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture-image-key" {
			t.Error("认证头丢失")
		}
		switch calls {
		case 1:
			if r.Method != "POST" || r.URL.Path != "/v1/images/edits" || !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
				t.Error("首轮应使用标准编辑协议")
			}
			w.WriteHeader(415)
		case 2:
			if r.URL.Path != "/v1/images/edits" || r.Header.Get("Content-Type") != "application/json" {
				t.Error("415 后没有单次协商JSON")
			}
			var v map[string]any
			if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
				t.Error(err)
			}
			if v["image"] != "data:image/png;base64,"+imageFixture || v["prompt"] != "保留图形，背景改为蓝色" || v["model"] != "fixture-image-model" || v["response_format"] != "b64_json" || v["size"] != "1024x1024" || v["n"] != float64(1) {
				t.Error("参考图或生成参数丢失")
			}
			fmt.Fprint(w, `{"data":[{"url":"/api/media/fixture.png"}]}`)
		case 3:
			if r.Method != "GET" || r.URL.Path != "/api/media/fixture.png" {
				t.Error("同源图片解析错误")
			}
			data, _ := base64.StdEncoding.DecodeString(imageFixture)
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(data)
		default:
			t.Error("重复生成或下载")
		}
	}))
	defer remote.Close()
	result, err := Execute(context.Background(), Config{Provider: "openai", Endpoint: remote.URL + "/v1", Key: "fixture-image-key", Model: "fixture-image-model", Size: "1024x1024"}, Request{Kind: "image", Prompt: "保留图形，背景改为蓝色", Media: imageFixture, MIME: "image/png"}, false)
	if err != nil || len(result.Data) == 0 || result.MIME != "image/png" || calls != 3 {
		t.Fatal(err, result.MIME, len(result.Data), calls)
	}
}

func TestImageGenerationJSONAndInlineDataStayCompatible(t *testing.T) {
	calls := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/images/generations" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("普通生图协议改变")
		}
		fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, imageFixture)
	}))
	defer remote.Close()
	result, err := Execute(context.Background(), Config{Endpoint: remote.URL, Key: "fixture", Model: "fixture"}, Request{Kind: "image", Prompt: "图像测试"}, false)
	if err != nil || result.MIME != "image/png" || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestImageNegotiationOnlyRetriesExplicit415Once(t *testing.T) {
	for _, status := range []int{400, 401, 415, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(status) }))
			defer remote.Close()
			_, err := Execute(context.Background(), Config{Endpoint: remote.URL, Key: "fixture", Model: "fixture"}, Request{Kind: "image", Prompt: "测试", Media: imageFixture}, false)
			want := 1
			if status == 415 {
				want = 2
			}
			if err == nil || calls != want {
				t.Fatal(err, calls, want)
			}
		})
	}
}

func TestImageURLSafetyAndDownloadDeadline(t *testing.T) {
	var foreignCalls int
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { foreignCalls++ }))
	defer foreign.Close()
	for _, kind := range []string{"cross-origin", "redirect", "non-image", "oversize", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" {
					imageURL := "/asset"
					if kind == "cross-origin" {
						imageURL = foreign.URL + "/private"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"url": imageURL}}})
					return
				}
				switch kind {
				case "redirect":
					http.Redirect(w, r, foreign.URL+"/private", 302)
				case "non-image":
					fmt.Fprint(w, "<html>not an image</html>")
				case "oversize":
					w.Header().Set("Content-Length", fmt.Sprint((32<<20)+1))
				case "timeout":
					select {
					case <-r.Context().Done():
					case <-time.After(100 * time.Millisecond):
					}
				}
			}))
			defer remote.Close()
			_, err := Execute(context.Background(), Config{Endpoint: remote.URL, Key: "fixture-sensitive-key", Model: "fixture", Timeout: 30 * time.Millisecond}, Request{Kind: "image", Prompt: "测试"}, false)
			if err == nil {
				t.Error("不安全或无效图片未阻止")
			}
		})
	}
	if foreignCalls != 0 {
		t.Fatal("向其他地址发送了图片下载或凭据", foreignCalls)
	}
}
