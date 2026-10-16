// 文件名：server.go
// 主要功能：提供内嵌 Web 资源、鉴权 API、状态推送和可取消的后台任务生命周期。
// 作者：2guan
package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

//go:embed assets/*
var assets embed.FS

type Server struct {
	cacheMu         sync.Mutex
	reportMu        sync.Mutex
	reportCacheMu   sync.Mutex // ponytail: 单实例串行补取缓存；并发量增加时改为按联系人加锁。
	resendMu        sync.Mutex // 重发互斥，避免重复点击产生多次发送。
	momentPublishMu sync.Mutex // 合并人工重复点击，避免并行发布。
	businessMu      sync.Mutex
	businessSlots   chan struct{}
	businessPending chan struct{}
	startedAt       time.Time // 仅记录本次服务启动，使用单调时钟计算，不写入持久化快照。
	cfg             control.Config
	host            control.Host
	store           *store
	ctx             context.Context
	cancel          context.CancelFunc
	http            *http.Server
	address         string
	authMu          sync.Mutex
	setupMu         sync.Mutex
	sessions        map[string]session
	attempts        map[string]attempt
	eventMu         sync.Mutex
	subscribers     map[chan struct{}]struct{}
	taskMu          sync.Mutex
	taskCancel      map[string]context.CancelFunc
	streamMu        sync.Mutex
	taskStreams     map[string]*taskStream
	workers         sync.WaitGroup
}

// Start 在微信登录前启动。Mock 仅允许回环监听，杜绝意外将演示免登录界面暴露出去。
func Start(parent context.Context, cfg control.Config, host control.Host) (*Server, error) {
	if cfg.Address == "" {
		cfg.Address = "127.0.0.1:8208"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "runtime/console"
	}
	if cfg.Mock {
		h, _, err := net.SplitHostPort(cfg.Address)
		if err != nil || !net.ParseIP(h).IsLoopback() {
			return nil, fmt.Errorf("Mock 模式只允许监听本机回环 IP")
		}
	}
	st, err := openStore(cfg.DataDir, cfg.Mock)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Server{businessSlots: make(chan struct{}, 4), businessPending: make(chan struct{}, 64), startedAt: time.Now(), cfg: cfg, host: host, store: st, ctx: ctx, cancel: cancel, sessions: map[string]session{}, attempts: map[string]attempt{}, subscribers: map[chan struct{}]struct{}{}, taskCancel: map[string]context.CancelFunc{}, taskStreams: map[string]*taskStream{}}
	if host != nil {
		s.host = debugHost{Host: host, s: s}
	}
	if err = s.restoreReportRefusals(); err != nil {
		cancel()
		st.db.Close()
		return nil, err
	}
	if err = s.restoreReplyRetries(); err != nil {
		cancel()
		st.db.Close()
		return nil, err
	}
	if err = s.restoreFileArchive(); err == nil {
		err = s.initBehaviors()
	}
	if err == nil {
		err = s.initBusiness()
	}
	if err == nil {
		err = s.initStickers()
		if err == nil {
			err = s.initPayments()
		}
	}
	if err != nil {
		cancel()
		st.db.Close()
		return nil, err
	}
	if cfg.DisableAI {
		_ = st.change(func(v *State) error {
			for i := range v.Plugins {
				if v.Plugins[i].ID == "ai" {
					v.Plugins[i].Enabled = false
					v.Plugins[i].Status = "stopped"
				}
			}
			return nil
		})
	}
	if !cfg.Disabled {
		listener, e := net.Listen("tcp", cfg.Address)
		if e != nil {
			cancel()
			st.db.Close()
			return nil, e
		}
		s.address = listener.Addr().String()
		s.http = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10}
		s.workers.Add(1)
		go func() {
			defer s.workers.Done()
			if e := s.http.Serve(listener); e != nil && !errors.Is(e, http.ErrServerClosed) {
				slog.Error("控制台服务停止", "error", e)
			}
		}()
		slog.Info("控制台已启动", "address", s.address, "mock", cfg.Mock)
	}
	s.workers.Add(1)
	go s.scheduler()
	return s, nil
}
func (s *Server) Address() string { return s.address }
func (s *Server) Close() {
	s.cancel()
	if s.http != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = s.http.Shutdown(ctx)
		cancel()
	}
	s.workers.Wait()
	_ = s.store.db.Close()
}
func (s *Server) number(key string, fallback float64) float64 {
	v := s.store.snapshot().Settings[key]
	if n, ok := v.(float64); ok {
		return n
	}
	return fallback
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth", s.auth)
	mux.HandleFunc("/api/auth/login", s.auth)
	mux.HandleFunc("/api/auth/setup", s.auth)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		u, ok := s.currentUser(r)
		if !ok {
			writeError(w, 401, "请先登录控制台")
			return
		}
		if r.Method != http.MethodGet && !sameOrigin(r) {
			writeError(w, 403, "请求来源不允许")
			return
		}
		switch r.URL.Path {
		case "/api/cache":
			s.cacheAPI(w, r, u)
		case "/api/logs":
			s.logsAPI(w, r, u)
		case "/api/reports", "/api/reports/action", "/api/reports/cache":
			s.reportsAPI(w, r, u)
		case "/api/memory", "/api/memory/source", "/api/memory/history", "/api/memory/preview":
			s.memoryAPI(w, r, u)
		case "/api/memory/scans":
			s.memoryScanAPI(w, r, u)
		case "/api/memory/trace":
			s.replyTraceAPI(w, r, u)
		case "/api/room":
			s.roomAPI(w, r)
		case "/api/person":
			s.personAPI(w, r)
		case "/api/moments/interact":
			s.momentInteractionAPI(w, r)
		case "/api/moments", "/api/moments/notifications", "/api/moments/cover":
			s.momentsAPI(w, r, u)
		case "/api/payments":
			s.paymentAPI(w, r, u)
		case "/api/stickers":
			s.stickersAPI(w, r, u)
		case "/api/stickers/image":
			s.stickerImage(w, r)
		case "/api/message-media":
			s.messageMedia(w, r)
		case "/api/message-thumbnail":
			s.messageThumbnail(w, r)
		case "/api/wechat-image":
			s.wechatImage(w, r)
		case "/api/wechat-login":
			s.wechatLogin(w, r, u)
		case "/api/business":
			s.businessAPI(w, r, u)
		case "/api/safety", "/api/safety/test":
			s.safetyAPI(w, r, u)
		case "/api/prompts/optimize":
			s.optimizePrompt(w, r, u)
		case "/api/plugins":
			s.pluginList(w, r)
		case "/api/plugin-config":
			s.pluginConfig(w, r, u)
		case "/api/models/discover":
			if r.Method != http.MethodPost {
				writeError(w, 405, "请求方法不支持")
				return
			}
			s.discoverModels(w, r, u)
		case "/api/behaviors":
			if r.Method != http.MethodGet {
				writeError(w, 405, "请求方法不支持")
				return
			}
			writeJSON(w, 200, s.cfg.Capabilities)
		case "/api/state":
			if r.Method != http.MethodGet {
				writeError(w, 405, "请求方法不支持")
				return
			}
			state := s.store.snapshot()
			// 全局刷新只携带近期摘要；日志页面独立查询，详情才读取原文。
			if len(state.Logs) > 100 {
				state.Logs = state.Logs[len(state.Logs)-100:]
			}
			state.Tasks = slices.DeleteFunc(state.Tasks, func(t Task) bool { return t.Status == "deleted" })
			state.Users = []User{}
			if u.Role != "admin" {
				// 调试原文可能包含完整人设与跨条资料，只向管理员提供。
				state.Logs = slices.DeleteFunc(state.Logs, func(v Record) bool { return v.Component == "debug" })
				// 防护自定义规则与会话覆盖仅通过管理员配置接口提供。
				for key := range state.Settings {
					if strings.HasPrefix(key, "prompt_guard.") || strings.HasPrefix(key, "context_cleaner.") || isBusinessPlugin(strings.SplitN(key, ".", 2)[0]) {
						delete(state.Settings, key)
					}
				}
				history := []Change{}
				for _, h := range state.History {
					if !isSafetyPlugin(h.Group) && !isBusinessPlugin(h.Group) {
						history = append(history, h)
					}
				}
				state.History = history
			}
			if u.Role == "admin" {
				state.Users, _ = s.users()
				if s.cfg.Mock {
					state.Users = append([]User{u}, state.Users...)
				}
			}
			// 运行时指标随状态响应返回，刷新页面和重置 Mock 数据都不会重置计时。
			writeJSON(w, 200, struct {
				State
				UptimeSeconds int64 `json:"uptime_seconds"`
			}{State: state, UptimeSeconds: int64(time.Since(s.startedAt) / time.Second)})
		case "/api/schema":
			if r.Method != http.MethodGet {
				writeError(w, 405, "请求方法不支持")
				return
			}
			writeJSON(w, 200, Schema())
		case "/api/events":
			s.events(w, r)
		case "/api/task-stream":
			s.streamTask(w, r)
		case "/api/action":
			if r.Method != http.MethodPost {
				writeError(w, 405, "请求方法不支持")
				return
			}
			s.action(w, r, u)
		case "/api/tasks":
			if r.Method != http.MethodPost {
				writeError(w, 405, "请求方法不支持")
				return
			}
			s.createTask(w, r, u)
		case "/api/auth/logout":
			if r.Method != http.MethodPost {
				writeError(w, 405, "请求方法不支持")
				return
			}
			if c, e := r.Cookie("weclay_session"); e == nil {
				s.authMu.Lock()
				delete(s.sessions, c.Value)
				s.authMu.Unlock()
			}
			http.SetCookie(w, &http.Cookie{Name: "weclay_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
			writeJSON(w, 200, map[string]bool{"ok": true})
		default:
			writeError(w, 404, "接口不存在")
		}
	})
	// 私有媒体从数据目录按随机 ID 读取；不接受路径穿越，也不暴露数据库和凭据文件。
	mux.HandleFunc("/media/", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.currentUser(r); !ok {
			writeError(w, 401, "请先登录")
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/media/")
		if name == "" || filepath.Base(name) != name || strings.Contains(name, "..") {
			writeError(w, 400, "媒体路径无效")
			return
		}
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		if strings.HasSuffix(name, ".bin") {
			// 兼容旧版把 audio/wave 保存成 .bin 的缓存；只放行可识别的 WAV，其他附件仍强制下载。
			header := make([]byte, 512)
			f, err := os.Open(filepath.Join(s.cfg.DataDir, "media", name))
			n := 0
			if err == nil {
				n, _ = f.Read(header)
				f.Close()
			}
			if http.DetectContentType(header[:n]) == "audio/wave" {
				w.Header().Set("Content-Type", "audio/wav")
			} else {
				w.Header().Set("Content-Disposition", "attachment")
				w.Header().Set("Content-Type", "application/octet-stream")
			}
		}
		http.ServeFile(w, r, filepath.Join(s.cfg.DataDir, "media", name))
	})
	root, _ := fs.Sub(assets, "assets")
	files := http.FileServer(http.FS(root))
	mux.Handle("/", files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock 免登录仅服务本机；同时校验 Host，避免 DNS rebinding 绕过回环监听。
		if s.cfg.Mock {
			h := r.Host
			if v, _, e := net.SplitHostPort(h); e == nil {
				h = v
			}
			if h != "localhost" && !net.ParseIP(h).IsLoopback() {
				writeError(w, 403, "Mock 仅允许本机访问")
				return
			}
		}
		// 同源部署并限制内容来源，聊天文本不会作为 HTML 执行。
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob: data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		mux.ServeHTTP(w, r)
	})
}

// taskStream 只在内存中暂存单个洞察任务的增量，最终报告仍由任务完成路径持久化。
type taskStream struct {
	text string
	done bool
	wake chan struct{}
}

func (s *Server) startTaskStream(id string) {
	s.streamMu.Lock()
	s.taskStreams[id] = &taskStream{wake: make(chan struct{}, 1)}
	s.streamMu.Unlock()
}

func (s *Server) appendTaskStream(id, delta string) {
	if delta == "" {
		return
	}
	s.streamMu.Lock()
	stream := s.taskStreams[id]
	if stream != nil && !stream.done && len(stream.text)+len(delta) <= 4<<20 {
		stream.text += delta
		close(stream.wake)
		stream.wake = make(chan struct{})
	}
	s.streamMu.Unlock()
}

func (s *Server) resetTaskStream(id string) {
	s.streamMu.Lock()
	if stream := s.taskStreams[id]; stream != nil && !stream.done {
		stream.text = ""
		close(stream.wake)
		stream.wake = make(chan struct{})
	}
	s.streamMu.Unlock()
}

func (s *Server) finishTaskStream(id string) {
	s.streamMu.Lock()
	if stream := s.taskStreams[id]; stream != nil {
		stream.done = true
		close(stream.wake)
		time.AfterFunc(30*time.Second, func() {
			s.streamMu.Lock()
			if s.taskStreams[id] == stream {
				delete(s.taskStreams, id)
			}
			s.streamMu.Unlock()
		})
	}
	s.streamMu.Unlock()
}

func (s *Server) streamTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "请求方法不支持")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, 400, "任务 ID 无效")
		return
	}
	f, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "当前服务不支持流式响应")
		return
	}
	s.streamMu.Lock()
	stream := s.taskStreams[id]
	s.streamMu.Unlock()
	if stream == nil {
		writeError(w, 404, "任务流已过期或不存在")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	last := 0
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	for {
		if _, valid := s.currentUser(r); !valid {
			return
		}
		s.streamMu.Lock()
		text, done, wake := stream.text, stream.done, stream.wake
		s.streamMu.Unlock()
		if len(text) < last {
			payload, _ := json.Marshal(map[string]string{"text": text})
			_, _ = fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", payload)
			last = len(text)
			f.Flush()
		} else if len(text) > last {
			payload, _ := json.Marshal(map[string]string{"text": text[last:]})
			_, _ = fmt.Fprintf(w, "event: delta\ndata: %s\n\n", payload)
			last = len(text)
			f.Flush()
		}
		if done {
			fmt.Fprint(w, "event: done\ndata: {}\n\n")
			f.Flush()
			return
		}
		select {
		case <-s.ctx.Done():
			return
		case <-r.Context().Done():
			return
		case <-wake:
		case <-tick.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			f.Flush()
		}
	}
}
func (s *Server) notify() {
	s.eventMu.Lock()
	defer s.eventMu.Unlock()
	for ch := range s.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "请求方法不支持")
		return
	}
	f, ok := w.(http.Flusher)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := make(chan struct{}, 1)
	s.eventMu.Lock()
	s.subscribers[ch] = struct{}{}
	s.eventMu.Unlock()
	defer func() { s.eventMu.Lock(); delete(s.subscribers, ch); s.eventMu.Unlock() }()
	fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	f.Flush()
	tick := time.NewTicker(25 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-r.Context().Done():
			return
		case <-ch:
			if _, valid := s.currentUser(r); !valid {
				return
			}
			fmt.Fprint(w, "event: update\ndata: {}\n\n")
			f.Flush()
		case <-tick.C:
			if _, valid := s.currentUser(r); !valid {
				return
			}
			fmt.Fprint(w, ": heartbeat\n\n")
			f.Flush()
		}
	}
}
func addLog(v *State, level, component, text string) {
	v.Logs = append(v.Logs, Record{ID: randomID(), Time: time.Now().Format(time.RFC3339), Level: level, Component: component, Message: text})
	if len(v.Logs) > 2000 {
		v.Logs = v.Logs[len(v.Logs)-2000:]
	}
}
func (s *Server) SetAccount(a Account) error {
	if previous := s.store.snapshot().Account.ID; previous != "" && a.ID != "" && previous != a.ID {
		// 切回同一账号也不能复活旧任务；取消令牌比账号ID比较更强。
		s.taskMu.Lock()
		for _, cancel := range s.taskCancel {
			cancel()
		}
		s.taskMu.Unlock()
	}
	err := s.store.switchAccount(a)
	if err == nil {
		err = s.restoreFileArchive()
	}
	s.notify()
	return err
}
func (s *Server) SetContacts(contacts []Contact) error {
	err := s.store.change(func(v *State) error {
		for i := range contacts {
			for _, old := range v.Contacts {
				if old.ID == contacts[i].ID {
					if (contacts[i].Name == "" || contacts[i].Name == contacts[i].ID) && old.Name != "" && old.Name != old.ID {
						contacts[i].Name = old.Name
					}
					// 微信已知状态优先，本地旧置顶不能覆盖远端取消置顶。
					if contacts[i].WechatPinned == nil {
						contacts[i].WechatPinned = old.WechatPinned
					}
					if contacts[i].Muted == nil {
						contacts[i].Muted = old.Muted
					}
					if contacts[i].WechatPinned != nil {
						contacts[i].Pinned = *contacts[i].WechatPinned
					} else {
						contacts[i].Pinned = old.Pinned
					}
					contacts[i].Mode = old.Mode
					contacts[i].Preview = old.Preview
					contacts[i].Unread = old.Unread
					contacts[i].Time = old.Time
				}
			}
		}
		for i := range contacts {
			classifyEnterpriseContact(&contacts[i])
		}
		v.Contacts = contacts
		// 回填联系人同步之前到达的消息，避免聊天列表因空通讯录而消失。
		for _, m := range v.Messages {
			ensureConversation(v, m)
			applyPublisher(v, m)
			for i := range v.Contacts {
				if v.Contacts[i].ID == m.Target && m.Time >= v.Contacts[i].Time {
					v.Contacts[i].Time = m.Time
					v.Contacts[i].Preview = m.Content
				}
			}
		}
		return nil
	})
	s.notify()
	return err
}

// Record 在 ACK 之前持久化去重记录；生成任务异步执行，不阻塞微信事件流。
func (s *Server) Record(m Message) error {
	account := s.store.snapshot().Account.ID
	if !m.Self && m.SenderID == "" {
		for _, c := range s.store.snapshot().Contacts {
			if c.ID == m.Target && c.Kind == "friend" {
				m.SenderID = m.Target
				break
			}
		}
	}
	if m.Kind == "system" {
		if id, content, ok := parseRevokeNotice(m.Content); ok && id != "" {
			err := s.store.change(func(v *State) error {
				if v.Account.ID != account {
					return fmt.Errorf("账号已切换，停止旧消息归档")
				}
				if account != "" {
					if e := s.store.recordMemoryRevokeLocked(account, m.Target, id, m.ID); e != nil {
						return e
					}
				}
				applyRevokeNotice(v, m.Target, id, content)
				return nil
			})
			if err != nil {
				return err
			}
			if account != "" {
				if err = s.recordMemoryArchive(account, m); err != nil {
					return err
				}
			}
			s.notify()
			return nil
		}
	}
	var keep bool
	m, keep = ParseWechatMessage(m, m.Content)
	if !keep {
		return nil
	}
	m = resolveNotice(s.store.snapshot(), m)
	if err := s.prepareArticleThumbnails(&m); err != nil {
		return err
	}
	added := false
	deleted := false
	err := s.store.change(func(v *State) error {
		if v.Account.ID != account {
			return fmt.Errorf("账号已切换，停止旧消息归档")
		}
		if account != "" {
			if m.Rich != nil && m.Rich.QuoteID != "" {
				removed, e := s.store.memoryDeletedLocked(account, m.Target, m.Rich.QuoteID)
				if e != nil {
					return e
				}
				revoked, e := s.store.memoryRevokedLocked(account, m.Target, m.Rich.QuoteID)
				if e != nil {
					return e
				}
				if removed || revoked {
					m.Rich.QuoteUnavailable, m.Rich.Quote = true, ""
				}
			}
			var e error
			deleted, e = s.store.memoryDeletedLocked(account, m.Target, m.ID)
			if e != nil || deleted {
				return e
			}
			revoked, e := s.store.memoryRevokedLocked(account, m.Target, m.ID)
			if e != nil {
				return e
			}
			if revoked {
				m.Status = "revoked_by_other"
				m.Content = "[消息已撤回]"
			}
		}
		for _, old := range v.Messages {
			if old.ID == m.ID && old.Target == m.Target {
				return nil
			}
		}
		ensureConversation(v, m)
		applyPublisher(v, m)
		v.Messages = append(v.Messages, m)
		for i := range v.Contacts {
			if v.Contacts[i].ID == m.Target {
				v.Contacts[i].Preview = m.Content
				v.Contacts[i].Time = m.Time
				if !m.Self {
					v.Contacts[i].Unread++
				}
			}
		}
		if !v.Mock {
			updateMessagePoint(v, m.Time)
		}
		added = true
		return nil
	})
	if err != nil {
		return err
	}
	if deleted {
		return nil
	}
	// 归档索引可在事件重放时补写；只有首次接收才执行记忆提取和回复。
	if account != "" {
		if err = s.recordMemoryArchive(account, m); err != nil {
			return err
		}
	}
	if added {
		if err = s.rememberExplicit(m); err != nil {
			_ = s.store.change(func(v *State) error {
				addLog(v, "WARN", "memory", "明确记忆请求未生效，请在AI记忆页核对")
				return nil
			})
		}
		s.notify()
		if !s.dispatchBusiness(m) {
			s.autoReply(m)
		}
	}
	return nil
}
func (s *Server) saveMedia(data []byte, contentType string) (string, error) {
	ext := ".bin"
	// Go 嗅探 WAV 返回 audio/wave；显式指定扩展名，避免依赖系统 MIME 数据库。
	if contentType == "audio/wave" || contentType == "audio/x-wav" {
		contentType = "audio/wav"
	}
	if extensions, _ := mime.ExtensionsByType(strings.Split(contentType, ";")[0]); len(extensions) > 0 {
		ext = extensions[0]
	}
	if contentType == "audio/wav" {
		ext = ".wav"
	}
	// HTML/SVG 等主动内容作为附件下载，不能在同源页面中执行。
	if !strings.HasPrefix(contentType, "image/") && !strings.HasPrefix(contentType, "audio/") && !strings.HasPrefix(contentType, "video/") || strings.Contains(contentType, "svg") {
		ext = ".bin"
	}
	dir := filepath.Join(s.cfg.DataDir, "media")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	name := randomID() + ext
	if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
		return "", err
	}
	return "/media/" + name, nil
}

// updateMessagePoint 根据已去重的归档计算收发量，发送失败或未知回执不计入成功发送。
func updateMessagePoint(v *State, stamp string) {
	if len(stamp) < 10 {
		return
	}
	date := stamp[:10]
	index := -1
	for i, p := range v.Trend {
		if p.Date == date {
			index = i
			break
		}
	}
	if index < 0 {
		v.Trend = append(v.Trend, Point{Date: date})
		index = len(v.Trend) - 1
	}
	v.Trend[index].Received = 0
	v.Trend[index].Sent = 0
	for _, m := range v.Messages {
		if strings.HasPrefix(m.Time, date) {
			if !m.Self {
				v.Trend[index].Received++
			} else if m.Status == "sent" {
				v.Trend[index].Sent++
			}
		}
	}
}
