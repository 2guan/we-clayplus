// 文件名: plugins/dashboard/server.go
// 主要功能: Dashboard HTTP 服务器生命周期、路由挂载与前端静态资源托管
// 作者: 2guan

package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

//go:embed all:web
var webFS embed.FS

// Server 封装 Dashboard 的 HTTP Web 服务及底层依赖
type Server struct {
	plugin    *DashboardPlugin // 插件宿主与能力句柄
	store     *Store           // SQLite 数据存储引擎
	sse       *SSEBroker       // 实时事件与日志广播管道
	startTime time.Time        // 服务启动时间戳
	httpSrv   *http.Server     // 底层标准库 HTTP 服务实例
}

// NewServer 创建并初始化 Web 控制台服务器
func NewServer(p *DashboardPlugin, store *Store, sse *SSEBroker) *Server {
	return &Server{
		plugin:    p,
		store:     store,
		sse:       sse,
		startTime: time.Now(),
	}
}

// Start 启动 HTTP 监听与服务协程
func (s *Server) Start(addr string) error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	// 包装全局中间件（CORS 跨域支持与请求恢复）
	rootHandler := s.wrapGlobalMiddleware(mux)

	s.httpSrv = &http.Server{
		Addr:              addr,
		Handler:           rootHandler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	go func() {
		slog.Info("[dashboard] HTTP 控制台服务已就绪", "addr", addr)
		if err := s.httpSrv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("[dashboard] HTTP 控制台异常退出", "err", err)
		}
	}()

	return nil
}

// Shutdown 优雅关闭 HTTP 服务并释放连接
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpSrv != nil {
		slog.Info("[dashboard] 正在优雅关闭 HTTP 控制台服务...")
		return s.httpSrv.Shutdown(ctx)
	}
	return nil
}

// registerRoutes 集中注册所有 RESTful API 路由与前端静态资源托管
func (s *Server) registerRoutes(mux *http.ServeMux) {
	// ----------------- 公开认证与首次向导接口 -----------------
	mux.HandleFunc("GET /api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("POST /api/auth/setup", s.handleAuthSetup)
	mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)

	// ----------------- 登录用户通用接口（操作员与管理员均可访问） -----------------
	mux.HandleFunc("GET /api/auth/profile", s.AuthMiddleware(s.handleAuthProfile))
	mux.HandleFunc("POST /api/auth/profile", s.AuthMiddleware(s.handleAuthUpdateProfile))
	mux.HandleFunc("POST /api/auth/password", s.AuthMiddleware(s.handleAuthUpdatePassword))

	// 运行总览
	mux.HandleFunc("GET /api/overview", s.AuthMiddleware(s.handleOverview))

	// 微信高仿工作台与聊天
	mux.HandleFunc("GET /api/chats/sessions", s.AuthMiddleware(s.handleChatSessions))
	mux.HandleFunc("GET /api/chats/messages", s.AuthMiddleware(s.handleChatMessages))
	mux.HandleFunc("POST /api/chats/send", s.AuthMiddleware(s.handleChatSend))
	mux.HandleFunc("POST /api/chats/revoke", s.AuthMiddleware(s.handleChatRevoke))

	// 朋友圈互动
	mux.HandleFunc("GET /api/moments/user", s.AuthMiddleware(s.handleMomentsUser))
	mux.HandleFunc("POST /api/moments/like", s.AuthMiddleware(s.handleMomentsLike))
	mux.HandleFunc("POST /api/moments/comment", s.AuthMiddleware(s.handleMomentsComment))

	// 通讯录与群聊
	mux.HandleFunc("GET /api/contacts", s.AuthMiddleware(s.handleContactsList))
	mux.HandleFunc("POST /api/contacts/remark", s.AuthMiddleware(s.handleContactSetRemark))
	mux.HandleFunc("GET /api/chatrooms/members", s.AuthMiddleware(s.handleChatroomMembers))

	// 好友/群聊专属模型与能力策略
	mux.HandleFunc("GET /api/targets/override", s.AuthMiddleware(s.handleTargetOverrideGet))
	mux.HandleFunc("POST /api/targets/override", s.AuthMiddleware(s.handleTargetOverrideSave))

	// 插件中心
	mux.HandleFunc("GET /api/plugins", s.AuthMiddleware(s.handlePluginsList))

	// 实时日志查询与导出
	mux.HandleFunc("GET /api/logs/recent", s.AuthMiddleware(s.handleLogsRecent))
	mux.HandleFunc("GET /api/logs/export", s.AuthMiddleware(s.handleLogsExport))

	// 微信底层状态与扫码登录
	mux.HandleFunc("GET /api/wechat/qrcode", s.AuthMiddleware(s.handleWeChatQRCode))
	mux.HandleFunc("GET /api/wechat/status", s.AuthMiddleware(s.handleWeChatStatus))

	// 系统设置（读：所有登录用户，写：仅限管理员）
	mux.HandleFunc("GET /api/settings", s.AuthMiddleware(s.handleSettingsGet))
	mux.HandleFunc("POST /api/settings", s.RequireAdmin(s.handleSettingsSave))

	// AI 权限矩阵与全维度拟人延时配置
	mux.HandleFunc("GET /api/settings/matrix", s.AuthMiddleware(s.handleMatrixSettingsGet))
	mux.HandleFunc("POST /api/settings/matrix", s.RequireAdmin(s.handleMatrixSettingsSave))

	// 实时 SSE 推送管道（携带 URL query token 校验）
	mux.HandleFunc("GET /api/sse", s.AuthMiddleware(s.sse.ServeHTTP))

	// ----------------- 仅管理员独占接口 (RBAC Admin) -----------------
	mux.HandleFunc("GET /api/users", s.RequireAdmin(s.handleUsersList))
	mux.HandleFunc("POST /api/users/add", s.RequireAdmin(s.handleUsersAdd))
	mux.HandleFunc("POST /api/users/update", s.RequireAdmin(s.handleUsersUpdate))
	mux.HandleFunc("POST /api/users/reset-password", s.RequireAdmin(s.handleUsersResetPassword))
	mux.HandleFunc("POST /api/users/delete", s.RequireAdmin(s.handleUsersDelete))

	// ----------------- 前端单页应用 (SPA) 与静态文件挂载 -----------------
	subFS, err := fs.Sub(webFS, "web")
	if err != nil {
		slog.Error("[dashboard] 提取嵌入式 web 资源子目录失败", "err", err)
	}

	fileServer := http.FileServer(http.FS(subFS))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// API 路径若未匹配则返回 404 JSON
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.writeJSON(w, http.StatusNotFound, APIResponse{Code: 404, Message: "API 路径不存在"})
			return
		}

		// 清理路径尝试寻找静态文件
		cleanedPath := strings.TrimPrefix(r.URL.Path, "/")
		if cleanedPath == "" {
			cleanedPath = "index.html"
		}

		// 检查静态文件是否存在
		if subFS != nil {
			f, err := subFS.Open(cleanedPath)
			if err == nil {
				_ = f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// 未匹配到的路由兜底回退至 index.html (SPA Fallback)
		r.URL.Path = "/"
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		fileServer.ServeHTTP(w, r)
	})
}

// wrapGlobalMiddleware 包装全局 CORS 和 Panic 恢复中间件
func (s *Server) wrapGlobalMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS 跨域处理
		if s.plugin.Config.EnableCORS {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		// 捕获异常防止崩溃
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("[dashboard] HTTP 请求处理发生严重恐慌", "err", rec, "path", r.URL.Path)
				s.writeJSON(w, http.StatusInternalServerError, APIResponse{
					Code:    500,
					Message: "服务器内部异常",
				})
			}
		}()

		next.ServeHTTP(w, r)
	})
}
