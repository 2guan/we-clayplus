// 文件名：report_timeout_test.go
// 主要功能：验证流式分析超时后保留可用正文，失败报告仍经过输出防护。
// 作者：2guan
package dashboard

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/plugins/ai"
)

func TestInsightTimeoutPreservesOnlySafePartialReport(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"partial", "部分报告", "部分报告"},
		{"secret", "fixture-retry-key", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, sends := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", tc.body)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			})
			replyRegressionCheck(t, s.store.change(func(st *State) error {
				st.Models[0].Timeout = 1
				return nil
			}))
			id, err := s.Submit(ai.Request{Kind: "insight", Source: "analysis", Model: "fixture-retry-model", Prompt: "请总结资料"})
			replyRegressionCheck(t, err)
			task := waitModelTask(t, s, id)
			if task.Status != "failed" || task.Result != tc.want || sends.Load() != 0 {
				t.Fatal("超时结果状态或正文错误", task.Status, len(task.Result), sends.Load())
			}
			if tc.want != "" && (!strings.Contains(task.Error, "超时") || !strings.Contains(task.Error, "context deadline exceeded")) {
				t.Fatal("超时原因丢失", task.Error)
			}
			reopened, err := openStore(s.cfg.DataDir, false)
			replyRegressionCheck(t, err)
			defer reopened.db.Close()
			found := false
			for _, saved := range reopened.snapshot().Tasks {
				if saved.ID == id {
					found = true
					if saved.Status != "failed" || saved.Result != tc.want {
						t.Fatal("未正确持久化部分报告")
					}
				}
			}
			if !found {
				t.Fatal("失败报告未保存")
			}
		})
	}
}
