// 文件名：wechat_login.go
// 主要功能：通过现有宿主桥读取 core 扫码状态，只向后台管理员开放。
// 作者：2guan
package dashboard

import (
	"context"
	"net/http"
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
		writeJSON(w, 200, map[string]any{"online": false, "status": "脱机演示模式不连接微信，请在真实模式扫码。"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if r.Method == http.MethodPost {
		_, err := s.host.Execute(ctx, control.Command{Operation: "login-restart"})
		if err != nil {
			writeError(w, 409, err.Error())
			return
		}
		writeJSON(w, 200, map[string]bool{"restarting": true})
		return
	}
	result, err := s.host.Execute(ctx, control.Command{Operation: "login-view"})
	if err != nil {
		writeError(w, 503, "暂时无法读取微信登录状态，请稍后重试")
		return
	}
	writeJSON(w, 200, result)
}
