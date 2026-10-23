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

type loginHost struct {
	t  *testing.T
	qr string
}

func (h loginHost) Execute(_ context.Context, c control.Command) (any, error) {
	switch c.Operation {
	case "login-retry":
		return map[string]bool{"retrying": true}, nil
	case "login-view":
		if h.qr != "" {
			return map[string]any{"online": false, "qr": h.qr, "verify_url": "https://fixture.invalid/private-ticket"}, nil
		}
		return json.RawMessage(`{"online":false,"phase":"verification_required","status":"等待扫码","qr":"data:image/jpg;base64,Zml4dHVyZQ==","verify_url":"https://fixture.invalid/private-ticket"}`), nil
	case "login-verify-code":
		var q struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(c.Params, &q) != nil || q.Code != "123456" {
			h.t.Fatal("验证码未按限定参数传递")
		}
		return map[string]bool{"submitted": true}, nil
	case "login-device-confirm":
		if len(c.Params) != 0 {
			h.t.Fatal("设备确认不应接收浏览器参数")
		}
		return map[string]bool{"submitted": true}, nil
	case "core-status":
		return map[string]any{"state": "waiting_login", "managed": true, "status": "等待扫码", "token": "must-not-leak"}, nil
	case "core-start":
		return map[string]any{"state": "starting", "managed": true, "status": "启动中"}, nil
	case "core-stop":
		return map[string]any{"state": "stopped", "managed": true, "status": "已停止"}, nil
	default:
		h.t.Fatalf("调用了未允许的操作：%s", c.Operation)
		return nil, nil
	}
}

func TestWechatLoginView(t *testing.T) {
	s := newServer(t, false)
	s.host = loginHost{t: t}
	if w := call(t, s, "/api/wechat-login", nil, nil, ""); w.Code != 401 {
		t.Fatal("未登录可读取二维码")
	}
	w := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-admin", "password": "abc123"}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	if denied := call(t, s, "/api/wechat-login", map[string]any{}, nil, ""); denied.Code != 401 {
		t.Fatal("未登录可重新扫码")
	}
	if denied := call(t, s, "/api/wechat-login", map[string]any{}, cookie, "http://evil.invalid"); denied.Code != 403 {
		t.Fatal("跨站可重新扫码")
	}
	if retried := call(t, s, "/api/wechat-login", map[string]any{}, cookie, ""); retried.Code != 200 {
		t.Fatal(retried.Body.String())
	}
	w = call(t, s, "/api/wechat-login", nil, cookie, "")
	var view map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || view["qr"] != "data:image/jpg;base64,Zml4dHVyZQ==" || view["device_confirm_available"] != true || view["verify_url"] != nil || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("二维码未完整传递或允许缓存")
	}
	s.host = loginHost{t: t, qr: "data:image/svg+xml;base64,PHN2Zy8+"}
	w = call(t, s, "/api/wechat-login", nil, cookie, "")
	view = nil
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view["qr"] != nil || view["verify_url"] != nil {
		t.Fatalf("SVG 或验证 URL 不应传给浏览器: %s", w.Body.String())
	}
	s.host = loginHost{t: t}
	if w := call(t, s, "/api/wechat-login/verify-code", map[string]string{"code": "123456"}, nil, ""); w.Code != 401 {
		t.Fatal("未登录可提交验证码")
	}
	if w := call(t, s, "/api/wechat-login/device-confirm", map[string]any{}, cookie, "http://evil.invalid"); w.Code != 403 {
		t.Fatal("跨站可确认设备")
	}
	for _, body := range []map[string]string{{"code": ""}, {"code": "123", "url": "https://fixture.invalid/private-ticket"}} {
		if w := call(t, s, "/api/wechat-login/verify-code", body, cookie, ""); w.Code != 400 {
			t.Fatalf("非法验证码请求未拒绝: %s", w.Body.String())
		}
	}
	if w := call(t, s, "/api/wechat-login/verify-code", map[string]string{"code": "123456"}, cookie, ""); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || len(w.Body.Bytes()) > 128 {
		t.Fatal("验证码提交失败或回传了敏感信息")
	}
	if w := call(t, s, "/api/wechat-login/device-confirm", map[string]string{"url": "https://fixture.invalid/private-ticket"}, cookie, ""); w.Code != 400 {
		t.Fatal("浏览器可指定验证 URL")
	}
	if w := call(t, s, "/api/wechat-login/device-confirm", map[string]any{}, cookie, ""); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("设备确认失败")
	}
	// forbiddenHost 保证脱机预览不调用 core。
	mock := newServer(t, true)
	if w := call(t, mock, "/api/wechat-login", nil, nil, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call(t, mock, "/api/wechat-login", map[string]any{}, nil, ""); w.Code != 409 {
		t.Fatal("Mock 可触发登录重试")
	}
}

func TestCoreControlAdminAndFixedActions(t *testing.T) {
	s := newServer(t, false)
	s.host = loginHost{t: t}
	if w := call(t, s, "/api/core", nil, nil, ""); w.Code != 401 {
		t.Fatal("未登录可读取 core 状态")
	}
	if w := call(t, s, "/api/core", map[string]string{"action": "stop"}, nil, ""); w.Code != 401 {
		t.Fatal("未登录可停止 core")
	}
	setup := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-admin", "password": "abc123"}, nil, "")
	if setup.Code != 200 {
		t.Fatal(setup.Body.String())
	}
	cookie := setup.Result().Cookies()[0]
	if w := call(t, s, "/api/core", map[string]string{"action": "stop"}, cookie, "http://evil.invalid"); w.Code != 403 {
		t.Fatal("跨站可停止 core")
	}
	for _, body := range []map[string]string{{"action": "unknown"}, {"action": "start", "path": "/tmp/other"}} {
		if w := call(t, s, "/api/core", body, cookie, ""); w.Code != 400 {
			t.Fatalf("非法 core 命令未拒绝: %s", w.Body.String())
		}
	}
	w := call(t, s, "/api/core", nil, cookie, "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Body.String())
	}
	var view map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view["state"] != "waiting_login" || view["managed"] != true || view["token"] != nil {
		t.Fatalf("core 状态字段错误或泄露多余字段: %#v", view)
	}
	for action, state := range map[string]string{"start": "starting", "stop": "stopped"} {
		w = call(t, s, "/api/core", map[string]string{"action": action}, cookie, "")
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view["state"] != state {
			t.Fatalf("%s 操作返回错误: %s", action, w.Body.String())
		}
	}
	mock := newServer(t, true)
	if w := call(t, mock, "/api/core", nil, nil, ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call(t, mock, "/api/core", map[string]string{"action": "start"}, nil, ""); w.Code != 409 {
		t.Fatal("Mock 可启动真实 core")
	}
}
