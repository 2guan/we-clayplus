// 文件名：reports_test.go
// 主要功能：验证洞察缓存、失败重试、追问、来源变化、删除和账号隔离。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/we-clay/we-clay/sdk/control"
)

func reportCookie(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	s.authMu.Lock()
	for id := range s.sessions {
		s.authMu.Unlock()
		return &http.Cookie{Name: "weclay_session", Value: id}
	}
	s.authMu.Unlock()
	w := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-report-admin", "password": "fixture-report-password"}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}
func reportSubmitFixture(t *testing.T, s *Server, body any) Task {
	t.Helper()
	w := call(t, s, "/api/reports", body, reportCookie(t, s), "")
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return waitModelTask(t, s, response.ID)
}
func reportActionFixture(t *testing.T, s *Server, id, op, question string) Task {
	t.Helper()
	w := call(t, s, "/api/reports/action", map[string]any{"id": id, "operation": op, "question": question}, reportCookie(t, s), "")
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return waitModelTask(t, s, response.ID)
}
func reportEvent(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", text)
}

func TestReportCacheSnapshotRetryAndFollowup(t *testing.T) {
	var calls, reads atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid model input")
		}
		raw, _ := json.Marshal(body)
		if n == 3 && (!strings.Contains(string(raw), "fixture-base-report") || !strings.Contains(string(raw), "请解释依据")) {
			t.Error("追问没有使用原报告和问题")
		}
		if n == 3 {
			reportEvent(w, "fixture-supplement")
		} else {
			reportEvent(w, "fixture-base-report")
		}
	})
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		if c.Operation != "moments-page" {
			t.Error("unexpected operation", c.Operation)
		}
		reads.Add(1)
		if c.ID == "" {
			return MomentPage{Items: []Moment{{ID: "moment-1", AuthorID: m.Target, Content: "fixture-source-1", Images: []string{"/media/fixture.png"}}, {ID: "moment-2", AuthorID: m.Target, Content: "fixture-source-2"}}, NextCursor: "older"}, nil
		}
		return MomentPage{Items: []Moment{{ID: "moment-3", AuthorID: m.Target, Content: "fixture-source-3"}}}, nil
	})
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Models[0].Capabilities = []string{"chat", "vision"}; return nil }))
	replyRegressionCheck(t, os.MkdirAll(filepath.Join(s.cfg.DataDir, "media"), 0700))
	f, err := os.Create(filepath.Join(s.cfg.DataDir, "media", "fixture.png"))
	replyRegressionCheck(t, err)
	img := image.NewRGBA(image.Rect(0, 0, 900, 600))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	replyRegressionCheck(t, png.Encode(f, img))
	replyRegressionCheck(t, f.Close())
	create := reportCreate{Type: "moments", Target: m.Target, Prompt: "请分析实际资料", Model: "fixture-retry-model", Range: ReportRange{Mode: "count", Count: 1}}
	failed := reportSubmitFixture(t, s, create)
	if failed.Status != "failed" || failed.Result != "" || failed.Insight.SnapshotID == "" || reads.Load() != 1 {
		t.Fatal("失败报告没有快照", failed.Status, reads.Load())
	}
	// 缩略图和原资料必须加密保存在独立存储，不出现在全局状态。
	stateRaw, _ := json.Marshal(s.store.snapshot())
	if strings.Contains(string(stateRaw), "fixture-source-1") || strings.Contains(string(stateRaw), "/9j/") {
		t.Fatal("原始快照泄露到全局状态")
	}
	fail.Store(false)
	retried := reportActionFixture(t, s, failed.ID, "retry", "")
	if retried.Status != "completed" || retried.Insight.SnapshotID != failed.Insight.SnapshotID || reads.Load() != 1 {
		t.Fatal("重试重新读取资料", retried.Error, reads.Load())
	}
	if w := call(t, s, "/api/reports/action", map[string]any{"id": failed.ID, "operation": "retry"}, reportCookie(t, s), ""); w.Code != 400 {
		t.Fatal("重复重试被接受")
	}
	follow := reportActionFixture(t, s, retried.ID, "followup", "请解释依据")
	if follow.Status != "completed" || follow.Insight.RootID != failed.ID || follow.Insight.Question != "请解释依据" || reads.Load() != 1 {
		t.Fatal("追问范围错误", follow.Error)
	}
	fail.Store(true)
	failedTurn := reportActionFixture(t, s, follow.ID, "followup", "再补充一个例子")
	if failedTurn.Status != "failed" || failedTurn.Insight.TurnID == "" {
		t.Fatal("失败追问没有留下独立记录")
	}
	fail.Store(false)
	turnRetry := reportActionFixture(t, s, failedTurn.ID, "retry", "")
	if turnRetry.Status != "completed" || turnRetry.Insight.TurnID != failedTurn.Insight.TurnID || turnRetry.Insight.SnapshotID != failed.Insight.SnapshotID || reads.Load() != 1 {
		t.Fatal("追问重试更换了范围或重复读取")
	}
	// 缩略图已删除，缓存足够时仍应直接使用处理好的图片。
	replyRegressionCheck(t, os.Remove(filepath.Join(s.cfg.DataDir, "media", "fixture.png")))
	create.Range.Count = 2
	cached := reportSubmitFixture(t, s, create)
	if cached.Status != "completed" || !cached.Insight.CacheHit || cached.Insight.ImageCount != 1 || reads.Load() != 1 {
		t.Fatal("重复分析未命中缓存", cached.Error, reads.Load())
	}
	create.Range.Count = 3
	expanded := reportSubmitFixture(t, s, create)
	if expanded.Status != "completed" || expanded.Insight.Count != 3 || reads.Load() != 2 {
		t.Fatal("扩大范围没有从游标续取", expanded.Error, reads.Load())
	}
	reopened, err := openStore(s.cfg.DataDir, false)
	replyRegressionCheck(t, err)
	var persisted momentAnalysisCache
	replyRegressionCheck(t, reopened.loadReportValue(reportKey("insight-cache", s.store.snapshot().Account.ID, m.Target), &persisted))
	reopened.db.Close()
	if len(persisted.Items) != 3 {
		t.Fatal("重启丢失资料缓存")
	}
	// 更新资料生成新版本；不改变此前失败/追问绑定的快照。
	create.Refresh = true
	create.Range.Count = 1
	fresh := reportSubmitFixture(t, s, create)
	if fresh.Status != "completed" || fresh.Insight.CacheHit || reads.Load() != 3 || fresh.Insight.SnapshotID == failed.Insight.SnapshotID {
		t.Fatal("更新资料未生成新版本")
	}
	replyRegressionCheck(t, s.deleteReport(failed.ID))
	if _, err := s.Task(follow.ID); err == nil {
		t.Fatal("删除报告没有删除追问")
	}
	if _, err := s.loadReportSnapshot(s.store.snapshot().Account.ID, failed.Insight.SnapshotID); err == nil {
		t.Fatal("报告删除后残留可读快照")
	}
	if _, err := s.Task(cached.ID); err != nil {
		t.Fatal("删除影响其他报告")
	}
}

func TestReportCollectionFailureRetryAndSourceInvalidation(t *testing.T) {
	var reads, calls atomic.Int32
	s, m, _ := retryFixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); reportEvent(w, "fixture-answer") })
	s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
		if reads.Add(1) == 1 {
			return nil, fmt.Errorf("fixture temporary failure")
		}
		return MomentPage{Items: []Moment{{ID: "moment-1", AuthorID: m.Target, Content: "资料"}}}, nil
	})
	failed := reportSubmitFixture(t, s, reportCreate{Type: "moments", Target: m.Target, Prompt: "请总结资料", Range: ReportRange{Mode: "count", Count: 5}})
	if failed.Status != "failed" || failed.Insight.SnapshotID != "" || calls.Load() != 0 {
		t.Fatal("读取失败没有留下可重试任务")
	}
	retried := reportActionFixture(t, s, failed.ID, "retry", "")
	if retried.Status != "completed" || reads.Load() != 2 || calls.Load() != 1 {
		t.Fatal("读取阶段重试失败", retried.Error)
	}
	// 归档消息撤回后不能通过旧快照继续分析。
	original := reportSubmitFixture(t, s, reportCreate{Type: "conversation", Target: m.Target, Prompt: "请总结资料", Range: ReportRange{Mode: "count", Count: 1}})
	if original.Status != "completed" {
		t.Fatal(original.Error)
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages[0].Status = "revoked"; return nil }))
	rejected := reportActionFixture(t, s, original.ID, "followup", "请详细说明")
	if rejected.Status != "failed" || !strings.Contains(rejected.Error, "撤回") || calls.Load() != 2 {
		t.Fatal("撤回资料仍被注入", rejected.Error, calls.Load())
	}
	// 更换账号不能读取已有资料快照或命中其他账号缓存。
	if _, err := s.loadReportSnapshot("fixture-other-account", original.Insight.SnapshotID); err == nil {
		t.Fatal("快照跨账号可读")
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Account.ID = "fixture-other-account"; return nil }))
	w := call(t, s, "/api/reports/cache?target="+m.Target, nil, reportCookie(t, s), "")
	if !strings.Contains(w.Body.String(), `"count":0`) {
		t.Fatal("缓存跨账号可见")
	}
}

func TestReportRangesAreSelectedOnServer(t *testing.T) {
	s := newServer(t, true)
	st := s.store.snapshot()
	target := st.Contacts[0].ID
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Messages = nil
		for i := 0; i < 2005; i++ {
			st.Messages = append(st.Messages, Message{ID: fmt.Sprintf("fixture-%d", i), Target: target, Sender: "测试成员", Kind: "text", Status: "sent", Content: fmt.Sprintf("归档范围资料-%d", i), Time: "2026-10-16T00:00:00+08:00"})
		}
		st.Settings["context_cleaner.max_input"] = float64(4096)
		for i := range st.Models {
			st.Models[i].MaxOutput = 128
		}
		return nil
	}))
	task := reportSubmitFixture(t, s, reportCreate{Type: "conversation", Target: target, Prompt: "总结归档资料", Range: ReportRange{Mode: "count", Count: 2000}})
	if task.Status != "failed" || !strings.Contains(task.Error, "容量") {
		t.Fatal("大范围资料未明确报错", task.Error)
	}
	snap, err := s.loadReportSnapshot(st.Account.ID, task.Insight.SnapshotID)
	replyRegressionCheck(t, err)
	if len(snap.Documents) != 2000 || snap.Documents[0].ID != "fixture-5" || snap.Documents[1999].ID != "fixture-2004" {
		t.Fatal("近期条数被静默截断")
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Settings["context_cleaner.max_input"] = float64(128000)
		st.Messages = nil
		for i, v := range []string{"2026-10-15T23:59:59+08:00", "2026-10-16T00:00:00+08:00", "2026-10-16T00:01:59+08:00", "2026-10-16T00:02:00+08:00"} {
			st.Messages = append(st.Messages, Message{ID: fmt.Sprint("time-", i), Target: target, Sender: "测试成员", Kind: "text", Status: "sent", Content: "时间范围资料", Time: v})
		}
		return nil
	}))
	task = reportSubmitFixture(t, s, reportCreate{Type: "conversation", Target: target, Prompt: "总结时间段资料", Range: ReportRange{Mode: "time", Start: "2026-10-15T16:00:00Z", End: "2026-10-15T16:02:00Z"}})
	if task.Status != "completed" {
		t.Fatal(task.Error)
	}
	snap, err = s.loadReportSnapshot(st.Account.ID, task.Insight.SnapshotID)
	replyRegressionCheck(t, err)
	if len(snap.Documents) != 2 || snap.Documents[0].ID != "time-1" || snap.Documents[1].ID != "time-2" {
		t.Fatal("北京时间和 UTC 范围不一致")
	}
}

func TestLegacyReportCannotForgeSnapshotMetadata(t *testing.T) {
	s := newServer(t, true)
	w := call(t, s, "/api/tasks", map[string]any{"kind": "insight", "prompt": "请总结", "insight": map[string]any{"type": "conversation", "prompt": "请总结", "managed": true, "root_id": "fixture-victim", "snapshot_id": "fixture-private-source", "parent_id": "fixture-parent"}}, nil, "")
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		ID string `json:"id"`
	}
	replyRegressionCheck(t, json.Unmarshal(w.Body.Bytes(), &result))
	task := waitModelTask(t, s, result.ID)
	if task.Insight.Managed || task.Insight.RootID != "" || task.Insight.SnapshotID != "" || task.Insight.ParentID != "" {
		t.Fatal("旧入口接受了伪造的资料关联")
	}
}
