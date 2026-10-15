// 文件名：models_test.go
// 主要功能：用本机 HTTP 服务器验证模型目录、MiMo 音色协议、图片 multipart 及超时语义。
// 作者：2guan
package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiscoverProtocolsAndMockIsolation(t *testing.T) {
	for _, provider := range []string{"openai", "mimo", "anthropic", "gemini"} {
		t.Run(provider, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v1/models" {
					t.Error("模型目录请求错误")
				}
				header := "Authorization"
				expected := "Bearer fixture"
				if provider == "gemini" {
					header = "x-goog-api-key"
					expected = "fixture"
				}
				if provider == "anthropic" {
					header = "x-api-key"
					expected = "fixture"
				}
				if r.Header.Get(header) != expected {
					t.Error("凭据头错误")
				}
				if provider == "gemini" {
					fmt.Fprint(w, `{"models":[{"name":"models/example"}]}`)
				} else {
					fmt.Fprint(w, `{"data":[{"id":"example"},{"id":"example"}]}`)
				}
			}))
			defer server.Close()
			ids, err := Discover(context.Background(), Config{Provider: provider, Endpoint: server.URL + "/v1", Key: "fixture"}, false)
			if err != nil || len(ids) != 1 || ids[0] != "example" {
				t.Fatalf("目录解析错误 %v %v", ids, err)
			}
		})
	}
	old := http.DefaultTransport
	blocked := &denyTransport{}
	http.DefaultTransport = blocked
	defer func() { http.DefaultTransport = old }()
	if _, err := Discover(context.Background(), Config{Endpoint: "http://forbidden.invalid"}, true); err != nil || blocked.calls.Load() != 0 {
		t.Fatal("Mock 访问外网")
	}
}
func TestMimoSpeechModes(t *testing.T) {
	for _, mode := range []string{"mimo-v2.5-tts", "mimo-v2.5-tts-voicedesign", "mimo-v2.5-tts-voiceclone"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/chat/completions" {
					t.Error("MiMo 不应调用 audio/speech")
				}
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				messages := body["messages"].([]any)
				if messages[1].(map[string]any)["role"] != "assistant" || messages[1].(map[string]any)["content"] != "测试" {
					t.Error("合成文本角色错误")
				}
				audio := body["audio"].(map[string]any)
				if strings.HasSuffix(mode, "voiceclone") && !strings.HasPrefix(audio["voice"].(string), "data:audio/wav;base64,") {
					t.Error("克隆样本未传入")
				}
				if strings.HasSuffix(mode, "voicedesign") && audio["voice"] != nil {
					t.Error("设计模式不应带内置音色")
				}
				fmt.Fprint(w, `{"choices":[{"message":{"audio":{"data":"UklGRg=="}}}]}`)
			}))
			defer server.Close()
			result, err := Execute(context.Background(), Config{Provider: "mimo", Endpoint: server.URL, Key: "fixture", Model: mode, Voice: "冰糖", Design: "自然低沉"}, Request{Kind: "tts", Prompt: "测试", Media: base64.StdEncoding.EncodeToString([]byte("fixture")), MIME: "audio/wav"}, false)
			if err != nil || string(result.Data) != "RIFF" {
				t.Fatalf("音频解析失败 %v", err)
			}
		})
	}
}
func TestRequestTimeoutPreservesCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer server.Close()
	_, err := Execute(context.Background(), Config{Endpoint: server.URL, Key: "fixture", Model: "fixture", Timeout: 20 * time.Millisecond}, Request{Kind: "chat", Prompt: "test"}, false)
	if !errors.Is(err, context.DeadlineExceeded) || !Retryable(err) {
		t.Fatalf("超时未透传 %v", err)
	}
}
func TestImageEditUsesMultipart(t *testing.T) {
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jDe8AAAAASUVORK5CYII=")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/edits" {
			t.Error("垫图未走编辑接口")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("size") != "1024x1024" || r.FormValue("prompt") != "test" {
			t.Error("编辑参数丢失")
		}
		f, _, err := r.FormFile("image")
		if err != nil {
			t.Error(err)
		} else {
			f.Close()
		}
		fmt.Fprintf(w, `{"data":[{"b64_json":%q}]}`, base64.StdEncoding.EncodeToString(png))
	}))
	defer server.Close()
	result, err := Execute(context.Background(), Config{Endpoint: server.URL, Key: "fixture", Model: "gpt-image-test", Size: "1024x1024"}, Request{Kind: "image", Prompt: "test", Media: base64.StdEncoding.EncodeToString(png), MIME: "image/png"}, false)
	if err != nil || len(result.Data) == 0 {
		t.Fatal(err)
	}
}

func TestToolCallContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body["tools"].([]any)) != 1 || body["parallel_tool_calls"] != false {
			t.Error("工具未受限为串行")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":null,"tool_calls":[{"function":{"name":"contact__SetRemark","arguments":"{\"content\":\"测试备注\"}"}}]}}]}`)
	}))
	defer server.Close()
	result, err := Execute(context.Background(), Config{Endpoint: server.URL, Model: "fixture", Key: "fixture"}, Request{Kind: "chat", Prompt: "test", Tools: []Tool{{Name: "contact__SetRemark", Description: "仅当前会话"}}}, false)
	if err != nil || len(result.ToolCalls) != 1 || result.ToolCalls[0].Name != "contact__SetRemark" {
		t.Fatalf("工具调用丢失 %+v %v", result, err)
	}
}

// MiMo 音频输入按官方文档传 data URL，不走 multipart 转写端点。
func TestMiMoAudioUnderstandingContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if r.URL.Path != "/chat/completions" || json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Messages) != 2 || !strings.Contains(string(body.Messages[1].Content), "data:audio/wav;base64,") || !strings.Contains(string(body.Messages[1].Content), "input_audio") {
			t.Error("MiMo 音频请求协议不匹配")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"合成测试转写"}}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`)
	}))
	defer server.Close()
	result, err := Execute(context.Background(), Config{Endpoint: server.URL, Key: "fixture", Provider: "mimo", Model: "mimo-v2.6-flash"}, Request{Kind: "audio", Prompt: "转写", Media: base64.StdEncoding.EncodeToString([]byte("fixture")), MIME: "audio/wav"}, false)
	if err != nil || result.Text != "合成测试转写" || result.InputTokens != 12 {
		t.Fatal(result, err)
	}
}
