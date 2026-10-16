// 文件名：refusal_test.go
// 主要功能：验证流式、普通响应的明确拒绝均失败，正常报告引用不会误判。
// 作者：2guan
package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProviderRefusalIsFailure(t *testing.T) {
	for _, tc := range []struct {
		name, content, reason, refusal string
		want                           bool
	}{
		{"content-filter", "The request was rejected because it was considered high risk", "content_filter", "", true},
		{"exact-notice", "The request was rejected because it was considered high risk.", "stop", "", true},
		{"refusal-field", "", "stop", "fixture provider refusal", true},
		{"partial-filtered", "部分有效报告", "content_filter", "", true},
		{"quoted-notice", "报告引用：The request was rejected because it was considered high risk", "stop", "", false},
		{"normal", "正常报告中讨论风险", "stop", "", false},
	} {
		for _, stream := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/%v", tc.name, stream), func(t *testing.T) {
				remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q,\"refusal\":%q},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", tc.content, tc.refusal, tc.reason)
					} else {
						fmt.Fprintf(w, "{\"choices\":[{\"message\":{\"content\":%q,\"refusal\":%q},\"finish_reason\":%q}]}", tc.content, tc.refusal, tc.reason)
					}
				}))
				defer remote.Close()
				cfg := Config{Provider: "openai", Endpoint: remote.URL, Model: "fixture", Key: "fixture-refusal-key"}
				q := Request{Kind: "insight", Prompt: "请分析资料"}
				var got Result
				var err error
				if stream {
					got, err = ExecuteStream(context.Background(), cfg, q, false, func(string) {})
				} else {
					got, err = Execute(context.Background(), cfg, q, false)
				}
				if Refused(err) != tc.want {
					t.Fatal("拒绝状态错误", err)
				}
				if tc.want && RefusalText(tc.content) && got.Text != "" {
					t.Fatal("拒绝说明被当作报告正文")
				}
				if !tc.want && (err != nil || got.Text != tc.content) {
					t.Fatal("正常报告被误判", err)
				}
				if tc.name == "partial-filtered" && got.Text != tc.content {
					t.Fatal("部分有效正文丢失")
				}
			})
		}
	}
}
