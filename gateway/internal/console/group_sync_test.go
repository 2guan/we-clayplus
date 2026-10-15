package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/message"
)

func TestNewGroupProfileSync(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	core := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/contacts/detail" {
			t.Error("unexpected request")
			w.WriteHeader(400)
			return
		}
		var ids []string
		if json.NewDecoder(r.Body).Decode(&ids) != nil || len(ids) != 1 {
			t.Error("invalid query")
			w.WriteHeader(400)
			return
		}
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(400)
			return
		} // 首次资料尚未就绪，模拟已有的失败缓存。
		if n == 2 {
			close(started)
			<-release
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"base_response": map[string]int{"code": 0}, "contact_list": []any{map[string]any{"username": map[string]string{"value": ids[0]}, "nickname": map[string]string{"value": "测试群"}, "small_avatar_url": "https://wx.qlogo.cn/fixture"}}}})
	}))
	client, err := api.NewGRPCClient(context.Background(), core.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	old := api.GetCore
	api.GetCore = func() *api.Client { return client }
	defer func() { api.GetCore = old }()
	contactability.LoadCache(t.TempDir())
	contactability.SetSelf(&contact.SelfInfo{Username: "fixture-self"})
	defer contactability.SetSelf(nil)
	// 预先缓存查询失败；后续后台刷新必须能越过此失败缓存。
	if contact.Instance.Get("fixture-room@chatroom") != nil {
		t.Fatal("expected initial cache miss")
	}
	s, err := dashboard.Start(context.Background(), control.Config{Disabled: true, DataDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b := &Bridge{Dashboard: s}
	b.Ready.Store(true)
	released := false
	defer func() {
		if !released {
			close(release)
		}
		b.WaitContactSync()
	}()
	msg := &message.Message{Id: 1, Type: message.TypeText, Sender: &contact.Contact{Username: "fixture-room@chatroom"}, Receiver: &contact.Contact{Username: "fixture-self"}, Content: "测试消息"}
	if err := b.Record(msg); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("资料查询未触发")
	}
	// 连续消息不重复查询；查询期间到达的另一个新群不能丢失。
	for i := 0; i < 5; i++ {
		b.startGroupSync("fixture-room@chatroom")
	}
	b.startGroupSync("fixture-second@chatroom")
	close(release)
	released = true
	deadline := time.Now().Add(3 * time.Second)
	for s.NeedsContactProfile("fixture-room@chatroom") || s.NeedsContactProfile("fixture-second@chatroom") {
		if time.Now().After(deadline) {
			t.Fatal("群资料未投影到页面")
		}
		time.Sleep(10 * time.Millisecond)
	}
	b.WaitContactSync()
	if calls.Load() != 3 {
		t.Fatalf("重复请求: %d", calls.Load())
	}
	req := httptest.NewRequest("POST", "http://local/api/auth/setup", strings.NewReader(`{"name":"fixture-admin","password":"abc123"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	req = httptest.NewRequest("GET", "http://local/api/state", nil)
	req.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	var st dashboard.State
	if json.Unmarshal(w.Body.Bytes(), &st) != nil {
		t.Fatal("invalid state")
	}
	for _, c := range st.Contacts {
		if c.ID == "fixture-room@chatroom" && (c.Name != "测试群" || c.Avatar == "" || c.Preview != "测试消息" || c.Unread != 1) {
			t.Fatal("同步丢失群资料或会话状态")
		}
	}
}
