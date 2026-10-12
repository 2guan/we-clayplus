// 文件名: plugins/dashboard/main.go
// 主要功能: Dashboard 插件入口、生命周期控制、微信能力注入与实时事件监听
// 作者: 2guan

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/we-clay/we-clay/sdk/cdn"
	"github.com/we-clay/we-clay/sdk/chatroom"
	contactsdk "github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/login"
	"github.com/we-clay/we-clay/sdk/message"
	"github.com/we-clay/we-clay/sdk/moments"
	"github.com/we-clay/we-clay/sdk/plugin"
)

// DashboardPlugin 现代化 Web 管理控制台插件
// 实现了 plugin.Plugin, plugin.Lifecycle, plugin.EventPlugin 接口
type DashboardPlugin struct {
	// 配置能力（继承泛型 ConfigAbility）
	plugin.ConfigAbility[Config]

	// 微信与宿主底层能力（Host 会自动反射注入）
	contact  contactsdk.Ability
	chatroom chatroom.Ability
	message  message.Ability
	moments  moments.Ability
	login    login.Ability
	cdn      cdn.Ability
	caller   plugin.CallerAbility

	// 插件内部核心组件
	store  *Store
	sse    *SSEBroker
	server *Server
}

// GetMetadata 返回插件基础元数据
func (p *DashboardPlugin) GetMetadata() *plugin.Metadata {
	return &plugin.Metadata{
		Name:        "dashboard",
		Author:      "2guan",
		Version:     "1.0.0",
		Description: "we-clayplus 现代化全功能后台控制台",
		Priority:    100,
		Next:        true,
		AlwaysRun:   true,
	}
}

// OnLoad 插件初次装载时调用，初始化持久化存储、日志钩子并启动 HTTP 服务器
func (p *DashboardPlugin) OnLoad() error {
	slog.Info("[dashboard] 正在加载控制台插件...")

	// 1. 确保数据目录存在
	dataDir := p.Config.DataDir
	if dataDir == "" {
		dataDir = filepath.Join("runtime", "gateway", "data")
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("创建控制台数据目录失败: %w", err)
	}

	// 2. 初始化 SQLite 存储
	dbPath := filepath.Join(dataDir, "dashboard.db")
	store, err := NewStore(dbPath)
	if err != nil {
		return fmt.Errorf("初始化数据库失败: %w", err)
	}
	p.store = store

	// 3. 初始化 SSE 广播中继器
	p.sse = NewSSEBroker(3000)

	// 4. 将 slog 日志流转发至 SSE 广播环形缓冲区，支持前端实时日志大屏
	origHandler := slog.Default().Handler()
	slog.SetDefault(slog.New(&sseLogHandler{next: origHandler, sse: p.sse}))

	// 5. 初始化并启动 HTTP Web 服务
	p.server = NewServer(p, p.store, p.sse)
	addr := fmt.Sprintf("%s:%d", p.Config.Host, p.Config.Port)
	if err := p.server.Start(addr); err != nil {
		return fmt.Errorf("启动 HTTP 服务失败: %w", err)
	}

	slog.Info("[dashboard] 控制台插件加载完成", "web_url", fmt.Sprintf("http://%s", addr))
	return nil
}

// OnUnload 插件卸载或进程退出时调用，优雅关闭 HTTP 服务和数据库连接
func (p *DashboardPlugin) OnUnload() error {
	slog.Info("[dashboard] 正在卸载控制台插件...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if p.server != nil {
		_ = p.server.Shutdown(ctx)
	}
	if p.store != nil {
		_ = p.store.Close()
	}

	slog.Info("[dashboard] 控制台插件卸载完成")
	return nil
}

// OnEnable 插件启用时的回调钩子
func (p *DashboardPlugin) OnEnable() error {
	return nil
}

// OnDisable 插件停用时的回调钩子
func (p *DashboardPlugin) OnDisable() error {
	return nil
}

// GetSubscriptions 返回插件订阅的所有事件主题（涵盖文本、图片、音视频、名片及系统通知）
func (p *DashboardPlugin) GetSubscriptions() []string {
	return []string{
		message.TypeText.Topic,
		message.TypeImage.Topic,
		message.TypeVoice.Topic,
		message.TypeVideo.Topic,
		message.TypeEmoji.Topic,
		message.TypeLocation.Topic,
		message.TypeFile.Topic,
		message.TypeApplication.Topic,
		message.TypeAppQuote.Topic,
		message.TypeSystem.Topic,
		message.TypeSystemTip.Topic,
		message.TypeAppChatRecord.Topic,
	}
}

// OnEvent 接收微信上游事件，异步落盘归档至 SQLite 并通过 SSE 实时推送到前端控制台
func (p *DashboardPlugin) OnEvent(event *plugin.Event) (bool, error) {
	if event == nil {
		return true, nil
	}

	msgPayload, ok := event.Payload.(*plugin.Event_Message)
	if !ok || msgPayload == nil || msgPayload.Message == nil {
		return true, nil
	}

	msg := msgPayload.Message

	// 判断是否为群聊消息
	isGroup := false
	if msg.Sender != nil && msg.Sender.GetType() == contactsdk.ContactType_CONTACT_TYPE_CHATROOM {
		isGroup = true
	}

	// 提取会话与发送者信息
	var (
		sessionID    string
		senderID     string
		senderName   string
		senderAvatar string
		receiverID   string
		isOutgoing   = false
	)

	// 获取微信登录账号的自身唯一标识
	var selfUsername string
	if p.contact != nil {
		if self := p.contact.GetSelf(); self != nil {
			selfUsername = self.Username
		}
	}

	if isGroup {
		sessionID = msg.Sender.GetUsername()
		receiverID = msg.Receiver.GetUsername()
		if msg.Member != nil {
			senderID = msg.Member.GetUsername()
			senderName = msg.Member.GetDisplayName()
			if senderName == "" {
				senderName = msg.Member.GetNickname()
			}
			senderAvatar = msg.Member.GetAvatar()
		} else {
			senderID = msg.Sender.GetUsername()
			senderName = msg.Sender.GetNickname()
			senderAvatar = msg.Sender.GetAvatar()
		}
		if selfUsername != "" && senderID == selfUsername {
			isOutgoing = true
		}
	} else {
		senderID = msg.Sender.GetUsername()
		senderName = msg.Sender.GetNickname()
		senderAvatar = msg.Sender.GetAvatar()
		receiverID = msg.Receiver.GetUsername()

		if selfUsername != "" && senderID == selfUsername {
			isOutgoing = true
			sessionID = receiverID
		} else {
			sessionID = senderID
		}
	}

	msgTypeStr := "text"
	if msg.Type != nil && msg.Type.Desc != "" {
		msgTypeStr = msg.Type.Desc
	}

	msgTime := time.Now()
	if msg.Timestamp > 0 {
		msgTime = time.Unix(int64(msg.Timestamp), 0)
	}

	chatMsg := &ChatMessage{
		MsgID:        strconv.FormatInt(msg.Id, 10),
		SessionID:    sessionID,
		SenderID:     senderID,
		SenderName:   senderName,
		SenderAvatar: senderAvatar,
		MsgType:      msgTypeStr,
		Content:      msg.Content,
		IsSelf:       isOutgoing,
		IsOutgoing:   isOutgoing,
		CreatedAt:    msgTime,
	}

	// 1. 落盘到 SQLite
	if p.store != nil {
		_ = p.store.SaveChatMessage(chatMsg)
	}

	// 2. 实时推送给 Web 控制台
	if p.sse != nil {
		p.sse.BroadcastMessage(chatMsg)
	}

	// 允许事件链继续传递给其他业务插件（如 AI、forward）
	return true, nil
}

// ----------------- 日志捕获与 SSE 广播集成 -----------------

// sseLogHandler 包装 slog.Handler，将系统日志异步注入 SSE 环形缓冲区
type sseLogHandler struct {
	next slog.Handler
	sse  *SSEBroker
}

func (h *sseLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *sseLogHandler) Handle(ctx context.Context, r slog.Record) error {
	levelStr := "INFO"
	switch {
	case r.Level < slog.LevelInfo:
		levelStr = "DEBUG"
	case r.Level < slog.LevelWarn:
		levelStr = "INFO"
	case r.Level < slog.LevelError:
		levelStr = "WARN"
	default:
		levelStr = "ERROR"
	}

	if h.sse != nil {
		h.sse.BroadcastLog(levelStr, "core", r.Message)
	}

	return h.next.Handle(ctx, r)
}

func (h *sseLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &sseLogHandler{next: h.next.WithAttrs(attrs), sse: h.sse}
}

func (h *sseLogHandler) WithGroup(name string) slog.Handler {
	return &sseLogHandler{next: h.next.WithGroup(name), sse: h.sse}
}

func main() {
	p := &DashboardPlugin{
		ConfigAbility: plugin.ConfigAbility[Config]{
			Config: DefaultConfig(),
		},
	}

	slog.Info("[dashboard] we-clayplus 后台控制台插件启动中...")
	plugin.Start(p)
}
