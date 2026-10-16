// 文件名：report_refusal_test.go
// 主要功能：验证服务方拒绝不标成功、不自动换模型，手动重试复用原资料。
// 作者：2guan
package dashboard

import (
	"net/http"
	"sync/atomic"
	"testing"
)

func TestRefusedReportCanRetryWithoutAutomaticFallback(t *testing.T) {
	var calls atomic.Int32
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"The request was rejected because it was considered high risk\"},\"finish_reason\":\"content_filter\"}]}\n\ndata: [DONE]\n\n"))
			return
		}
		reportEvent(w, "fixture valid report")
	})
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		second := st.Models[0]
		second.ID = "fixture-second"
		second.Model = "fixture-second"
		st.Models = append(st.Models, second)
		st.Pipeline = append(st.Pipeline, second.ID)
		st.Settings["routing.fallback"] = true
		return nil
	}))
	failed := reportSubmitFixture(t, s, reportCreate{Type: "conversation", Target: m.Target, Prompt: "请总结资料", Range: ReportRange{Mode: "count", Count: 1}})
	if failed.Status != "failed" || failed.Result != "" || calls.Load() != 1 || failed.Insight.SnapshotID == "" {
		t.Fatal("拒绝被标成成功或自动切换模型", failed.Status, calls.Load())
	}
	retried := reportActionFixture(t, s, failed.ID, "retry", "")
	if retried.Status != "completed" || retried.Insight.SnapshotID != failed.Insight.SnapshotID || calls.Load() != 2 {
		t.Fatal("手动重试未复用原资料", retried.Error)
	}
}
func TestRestoreOnlyExactRejectedReports(t *testing.T) {
	s := newServer(t, true)
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Tasks = []Task{{ID: "old-refused", Kind: "insight", Status: "completed", Result: "The request was rejected because it was considered high risk", Insight: &InsightInfo{Type: "conversation", Prompt: "请总结"}}, {ID: "quoted", Kind: "insight", Status: "completed", Result: "报告引用：The request was rejected because it was considered high risk"}}
		return nil
	}))
	replyRegressionCheck(t, s.restoreReportRefusals())
	failed, _ := s.Task("old-refused")
	quoted, _ := s.Task("quoted")
	if failed.Status != "failed" || failed.Result != "" || failed.Error == "" || quoted.Status != "completed" {
		t.Fatal("旧报告修正错误")
	}
	replyRegressionCheck(t, s.restoreReportRefusals())
}
