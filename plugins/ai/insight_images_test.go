// 文件名：insight_images_test.go
// 主要功能：验证三类厂商的洞察请求均携带图片内容及朋友圈关联标识。
// 作者：2guan
package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInsightImagesProviderPayload(t *testing.T) {
	for _, provider := range []string{"openai", "gemini", "anthropic"} {
		t.Run(provider, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				body := string(raw)
				for _, expected := range []string{"fixture-image-bytes", "朋友圈 fixture-1", "image/jpeg"} {
					if !strings.Contains(body, expected) {
						t.Errorf("%s 请求缺少 %s", provider, expected)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"choices":[{"message":{"content":"分析"}}],"candidates":[{"content":{"parts":[{"text":"分析"}]}}],"content":[{"type":"text","text":"分析"}]}`)
			}))
			defer server.Close()
			_, err := Execute(context.Background(), Config{Provider: provider, Endpoint: server.URL, Model: "fixture", Key: "fixture-key"}, Request{Kind: "insight", Prompt: "分析资料", Images: []InputImage{{Label: "朋友圈 fixture-1", MIME: "image/jpeg", Data: "fixture-image-bytes"}}}, false)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
