// 文件名: plugins/ai/main.go
// 主要功能: AI 智能体插件入口、微信底层能力注入、生命周期控制与 RPC 调用暴露
// 作者: 2guan

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/we-clay/we-clay/sdk/cdn"
	"github.com/we-clay/we-clay/sdk/chatroom"
	contactsdk "github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/message"
	"github.com/we-clay/we-clay/sdk/moments"
	"github.com/we-clay/we-clay/sdk/plugin"
)

// AIPlugin 现代化多模态智能体核心执行引擎
// 实现了 plugin.Plugin, plugin.Lifecycle, plugin.EventPlugin, plugin.CalledPlugin 接口
type AIPlugin struct {
	// 配置能力
	plugin.ConfigAbility[Config]

	// 宿主与微信底层能力注入（Host 会自动反射注入）
	message  message.Ability
	contact  contactsdk.Ability
	chatroom chatroom.Ability
	moments  moments.Ability
	cdn      cdn.Ability
	caller   plugin.CallerAbility
	session  plugin.SessionAbility

	// 核心业务组件
	router    *Router
	memory    *MemoryManager
	matrix    *MatrixExecutor
	auditor   *Auditor
	tts       *TTSService
	scheduler *Scheduler
	pipeline  *Pipeline
}

// GetMetadata 返回插件元数据
func (p *AIPlugin) GetMetadata() *plugin.Metadata {
	return &plugin.Metadata{
		Name:        "ai",
		Author:      "2guan",
		Version:     "1.0.0",
		Description: "we-clayplus 微信多模态高情商智能体引擎 (支持mimo/DeepSeek/多模态/矩阵权限/30s-60s防封排队)",
		Priority:    50,
		Next:        true,
		AlwaysRun:   true,
	}
}

// OnLoad 插件初次装载时调用，初始化大模型网关、上下文记忆、安全网关与定时调度器
func (p *AIPlugin) OnLoad() error {
	slog.Info("[ai] 正在加载 AI 智能体插件...")

	// 1. 初始化 SQLite 审计器
	dataDir := p.Config.DataDir
	if dataDir == "" {
		dataDir = filepath.Join("runtime", "gateway", "data")
	}
	auditor, err := NewAuditor(dataDir)
	if err != nil {
		slog.Warn("[ai] 打开 SQLite 审计数据库失败", "err", err)
	}
	p.auditor = auditor

	// 2. 初始化大模型路由网关与容灾调度器
	p.router = NewRouter(&p.Config)

	// 3. 初始化会话滑动窗口状态机
	p.memory = NewMemoryManager(p.Config.ContextRounds)

	// 4. 初始化 AI 能力授权矩阵与拟人化 30s~60s 等待执行器
	p.matrix = NewMatrixExecutor(p.Config.Matrix)

	// 5. 初始化语音合成与直发服务
	p.tts = NewTTSService(p.router, p.Config.Matrix.VoiceReply)

	// 6. 初始化周期性批量调度器 (朋友圈每日巡回)
	p.scheduler = NewScheduler(&p.Config, p.moments, p.router, p.matrix)

	// 7. 组装多模态流水线引擎
	p.pipeline = NewPipeline(
		&p.Config,
		p.router,
		p.memory,
		p.matrix,
		p.auditor,
		p.tts,
		p.message,
		p.contact,
		p.chatroom,
	)

	slog.Info("[ai] AI 智能体插件加载成功",
		"default_provider", p.Config.DefaultProvider,
		"default_model", p.Config.DefaultModel,
		"moments_cron", p.Config.Matrix.Moments.CronSchedule)

	return nil
}

// OnUnload 插件卸载或进程退出时调用，释放后台 Worker 协程与数据库连接
func (p *AIPlugin) OnUnload() error {
	slog.Info("[ai] 正在卸载 AI 智能体插件...")

	if p.scheduler != nil {
		p.scheduler.Close()
	}
	if p.matrix != nil {
		p.matrix.Close()
	}
	if p.auditor != nil {
		_ = p.auditor.Close()
	}

	slog.Info("[ai] AI 智能体插件卸载完成")
	return nil
}

// OnEnable 插件启用时的回调钩子
func (p *AIPlugin) OnEnable() error {
	slog.Info("[ai] AI 智能体插件已启用")
	return nil
}

// OnDisable 插件停用时的回调钩子
func (p *AIPlugin) OnDisable() error {
	slog.Info("[ai] AI 智能体插件已停用")
	return nil
}

// GetSubscriptions 返回插件订阅的所有微信事件
func (p *AIPlugin) GetSubscriptions() []string {
	return []string{
		message.TypeText.Topic,
		message.TypeImage.Topic,
		message.TypeVoice.Topic,
		message.TypeVideo.Topic,
		message.TypeEmoji.Topic,
		message.TypeAppQuote.Topic,
		message.TypeApplication.Topic,
		message.TypeSystem.Topic,
		message.TypeSystemTip.Topic,
		message.TypeVerify.Topic,
	}
}

// OnEvent 接收微信上游事件，转交给流水线处理
func (p *AIPlugin) OnEvent(event *plugin.Event) (bool, error) {
	if p.pipeline == nil {
		return true, nil
	}
	return p.pipeline.HandleEvent(event)
}

// GetCapabilities 声明当前插件对外暴露的 RPC 方法列表
func (p *AIPlugin) GetCapabilities() []string {
	return []string{
		"ai.test_model",
		"ai.get_status",
		"ai.trigger_moments",
	}
}

// OnCall 处理其他插件（如 Dashboard）通过 Host RPC 发送过来的调用请求
func (p *AIPlugin) OnCall(capability string, args map[string]string) (string, []byte, error) {
	switch capability {
	case "ai.test_model":
		// 测试指定模型联通性
		providerName := args["provider"]
		if providerName == "" {
			providerName = p.Config.DefaultProvider
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := p.router.TestProvider(ctx, providerName)
		if err != nil {
			return "application/json", nil, fmt.Errorf("测试模型失败: %w", err)
		}

		resBytes, _ := json.Marshal(map[string]any{
			"status":   "ok",
			"provider": providerName,
			"model":    resp.Model,
			"reply":    resp.Content,
			"latency":  resp.Duration.Milliseconds(),
		})
		return "application/json", resBytes, nil

	case "ai.get_status":
		// 获取 AI 插件实时运行状态
		activeSessions := 0
		if p.memory != nil {
			activeSessions = p.memory.Count()
		}

		resBytes, _ := json.Marshal(map[string]any{
			"active_sessions":  activeSessions,
			"default_provider": p.Config.DefaultProvider,
			"default_model":    p.Config.DefaultModel,
			"moments_enabled":  p.Config.Matrix.Moments.Enabled,
			"moments_schedule": p.Config.Matrix.Moments.CronSchedule,
		})
		return "application/json", resBytes, nil

	case "ai.trigger_moments":
		// 手动触发朋友圈批量巡回
		if p.scheduler != nil {
			p.scheduler.TriggerMomentsPatrol()
		}
		resBytes, _ := json.Marshal(map[string]any{
			"status":  "triggered",
			"message": "朋友圈巡回任务已成功唤醒并开始后台执行",
		})
		return "application/json", resBytes, nil

	default:
		return "", nil, fmt.Errorf("未知的调用方法: %s", capability)
	}
}

func main() {
	p := &AIPlugin{
		ConfigAbility: plugin.ConfigAbility[Config]{
			Config: DefaultConfig(),
		},
	}

	slog.Info("[ai] we-clayplus AI 智能体插件启动中...")
	plugin.Start(p)
}
