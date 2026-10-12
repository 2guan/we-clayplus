package ability_test

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	_ "github.com/we-clay/we-clay/gateway/internal/ability"
	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/chatroom"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/favor"
	"github.com/we-clay/we-clay/sdk/label"
	"github.com/we-clay/we-clay/sdk/miniapp"
	"github.com/we-clay/we-clay/sdk/moments"
	"github.com/we-clay/we-clay/sdk/user"
)

// 经过真实适配层和本地 gRPC；不连接微信，不把固定 200 当作成功证据。
func TestBusinessReceiptsMustNotBecomeSuccess(t *testing.T) {
	var body atomic.Value
	var calls atomic.Int32
	s := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, body.Load().(string))
	}))
	defer s.Close()
	c, err := api.NewGRPCClient(context.Background(), s.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	old := api.GetCore
	api.GetCore = func() *api.Client { return c }
	defer func() { api.GetCore = old }()
	methods := map[string]func() error{
		"chatroom.name":    func() error { _, e := chatroom.Instance.SetName("test-room@chatroom", "test"); return e },
		"chatroom.remark":  func() error { _, e := chatroom.Instance.SetRemark("test-room@chatroom", ""); return e },
		"chatroom.save":    func() error { _, e := chatroom.Instance.Save("test-room@chatroom", false); return e },
		"contact.remark":   func() error { return contact.Instance.SetRemark("test-peer", "") },
		"contact.request":  func() error { return contact.Instance.AddFriend("test-v1", "test-v2", "", 2, 3) },
		"contact.verify":   func() error { return contact.Instance.VerifyFriend("test-v1", "test-v2", 3) },
		"chatroom.qrcode":  func() error { _, e := chatroom.Instance.GetQRCode("test-room@chatroom"); return e },
		"label.list":       func() error { _, e := label.Instance.List(); return e },
		"label.add":        func() error { _, e := label.Instance.Add("test-label"); return e },
		"label.update":     func() error { _, e := label.Instance.Update(42, "test-label"); return e },
		"label.delete":     func() error { _, e := label.Instance.Delete("42"); return e },
		"label.modify":     func() error { _, e := label.Instance.ModifyContactLabels([]string{"test-peer"}, ""); return e },
		"favor.info":       func() error { _, e := favor.Instance.GetInfo(); return e },
		"moments.timeline": func() error { _, e := moments.Instance.Timeline("", 0); return e },
		"moments.userpage": func() error { _, e := moments.Instance.UserPage("test-peer", "", 0); return e },
		"moments.detail":   func() error { _, e := moments.Instance.Detail(42); return e },
		"moments.like":     func() error { _, e := moments.Instance.Like(42); return e },
		"moments.unlike":   func() error { _, e := moments.Instance.Unlike(42); return e },
		"moments.post":     func() error { _, e := moments.Instance.Post("test", nil, nil); return e },
		"moments.upload":   func() error { _, e := moments.Instance.Upload([]byte("test")); return e },
		"moments.sync":     func() error { _, e := moments.Instance.Sync(nil); return e },
		"user.profile":     func() error { _, e := user.Instance.GetProfile(); return e },
		"user.qrcode":      func() error { _, e := user.Instance.GetQRCode(0); return e },
		"user.safety":      func() error { _, e := user.Instance.GetSafetyInfo(); return e },
		"user.hardware":    func() error { _, e := user.Instance.GetBoundHardDevices(); return e },
		"miniapp.record":   func() error { _, e := miniapp.Instance.GetRecord(); return e },
	}
	for name, call := range methods {
		for _, data := range []string{`null`, `{}`, `{"base_response":{}}`, `{"code":-4,"result":{"code":-4},"base_response":{"code":-4}}`} {
			t.Run(name+"/"+data, func(t *testing.T) {
				body.Store(`{"code":0,"data":` + data + `}`)
				before := calls.Load()
				if err := call(); err == nil {
					t.Error("缺失或失败的业务回执被当成成功")
				}
				if calls.Load() != before+1 {
					t.Error("请求必须只发一次")
				}
			})
		}
	}
}
