// 文件名：wechat_login_test.go
// 主要功能：验证扫码状态的认证、有限桥接操作及 Mock 隔离。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/we-clay/we-clay/sdk/control"
)

type loginHost struct{ t *testing.T }

func (h loginHost) Execute(_ context.Context, c control.Command) (any, error) {
	if c.Operation == "login-restart" {
		return map[string]bool{"restarting": true}, nil
	}
	if c.Operation != "login-view" {
		h.t.Fatal("调用了扫码之外的操作")
	}
	return json.RawMessage(`{"online":false,"status":"等待扫码","qr":"data:image/jpg;base64,Zml4dHVyZQ=="}`), nil
}

func TestWechatLoginView(t *testing.T) {
	s := newServer(t, false)
	s.host = loginHost{t}
	if w := call(t, s, "/api/wechat-login", nil, nil, ""); w.Code != 401 {
		t.Fatal("未登录可读取二维码")
	}
	w := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-admin", "password": "abc123"}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if denied := call(t, s, "/api/wechat-login", map[string]any{}, nil, ""); denied.Code != 401 {
		t.Fatal("未登录可重启")
	}
	if denied := call(t, s, "/api/wechat-login", map[string]any{}, cookie, "http://evil.invalid"); denied.Code != 403 {
		t.Fatal("跨站可重启")
	}
	if restarted := call(t, s, "/api/wechat-login", map[string]any{}, cookie, ""); restarted.Code != 200 {
		t.Fatal(restarted.Body.String())
	}
	w = call(t, s, "/api/wechat-login", nil, cookie, "")
	var view map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || view["qr"] != "data:image/jpg;base64,Zml4dHVyZQ==" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("二维码未完整传递或允许缓存")
	}
	// forbiddenHost 保证脱机预览不调用 core。
	mock := newServer(t, true)
	if w := call(t, mock, "/api/wechat-login", nil, nil, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
}
