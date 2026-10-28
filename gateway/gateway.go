// 文件名：gateway.go
// 主要功能：组织嵌入式 Run 生命周期、控制台及受管 core 的启动与登录。
// 作者：2guan
// Package gateway 提供可嵌入应用的机器人及插件运行入口，通过 gRPC 连接独立 core。
// 导入此包不会启动服务；调用方负责 context、信号及自身进程的退出策略。
package gateway

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/ability"
	chatroomability "github.com/we-clay/we-clay/gateway/internal/ability/chatroom"
	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	messageability "github.com/we-clay/we-clay/gateway/internal/ability/message"
	eventsync "github.com/we-clay/we-clay/gateway/internal/ability/sync"
	"github.com/we-clay/we-clay/gateway/internal/api"
	userapi "github.com/we-clay/we-clay/gateway/internal/api/user"
	"github.com/we-clay/we-clay/gateway/internal/config"
	consolebridge "github.com/we-clay/we-clay/gateway/internal/console"
	"github.com/we-clay/we-clay/gateway/internal/coreprocess"
	"github.com/we-clay/we-clay/gateway/internal/plugin"
	"github.com/we-clay/we-clay/plugins/dashboard"
	sdkcontact "github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/message"
	sdkplugin "github.com/we-clay/we-clay/sdk/plugin"
)

// Config 是 gateway 的运行配置。零值使用本机 core 及 runtime/gateway 目录。
type Config = config.Config

// CoreConfig 指定下载版本和本地目录；设置 Config.Core 后由 Run 管理 core 生命周期。
type CoreConfig = config.CoreConfig

// OSSConfig 指定私有 OSS 中的 core 下载位置。
type OSSConfig = config.OSSConfig

// ConnectionState 只反映 core 的登录状态；不代表 gateway 缓存与插件已就绪。
type ConnectionState = coreprocess.ConnectionState

type coreSessionObserver struct {
	clientReady  func(*api.Client, Config)
	clientClosed func()
	connection   func(ConnectionState)
	ready        func()
}

// ErrAlreadyRunning 表示本进程已有一个 Run 正在启动、运行或退出。
var ErrAlreadyRunning = errors.New("本进程已有 gateway 正在运行")

// ErrLoginRequired 可通过 errors.Is 识别；保留 core 和设备资料，由用户重新扫码。
var ErrLoginRequired = coreprocess.ErrLoginRequired
var runMu sync.Mutex

// LoadConfig 可选地从 TOML 文件读取配置；直接传 Config 无需配置文件。
// 相对目录按调用时的进程工作目录解析。
func LoadConfig(path string) (Config, error) { return config.Load(path) }

// Run 阻塞运行，直到 ctx 取消或出现不可恢复的错误。正常取消返回 nil。
// Config.Core 非 nil 时下载、校验并启动 core，等待登录；退出时回收该子进程。
// Config.Core 为 nil 时连接已启动且已登录的 core，不管理其进程。
// 两种模式均不调用微信退出登录接口。
// 每个进程同时允许一个 Run；退出清理完成后可以再次调用。
func Run(ctx context.Context, cfg Config, options ...Option) (err error) {
	opts := runOptions{}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	if ctx == nil {
		return errors.New("gateway.Run 需要非 nil context")
	}
	if !runMu.TryLock() {
		return ErrAlreadyRunning
	}
	defer runMu.Unlock()
	if ctx.Err() != nil {
		return nil
	}
	// 子 context 随 Run 返回取消，避免启动失败后留下请求。
	runCtx, cancelCause := context.WithCancelCause(ctx)
	cancel := func() { cancelCause(nil) }
	defer func() {
		if cause := context.Cause(runCtx); cause != nil && !errors.Is(cause, context.Canceled) {
			err = cause
		}
		cancel()
		if ctx.Err() != nil {
			err = nil
		}
	}()
	cfg, err = cfg.Resolve()
	if err != nil {
		return err
	}
	if opts.coreConsoleControl && (cfg.Core == nil || !cfg.Core.KeepAlive || opts.withoutPlugins || opts.withoutDashboard || (cfg.Console != nil && cfg.Console.Disabled)) {
		return errors.New("控制台 core 管理仅支持启用 Dashboard 的本机 KeepAlive core")
	}
	var consoleController *coreConsoleController
	if opts.coreConsoleControl {
		consoleController = newCoreConsoleController(runCtx, cfg, opts)
		// Accessors remain unchanged until Dashboard.Close has drained HTTP handlers.
		// This defer is registered before the Dashboard's Close defer below.
		oldCore, oldConfig := api.GetCore, config.Get
		api.GetCore = func() *api.Client {
			if client := consoleController.client.Load(); client != nil {
				return client
			}
			return &api.Client{}
		}
		config.Get = func() *config.Config {
			if active := consoleController.config.Load(); active != nil {
				return active
			}
			return &consoleController.cfg
		}
		defer func() { api.GetCore, config.Get = oldCore, oldConfig }()
	}
	// 基础插件早于 core 登录启动；显式 Mock 在下载和建连之前进入独立路径。
	var bridge *consolebridge.Bridge
	keepConsole := false
	if !opts.withoutPlugins {
		cc := control.Config{DataDir: filepath.Join(filepath.Dir(cfg.DataDir), "console")}
		if cfg.Console != nil {
			cc = *cfg.Console
			if cc.DataDir == "" {
				cc.DataDir = filepath.Join(filepath.Dir(cfg.DataDir), "console")
			}
		}
		cc.Capabilities = dashboardCapabilities()
		cc.Disabled = cc.Disabled || opts.withoutDashboard
		cc.DisableAI = cc.DisableAI || opts.withoutAI
		keepConsole = !cc.Disabled
		bridge = &consolebridge.Bridge{}
		bridge.SetCoreAddress(cfg.CoreAddress)
		dashboardServer, startErr := dashboard.Start(runCtx, cc, bridge)
		if startErr != nil {
			return fmt.Errorf("控制台启动失败: %w", startErr)
		}
		bridge.Dashboard = dashboardServer
		defer func() { bridge.Ready.Store(false); dashboardServer.Close() }()
		if cc.Mock {
			<-runCtx.Done()
			return nil
		}
	}
	if cfg.Console != nil && cfg.Console.Mock {
		return errors.New("Mock 模式需要启用基础插件，不能与 WithoutPlugins 同时使用")
	}
	if opts.coreConsoleControl {
		return runCoreConsoleControl(consoleController, bridge)
	}
	return runCoreSession(runCtx, cancelCause, cfg, opts, bridge, keepConsole, nil)
}

// runCoreSession 只拥有本轮 core 连接和业务资源。控制台可跨轮次继续运行。
func runCoreSession(runCtx context.Context, cancelCause context.CancelCauseFunc, cfg Config, opts runOptions, bridge *consolebridge.Bridge, keepConsole bool, observer *coreSessionObserver) (err error) {
	cancel := func() { cancelCause(nil) }
	defer cancel()
	if bridge != nil {
		// 管理模式复用控制台 Bridge；上一轮 WaitContactSync 已排空后台任务。
		bridge.BeginContactSync()
	}
	var managed *coreprocess.Managed
	if cfg.Core != nil {
		if cfg.Core.KeepAlive {
			cfg, err = coreprocess.EnsureIndependent(runCtx, cfg)
			if err != nil {
				return err
			}
			go func() {
				if err := coreprocess.WaitIndependent(runCtx, cfg); runCtx.Err() == nil {
					cancelCause(err)
				}
			}()
		} else {
			binary, downloadErr := coreprocess.Ensure(runCtx, *cfg.Core)
			if downloadErr != nil {
				return downloadErr
			}
			managed, err = coreprocess.StartManaged(runCtx, binary, cfg, cancelCause)
			if err != nil {
				return err
			}
			defer func() { cancel(); managed.Close() }()
		}
	}
	if observer == nil {
		oldConfig := config.Get
		config.Get = func() *config.Config { return &cfg }
		defer func() { config.Get = oldConfig }()
	}
	client, err := api.NewConfiguredClient(runCtx, &cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	if bridge != nil {
		bridge.SetCoreAddress(cfg.CoreAddress)
	}
	if observer == nil {
		oldClient := api.GetCore
		api.GetCore = func() *api.Client { return client }
		defer func() { api.GetCore = oldClient }()
	} else {
		observer.clientReady(client, cfg)
		defer observer.clientClosed()
	}
	if bridge != nil && managed != nil {
		// 只有本次 Run 管理的 core 才开放重启；在线账号不能被此入口中断。
		bridge.SetLoginRestarter(func(ctx context.Context) error {
			return managed.RestartLogin(ctx, func(ctx context.Context) error {
				var view struct {
					Online bool   `json:"online"`
					Status string `json:"status"`
				}
				if err := client.Get("/api/login/view").WithContext(ctx).DoJson(&view); err != nil {
					return fmt.Errorf("暂时无法核实 core 登录状态，请稍后重试")
				}
				if view.Online || !strings.HasPrefix(view.Status, "登录流程已暂停") {
					return fmt.Errorf("仅在登录流程暂停时允许重新扫码")
				}
				return nil
			})
		})
		defer bridge.SetLoginRestarter(nil)
	}

	messageability.ResetReceipts()
	defer messageability.ResetReceipts()
	messageability.SetOutboundReady(false)
	defer messageability.SetOutboundReady(false)
	var handlerMu sync.Mutex
	callHandler := func(name string, fn func() error) error {
		handlerMu.Lock()
		defer handlerMu.Unlock()
		return invokeHandler(name, fn)
	}
	var lastConnection *ConnectionState
	reportConnection := func(s ConnectionState) error {
		// 本机状态每两秒读取；只向应用报告实际变化。
		handlerMu.Lock()
		defer handlerMu.Unlock()
		if lastConnection != nil && *lastConnection == s {
			return nil
		}
		lastConnection = &s
		if observer != nil && observer.connection != nil {
			observer.connection(s)
		}
		if h := opts.handlers.OnConnectionChanged; h != nil {
			return invokeHandler("OnConnectionChanged", func() error { return h(runCtx, s) })
		}
		return nil
	}
	if cfg.Core != nil {
		if err = coreprocess.WaitOnlineNotify(runCtx, client, *cfg.Core, keepConsole, reportConnection); err != nil {
			cancel()
			return err
		}
	}
	profile, err := userapi.Get().GetProfile()
	if err != nil {
		return fmt.Errorf("请先启动 core 并完成微信登录: %w", err)
	}
	info, ext := profile.GetUserInfo(), profile.GetUserInfoExt()
	username := info.GetUserName().GetValue()
	if username == "" {
		return errors.New("core 未返回已登录用户")
	}
	// 不同账号使用不同缓存，切换 core 时不读到前一账号的联系人。
	cacheDir := filepath.Join(cfg.DataDir, fmt.Sprintf("%x", sha256.Sum256([]byte(username))))
	if err = os.MkdirAll(cacheDir, 0700); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}
	contactability.SetSelf(&sdkcontact.SelfInfo{Username: username, Nickname: info.GetNickName().GetValue(), Alias: info.GetAlias(), Avatar: ext.GetSmallAvatarUrl(), Uin: info.GetUin(), Email: info.GetEmail().GetValue(), Mobile: info.GetMobile().GetValue()})
	defer contactability.SetSelf(nil)
	if bridge != nil {
		contactability.LoadCache(cacheDir)
	} else {
		contactability.Initial(cacheDir)
	}
	chatroomability.Initial(cacheDir)
	defer ability.Destroy()
	// 启动失败也回收已经启动的插件；停止回调先于缓存落盘及连接释放。
	defer func() { messageability.SetOutboundReady(false); cancel(); plugin.Destroy() }()
	if !opts.withoutPlugins {
		authKeyEnv := "WE_CLAY_AUTH_KEY"
		if cfg.Core != nil {
			authKeyEnv = cfg.Core.AuthKeyEnv
		}
		if err = plugin.Initial(runCtx, cfg.PluginsDir, !opts.withoutExternalPlugins, authKeyEnv); err != nil {
			return fmt.Errorf("插件初始化失败: %w", err)
		}
		if bridge != nil {
			if err = plugin.RegisterFoundation(bridge.Dashboard, !opts.withoutDashboard && (cfg.Console == nil || !cfg.Console.Disabled), !opts.withoutAI && (cfg.Console == nil || !cfg.Console.DisableAI)); err != nil {
				return err
			}
		}

	}
	if runCtx.Err() != nil {
		return nil
	}
	messageability.SetOutboundReady(true)
	slog.Info("we-clay gateway 已启动", "core", cfg.CoreAddress, "transport", "grpc")
	direct := newClient(runCtx)
	defer func() { cancel(); direct.state.close() }()
	hooks := eventsync.Hooks{WithoutPlugins: opts.withoutPlugins}
	if h := opts.handlers.OnReady; h != nil {
		hooks.Ready = func() error { return callHandler("OnReady", func() error { return h(runCtx, direct) }) }
	}
	if h := opts.handlers.OnMessage; h != nil {
		hooks.Message = func(m *message.Message) error {
			return callHandler("OnMessage", func() error { return h(runCtx, direct, m) })
		}
	}
	if h := opts.handlers.OnEvent; h != nil {
		hooks.Event = func(e *sdkplugin.Event) error {
			return callHandler("OnEvent", func() error { return h(runCtx, direct, e) })
		}
	}
	// 归档观察器先于可消费消息的业务回调执行；入库失败阻止当前批次确认。
	if bridge != nil {
		ready, receive := hooks.Ready, hooks.Message
		// 首次出现和后续变更均投影到控制台，不依赖已有联系人才能产生的变更事件。
		hooks.Contacts = bridge.SyncContacts
		hooks.Ready = func() error {
			if err := bridge.Online(); err != nil {
				return err
			}
			if ready != nil {
				if err := ready(); err != nil {
					return err
				}
			}
			if observer != nil && observer.ready != nil {
				observer.ready()
			}
			return nil
		}
		hooks.Message = func(m *message.Message) error {
			if err := bridge.Record(m); err != nil {
				return err
			}
			if receive != nil {
				return receive(m)
			}
			return nil
		}
		defer bridge.Ready.Store(false)
		// 必须先取消并等待读取任务，再落盘缓存、恢复全局 API 或释放账号资料。
		defer func() { cancel(); bridge.WaitContactSync() }()
	}
	watchDone := make(chan struct{})
	defer func() { cancel(); <-watchDone }()
	go func() {
		defer close(watchDone)
		if err := coreprocess.WatchOnline(runCtx, client, reportConnection); runCtx.Err() == nil {
			cancelCause(err)
		}
	}()
	return eventsync.RunEvents(runCtx, client, hooks)
}
