// 文件名：server_test.go
// 主要功能：验证脱机隔离、认证权限、配置冲突、任务生命周期与敏感凭据的存储边界。
// 作者：2guan
package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/sdk/control"
)

type forbiddenHost struct{ t *testing.T }

func (h forbiddenHost) Execute(context.Context, control.Command) (any, error) {
	h.t.Error("Mock 调用了真实 Host")
	return nil, nil
}
func newServer(t *testing.T, mock bool) *Server {
	t.Helper()
	s, e := Start(context.Background(), control.Config{Address: "127.0.0.1:0", DataDir: t.TempDir(), Mock: mock, Disabled: true}, forbiddenHost{t})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	return s
}
func call(t *testing.T, s *Server, path string, body any, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	t.Helper()
	method := "GET"
	var raw []byte
	if body != nil {
		method = "POST"
		raw, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, "http://127.0.0.1"+path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestOfflineIsolationAndSettingsRevision(t *testing.T) {
	s := newServer(t, true)
	state := s.store.snapshot()
	target := state.Contacts[0].ID
	w := call(t, s, "/api/action", actionRequest{Kind: "send", Target: target, Content: "纯本机消息"}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	// 收发消息不能造成无关设置表单冲突；配置本身则必须比较版本。
	q := actionRequest{Kind: "settings", Group: "brand", Revision: state.ConfigRevision, Values: map[string]any{"name": "测试空间"}}
	if w = call(t, s, "/api/action", q, nil, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = call(t, s, "/api/action", q, nil, ""); w.Code != 400 {
		t.Fatal("陈旧配置覆盖了新配置")
	}
	if w = call(t, s, "/api/action", actionRequest{Kind: "mock-scenario", Content: "offline"}, nil, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = call(t, s, "/api/action", actionRequest{Kind: "send", Target: target, Content: "不应发送"}, nil, ""); w.Code != 400 {
		t.Fatal("离线状态仍允许发送")
	}
	if _, e := os.Stat(filepath.Join(s.cfg.DataDir, "console.db")); !os.IsNotExist(e) {
		t.Fatal("Mock 接触正式数据库")
	}
	r := httptest.NewRequest("GET", "http://attacker.invalid/api/state", nil)
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("未阻止 DNS rebinding Host")
	}
}
func TestAuthenticationAndRoles(t *testing.T) {
	s := newServer(t, false)
	if w := call(t, s, "/api/state", nil, nil, ""); w.Code != 401 {
		t.Fatal("未登录可读数据")
	}
	body := map[string]string{"name": "fixture-admin", "password": "abc123"}
	if w := call(t, s, "/api/auth/setup", body, nil, "http://evil.invalid"); w.Code != 403 {
		t.Fatal("未阻止跨站初始化")
	}
	// 五位密码拒绝，六位密码无需初始化令牌即可创建管理员。
	if w := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-admin", "password": "abc12"}, nil, ""); w.Code != 400 {
		t.Fatal("应拒绝少于六位的密码")
	}
	w := call(t, s, "/api/auth/setup", body, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if again := call(t, s, "/api/auth/setup", body, nil, ""); again.Code != 403 {
		t.Fatal("不应允许重复初始化")
	}
	// 保存名称后，未登录的新访客也能读取它，但仍不能读取完整设置。
	q := actionRequest{Kind: "settings", Group: "brand", Revision: s.store.snapshot().ConfigRevision, Values: map[string]any{"name": "公开品牌测试"}}
	if w := call(t, s, "/api/action", q, cookie, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	public := call(t, s, "/api/auth", nil, nil, "")
	var info map[string]any
	if err := json.Unmarshal(public.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if public.Code != 200 || info["brandName"] != "公开品牌测试" || info["authenticated"] != false || len(info) != 5 {
		t.Fatal("未登录品牌信息错误或公开了额外字段")
	}
	if w := call(t, s, "/api/state", nil, nil, ""); w.Code != 401 {
		t.Fatal("品牌公开后，完整状态仍须认证")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("会话 Cookie 属性无效")
	}
	if _, e := os.Stat(filepath.Join(s.cfg.DataDir, "setup-token")); !os.IsNotExist(e) {
		t.Fatal("初始化凭据未销毁")
	}
	if w = call(t, s, "/api/action", actionRequest{Kind: "user", Name: "fixture-operator", Password: "fixture-password-123", Role: "operator", Enabled: true}, cookie, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = call(t, s, "/api/auth/login", map[string]string{"name": "fixture-operator", "password": "fixture-password-123"}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	operator := w.Result().Cookies()[0]
	w = call(t, s, "/api/action", actionRequest{Kind: "plugin", ID: "ai", Enabled: false}, operator, "")
	if w.Code != 400 {
		t.Fatal("操作员修改管理员设置")
	}
	w = call(t, s, "/api/state", nil, operator, "")
	var state State
	_ = json.Unmarshal(w.Body.Bytes(), &state)
	if len(state.Users) != 0 {
		t.Fatal("操作员读取账号列表")
	}
	// 密钥只存在密文表中，状态 API 不返回它。
	key := "fixture-secret-not-real"
	if e := s.store.putSecret("test", key); e != nil {
		t.Fatal(e)
	}
	var sealed string
	if e := s.store.db.QueryRow("SELECT value FROM secrets WHERE id='test'").Scan(&sealed); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(sealed, key) {
		t.Fatal("明文密钥")
	}
	plain, e := s.store.getSecret("test")
	if e != nil || plain != key {
		t.Fatal("密钥无法恢复")
	}
	if strings.Contains(call(t, s, "/api/state", nil, cookie, "").Body.String(), key) {
		t.Fatal("API 泄露密钥")
	}
}
func TestTaskCancelAndRestart(t *testing.T) {
	s := newServer(t, true)
	id, e := s.Submit(ai.Request{Kind: "chat", Prompt: "测试取消"})
	if e != nil {
		t.Fatal(e)
	}
	if w := call(t, s, "/api/action", actionRequest{Kind: "cancel-task", ID: id}, nil, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		task, _ := s.Task(id)
		if task.Status == "cancelled" {
			return
		}
		time.Sleep(time.Millisecond * 10)
	}
	t.Fatal("任务未取消")
}
func TestStoreRollbackAndRecovery(t *testing.T) {
	dir := t.TempDir()
	s, e := openStore(dir, true)
	if e != nil {
		t.Fatal(e)
	}
	_ = s.change(func(v *State) error { v.Tasks = append(v.Tasks, Task{ID: "unfinished", Status: "queued"}); return nil })
	_ = s.db.Close()
	reopened, e := openStore(dir, true)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.db.Close()
	for _, task := range reopened.snapshot().Tasks {
		if task.ID == "unfinished" && task.Status != "interrupted" {
			t.Fatal("重启后静默重试了未知请求")
		}
	}
}

// 切换账号时保留各自的会话分区；切回原账号可恢复，不能混入另一个账号的上下文。
func TestAccountPartitions(t *testing.T) {
	s := newServer(t, false)
	if e := s.SetAccount(Account{ID: "fixture-a", Status: "online"}); e != nil {
		t.Fatal(e)
	}
	if e := s.Record(Message{ID: "fixture-message", Target: "fixture-contact", Content: "A 的虚构消息", Kind: "text"}); e != nil {
		t.Fatal(e)
	}
	if e := s.SetAccount(Account{ID: "fixture-b", Status: "online"}); e != nil {
		t.Fatal(e)
	}
	if len(s.store.snapshot().Messages) != 0 {
		t.Fatal("会话串入另一账号")
	}
	if e := s.SetAccount(Account{ID: "fixture-a", Status: "online"}); e != nil {
		t.Fatal(e)
	}
	if len(s.store.snapshot().Messages) != 1 {
		t.Fatal("原账号归档丢失")
	}
}
