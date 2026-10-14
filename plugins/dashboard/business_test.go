// 文件名：business_test.go
// 主要功能：验证业务插件预览隔离、快照发送、权限、计划去重、事件规则和持久化，不操作真实微信。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/json"
	"github.com/we-clay/we-clay/sdk/control"
	"net/http"
	"strings"
	"testing"
	"time"
)

func businessCall(t *testing.T, s *Server, q businessRequest) businessResult {
	t.Helper()
	w := call(t, s, "/api/business", map[string]any{"operation": "run", "request": q}, nil, "")
	if w.Code != 200 {
		t.Fatalf("%s: %s", q.Plugin, w.Body.String())
	}
	var r businessResult
	if json.Unmarshal(w.Body.Bytes(), &r) != nil {
		t.Fatal("结果无效")
	}
	return r
}
func waitBusiness(t *testing.T, check func() bool) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if check() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("等待业务任务超时")
}
func TestBusinessPreviewAndOneShotDelivery(t *testing.T) {
	s := newServer(t, true)
	before := len(s.store.snapshot().Messages)
	for _, id := range []string{"statistics", "what_to_eat", "news", "music", "video_parser", "welcome", "auto_accept", "digest"} {
		q := businessRequest{Plugin: id, Target: "mock-contact-0", Days: 7, Query: "示例"}
		r := businessCall(t, s, q)
		if !r.Mock || r.Text == "" || r.Preview == "" {
			t.Fatalf("预览不完整 %s: %+v", id, r)
		}
	}
	if len(s.store.snapshot().Messages) != before {
		t.Fatal("预览发送了消息")
	}
	r := businessCall(t, s, businessRequest{Plugin: "what_to_eat", Target: "mock-contact-0"})
	for i := 0; i < 2; i++ {
		w := call(t, s, "/api/business", map[string]any{"operation": "send", "id": r.Preview}, nil, "")
		if i == 0 && w.Code != 200 || i == 1 && w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	st := s.store.snapshot()
	if len(st.Messages) != before+1 || st.Messages[len(st.Messages)-1].Content != r.Text {
		t.Fatal("发送未复用原预览")
	}
	w := call(t, s, "/api/business", map[string]any{"operation": "run", "request": businessRequest{Plugin: "what_to_eat", Target: "mock-contact-0", Send: true}}, nil, "")
	if w.Code != 400 {
		t.Fatal("绕过预览发送")
	}
	_ = s.store.change(func(v *State) error { v.Settings["what_to_eat.targets"] = ""; return nil })
	r = businessCall(t, s, businessRequest{Plugin: "what_to_eat", Target: "mock-contact-0"})
	w = call(t, s, "/api/business", map[string]any{"operation": "send", "id": r.Preview}, nil, "")
	if w.Code != 400 {
		t.Fatal("绕过白名单")
	}
}
func TestBusinessArchiveIdentityAndPermissions(t *testing.T) {
	s := newServer(t, true)
	_ = s.store.change(func(v *State) error {
		v.Messages = []Message{{ID: "a", SenderID: "member-a", Sender: "同名", Target: "mock-contact-0", Kind: "text", Content: "设计", Time: time.Now().Format(time.RFC3339)}, {ID: "b", SenderID: "member-b", Sender: "同名", Target: "mock-contact-0", Kind: "text", Content: "界面", Time: time.Now().Format(time.RFC3339)}, {ID: "c", Sender: "其他会话", Target: "mock-contact-1", Kind: "text", Time: time.Now().Format(time.RFC3339)}}
		return nil
	})
	r := businessCall(t, s, businessRequest{Plugin: "statistics", Target: "mock-contact-0", Days: 7})
	if r.Count != 2 || len(r.Ranking) != 2 {
		t.Fatal("串会话或合并同名", r)
	}
	real := newServer(t, false)
	if w := call(t, real, "/api/business", nil, nil, ""); w.Code != 401 {
		t.Fatal("匿名可访问")
	}
	w := call(t, real, "/api/auth/setup", map[string]string{"name": "fixture-admin", "password": "fixture-password-123"}, nil, "")
	admin := w.Result().Cookies()[0]
	call(t, real, "/api/action", actionRequest{Kind: "user", Name: "fixture-operator", Password: "fixture-password-123", Role: "operator", Enabled: true}, admin, "")
	w = call(t, real, "/api/auth/login", map[string]string{"name": "fixture-operator", "password": "fixture-password-123"}, nil, "")
	op := w.Result().Cookies()[0]
	for _, body := range []any{nil, map[string]any{"operation": "run", "request": businessRequest{Plugin: "news"}}} {
		if w = call(t, real, "/api/business", body, op, ""); w.Code != http.StatusForbidden {
			t.Fatal("操作员越权")
		}
	}
	w = call(t, real, "/api/state", nil, op, "")
	if strings.Contains(w.Body.String(), "auto_accept.targets") {
		t.Fatal("策略泄露到操作员")
	}
}
func TestBusinessScheduleAtMostOnceAndRestart(t *testing.T) {
	s := newServer(t, true)
	q := businessJob{Name: "测试文字", Enabled: true, Kind: "once", Timezone: "Asia/Shanghai", At: time.Now().Add(30 * time.Second).Format(time.RFC3339), Request: businessRequest{Plugin: "text", Target: "mock-contact-0", Query: "定时测试"}}
	j, e := s.saveBusinessJob(q)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.saveBusinessJob(businessJob{ID: j.ID, Name: "陈旧修改", Enabled: true, Kind: "once", Timezone: "Asia/Shanghai", At: q.At, Request: q.Request}); e == nil {
		t.Fatal("陈旧配置覆盖")
	}
	before := len(s.store.snapshot().Messages)
	now, _ := time.Parse(time.RFC3339, j.Next)
	s.tickBusiness(now.Add(time.Second))
	s.tickBusiness(now.Add(2 * time.Second))
	waitBusiness(t, func() bool { return len(s.store.snapshot().Messages) == before+1 })
	time.Sleep(50 * time.Millisecond)
	if len(s.store.snapshot().Messages) != before+1 {
		t.Fatal("重复执行")
	}
	jobs, _ := s.businessJobs("mock-account")
	if jobs[0].Enabled {
		t.Fatal("单次任务未停用")
	}
	dir := s.cfg.DataDir
	s.Close()
	// 不复用已关闭服务，验证数据库恢复。
	next, e := Start(context.Background(), control.Config{Mock: true, Disabled: true, DataDir: dir}, forbiddenHost{t})
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	loaded, e := next.businessJobs("mock-account")
	if e != nil || len(loaded) != 1 || loaded[0].Enabled {
		t.Fatal("重启丢失任务状态")
	}
	next.tickBusiness(now.Add(5 * time.Second))
	if len(next.store.snapshot().Messages) != before+1 {
		t.Fatal("重启补发")
	}
}
func TestBusinessEventsAndTickets(t *testing.T) {
	s := newServer(t, true)
	_ = s.store.change(func(v *State) error {
		v.Settings["welcome.delay_seconds"] = float64(0)
		v.Settings["auto_accept.delay_seconds"] = float64(0)
		v.Settings["auto_accept.keyword"] = "验证词"
		v.Settings["auto_accept.scenes"] = "3"
		return nil
	})
	before := len(s.store.snapshot().Messages)
	m := Message{ID: "join-fixture", Target: "mock-contact-0", Content: "“测试成员”通过扫描二维码加入了群聊", Kind: "text", Source: "wechat", Time: time.Now().Format(time.RFC3339)}
	if s.dispatchBusiness(m) {
		t.Fatal("普通文本伪造系统事件")
	}
	m.Kind = "system"
	if !s.dispatchBusiness(m) {
		t.Fatal("系统欢迎未分发")
	}
	s.dispatchBusiness(m)
	waitBusiness(t, func() bool { return len(s.store.snapshot().Messages) == before+1 })
	q := FriendRequest{ID: "fixture-friend", Applicant: "mock-contact-1", Comment: "验证词", V1: "fixture-v1", V2: "fixture-ticket", Scene: 3}
	if e := s.HandleFriend(q); e != nil {
		t.Fatal(e)
	}
	if e := s.HandleFriend(q); e != nil {
		t.Fatal(e)
	}
	waitBusiness(t, func() bool {
		var n int
		_ = s.store.db.QueryRow("SELECT count(*) FROM business_runs WHERE plugin='auto_accept' AND status='completed'").Scan(&n)
		return n == 1
	})
	raw := call(t, s, "/api/business", nil, nil, "").Body.String() + call(t, s, "/api/state", nil, nil, "").Body.String()
	if strings.Contains(raw, "fixture-ticket") || strings.Contains(raw, "fixture-v1") {
		t.Fatal("申请票据泄露")
	}
}
