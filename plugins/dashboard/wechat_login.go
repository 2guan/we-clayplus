// 文件名：wechat_login.go
// 主要功能：通过宿主桥读取扫码状态并控制受管 core，只向后台管理员开放。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

func (s *Server) wechatLogin(w http.ResponseWriter, r *http.Request, u User) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, 405, "请求方法不支持")
		return
	}
	if err := requireAdmin(u); err != nil {
		writeError(w, 403, "请使用管理员账号连接微信")
		return
	}
	// 二维码不缓存、不持久化；Mock 不触达真实 core。
	w.Header().Set("Cache-Control", "no-store")
	if s.cfg.Mock {
		if r.Method == http.MethodPost {
			writeError(w, 409, "脱机演示模式不能请求微信登录")
			return
		}
		writeJSON(w, 200, map[string]any{"online": false, "status": "脱机演示模式不连接微信，请在真实模式扫码。"})
		return
	}
	if s.host == nil {
		writeError(w, 503, "控制台尚未连接 gateway")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if r.Method == http.MethodPost {
		_, err := s.host.Execute(ctx, control.Command{Operation: "login-retry"})
		if err != nil {
			writeError(w, 409, "无法重新登录，请确认 core 正在运行并查看本机日志")
			return
		}
		writeJSON(w, 200, map[string]bool{"retrying": true})
		return
	}
	result, err := s.host.Execute(ctx, control.Command{Operation: "login-view"})
	if err != nil {
		writeError(w, 503, "暂时无法读取微信登录状态，请稍后重试")
		return
	}
	// core 的 verify_url 可能含登录票据。浏览器只需要知道是否能在本页确认。
	encoded, err := json.Marshal(result)
	if err != nil {
		writeError(w, 502, "微信登录状态暂不可用")
		return
	}
	var view struct {
		Online             bool   `json:"online"`
		Status             string `json:"status,omitempty"`
		Phase              string `json:"phase,omitempty"`
		CanRetry           bool   `json:"can_retry,omitempty"`
		QR                 string `json:"qr,omitempty"`
		PINRequired        bool   `json:"pin_required,omitempty"`
		VerificationStatus string `json:"verification_status,omitempty"`
		VerifyURL          string `json:"verify_url,omitempty"`
	}
	if err := json.Unmarshal(encoded, &view); err != nil {
		writeError(w, 502, "微信登录状态暂不可用")
		return
	}
	validQR := strings.HasPrefix(view.QR, "data:image/png;base64,") ||
		strings.HasPrefix(view.QR, "data:image/jpeg;base64,") ||
		strings.HasPrefix(view.QR, "data:image/jpg;base64,") ||
		strings.HasPrefix(view.QR, "data:image/gif;base64,") ||
		strings.HasPrefix(view.QR, "data:image/webp;base64,")
	if len(view.QR) > 1<<20 || !validQR {
		view.QR = ""
	}
	writeJSON(w, 200, struct {
		Online                 bool   `json:"online"`
		Status                 string `json:"status,omitempty"`
		Phase                  string `json:"phase,omitempty"`
		CanRetry               bool   `json:"can_retry,omitempty"`
		QR                     string `json:"qr,omitempty"`
		PINRequired            bool   `json:"pin_required,omitempty"`
		VerificationStatus     string `json:"verification_status,omitempty"`
		DeviceConfirmAvailable bool   `json:"device_confirm_available,omitempty"`
	}{view.Online, view.Status, view.Phase, view.CanRetry, view.QR, view.PINRequired, view.VerificationStatus, view.VerifyURL != "" && !view.PINRequired})
}

// wechatVerification 只接收验证码或设备确认，绝不接收客户端提供的 verify_url。
func (s *Server) wechatVerification(w http.ResponseWriter, r *http.Request, u User) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "请求方法不支持")
		return
	}
	if err := requireAdmin(u); err != nil {
		writeError(w, 403, "请使用管理员账号完成微信验证")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if s.cfg.Mock || s.host == nil {
		writeError(w, 409, "当前不能提交微信登录验证")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	operation := "login-device-confirm"
	var params json.RawMessage
	switch r.URL.Path {
	case "/api/wechat-login/verify-code":
		var body struct {
			Code string `json:"code"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil || len(body.Code) == 0 || len(body.Code) > 32 || strings.TrimSpace(body.Code) != body.Code || strings.ContainsAny(body.Code, "\r\n\t") {
			writeError(w, 400, "验证码格式无效")
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeError(w, 400, "验证码格式无效")
			return
		}
		operation = "login-verify-code"
		params, _ = json.Marshal(body)
	case "/api/wechat-login/device-confirm":
		// 请求体必须为空对象；验证 URL 由 gateway 从当前 core 会话读取。
		var body map[string]any
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128))
		if err := decoder.Decode(&body); err != nil || body == nil || len(body) != 0 {
			writeError(w, 400, "设备确认参数无效")
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeError(w, 400, "设备确认参数无效")
			return
		}
	default:
		writeError(w, 404, "路径不存在")
		return
	}
	if _, err := s.host.Execute(ctx, control.Command{Operation: operation, Params: params}); err != nil {
		writeError(w, 409, "微信验证未完成，请核对手机提示并查看本机日志")
		return
	}
	writeJSON(w, 200, map[string]bool{"submitted": true})
}

type coreControlView struct {
	State   string `json:"state"`
	Managed bool   `json:"managed"`
	Status  string `json:"status"`
}

// coreControl 只接受固定操作名，不允许浏览器指定 core 路径、进程或账号目录。
func (s *Server) coreControl(w http.ResponseWriter, r *http.Request, u User) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, 405, "请求方法不支持")
		return
	}
	if err := requireAdmin(u); err != nil {
		writeError(w, 403, "请使用管理员账号管理 core")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if s.cfg.Mock {
		if r.Method == http.MethodPost {
			writeError(w, 409, "脱机演示模式不能控制 core")
			return
		}
		writeJSON(w, 200, coreControlView{State: "stopped", Status: "脱机演示模式不连接微信"})
		return
	}
	if s.host == nil {
		writeError(w, 503, "控制台尚未连接 gateway")
		return
	}
	operation := "core-status"
	if r.Method == http.MethodPost {
		var body struct {
			Action string `json:"action"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			writeError(w, 400, "core 操作参数无效")
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeError(w, 400, "core 操作参数无效")
			return
		}
		switch body.Action {
		case "start":
			operation = "core-start"
		case "stop":
			operation = "core-stop"
		default:
			writeError(w, 400, "core 操作只支持 start 或 stop")
			return
		}
	}
	// 启动可能需要下载已发布的 core，给宿主留出合理时间。
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result, err := s.host.Execute(ctx, control.Command{Operation: operation})
	if err != nil {
		// 宿主错误可能包含本机路径或进程信息，不直接回显到浏览器。
		writeError(w, 409, "core 操作未完成，请查看本机运行日志")
		return
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		writeError(w, 502, "core 状态暂不可用")
		return
	}
	var view coreControlView
	if err := json.Unmarshal(encoded, &view); err != nil {
		writeError(w, 502, "core 状态暂不可用")
		return
	}
	switch view.State {
	case "stopped", "starting", "waiting_login", "online", "stopping", "error", "unmanaged":
	default:
		writeError(w, 502, "core 返回了未知状态")
		return
	}
	writeJSON(w, 200, view)
}
