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

func TestGroupNameReceiptAndExternalNoticeUpdateDashboard(t *testing.T) {
	for _, room := range []string{"fixture-rename@chatroom", "fixture-rename@im.chatroom"} {
		t.Run(room, func(t *testing.T) { testGroupNameReceiptAndExternalNotice(t, room) })
	}
}

func testGroupNameReceiptAndExternalNotice(t *testing.T, room string) {
	var name atomic.Value
	name.Store("测试旧群名")
	var writes, reads atomic.Int32
	core := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/chatroom/name/" + room:
			if r.Method != "PUT" {
				t.Error("群名请求方法错误")
			}
			writes.Add(1)
			name.Store(r.URL.Query().Get("name"))
			// 复现既有 packed 回执缺失字段的实际读回确认路径，不固定模拟成功。
			_, _ = w.Write([]byte(`{"code":0,"data":{"code":0,"result":{"count":1}}}`))
		case "/api/contacts/detail":
			reads.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"base_response": map[string]int{"code": 0}, "contact_list": []any{map[string]any{
					"username": map[string]string{"value": room}, "nickname": map[string]any{"value": name.Load()}, "small_avatar_url": "https://wx.qlogo.cn/fixture",
				}},
			}})
		default:
			t.Error("意外调用", r.Method, r.URL.Path)
			w.WriteHeader(400)
		}
	}))
	client, err := api.NewGRPCClient(context.Background(), core.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	old := api.GetCore
	api.GetCore = func() *api.Client { return client }
	defer func() { api.GetCore = old }()
	cacheDir := t.TempDir()
	contactability.LoadCache(cacheDir)
	contactability.SetSelf(&contact.SelfInfo{Username: "fixture-self"})
	defer contactability.SetSelf(nil)
	if _, err = contactability.RefreshUsername(room); err != nil {
		t.Fatal(err)
	}
	s, err := dashboard.Start(context.Background(), control.Config{Disabled: true, DisableAI: true, DataDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b := &Bridge{Dashboard: s}
	b.Ready.Store(true)
	defer b.WaitContactSync()
	if err = b.SyncContacts(); err != nil {
		t.Fatal(err)
	}
	// 已完整缓存的群也会从普通系统通知兜底核对，但同一群不会每条通知都请求详情。
	for i := 0; i < 5; i++ {
		if err = b.Record(&message.Message{Id: int64(10 + i), Type: message.TypeSystemTip, Sender: &contact.Contact{Username: room}, Receiver: &contact.Contact{Username: "fixture-self"}, Content: "成员撤回了一条消息", Timestamp: uint32(time.Now().Unix())}); err != nil {
			t.Fatal(err)
		}
	}
	b.contactSyncWG.Wait()
	if reads.Load() != 2 {
		t.Fatalf("普通群系统通知重复刷新资料：%d", reads.Load())
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "http://local/api/auth/setup", strings.NewReader(`{"name":"fixture-admin","password":"abc123"}`))
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	cookie := w.Result().Cookies()[0]
	read := func() dashboard.State {
		req := httptest.NewRequest("GET", "http://local/api/state", nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		var st dashboard.State
		if json.Unmarshal(w.Body.Bytes(), &st) != nil {
			t.Fatal("读取页面状态失败")
		}
		return st
	}
	if _, err = b.Execute(context.Background(), control.Command{Operation: "group-name", Target: room, Content: "测试新群名"}); err != nil {
		t.Fatal(err)
	}
	if read().Contacts[0].Name != "测试新群名" || contact.Instance.Get(room).Nickname != "测试新群名" || writes.Load() != 1 || reads.Load() != 3 {
		t.Fatal("确认成功的群名未同步，或写操作被重试")
	}
	// 模拟异常退出后重载联系人缓存；已确认的群名不能被旧缓存覆盖。
	contactability.LoadCache(cacheDir)
	if contact.Instance.Get(room).Nickname != "测试新群名" {
		t.Fatal("已确认的群名未写入联系人缓存")
	}
	name.Store("测试外部群名")
	if err = b.Record(&message.Message{Id: 100, Type: message.TypeSystemTip, Sender: &contact.Contact{Username: room}, Receiver: &contact.Contact{Username: "fixture-self"}, Content: "测试成员修改了群名", Timestamp: uint32(time.Now().Unix())}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for read().Contacts[0].Name != "测试外部群名" {
		if time.Now().After(deadline) {
			t.Fatal("完整资料的群被错误跳过刷新")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if writes.Load() != 1 || reads.Load() != 4 {
		t.Fatal("收到改名通知后未立即读回资料，或执行了额外写操作")
	}
}
