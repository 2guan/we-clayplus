// Package gateway 提供可嵌入应用的插件运行入口，通过 gRPC 连接独立 core。
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
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/ability"
	chatroomability "github.com/we-clay/we-clay/gateway/internal/ability/chatroom"
	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	messageability "github.com/we-clay/we-clay/gateway/internal/ability/message"
	eventsync "github.com/we-clay/we-clay/gateway/internal/ability/sync"
	"github.com/we-clay/we-clay/gateway/internal/api"
	userapi "github.com/we-clay/we-clay/gateway/internal/api/user"
	"github.com/we-clay/we-clay/gateway/internal/config"
	"github.com/we-clay/we-clay/gateway/internal/coreprocess"
	"github.com/we-clay/we-clay/gateway/internal/plugin"
	sdkcontact "github.com/we-clay/we-clay/sdk/contact"
)

// Config 是 gateway 的运行配置。零值使用本机 core 及 runtime/gateway 目录。
type Config = config.Config

// CoreConfig 指定下载版本和本地目录；设置 Config.Core 后由 Run 管理 core 生命周期。
type CoreConfig = config.CoreConfig

// ErrAlreadyRunning 表示本进程已有一个 Run 正在启动、运行或退出。
var ErrAlreadyRunning = errors.New("本进程已有 gateway 正在运行")
var runMu sync.Mutex

// LoadConfig 可选地从 TOML 文件读取配置；直接传 Config 无需配置文件。
// 相对目录按调用时的进程工作目录解析。
func LoadConfig(path string) (Config, error) { return config.Load(path) }

// Run 阻塞运行，直到 ctx 取消或出现不可恢复的错误。正常取消返回 nil。
// Config.Core 非 nil 时下载、校验并启动 core，等待登录；退出时回收该子进程。
// Config.Core 为 nil 时连接已启动且已登录的 core，不管理其进程。
// 两种模式均不调用微信退出登录接口。
// 每个进程同时允许一个 Run；退出清理完成后可以再次调用。
func Run(ctx context.Context, cfg Config) (err error) {
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
	if cfg.Core != nil {
		binary, downloadErr := coreprocess.Ensure(runCtx, *cfg.Core)
		if downloadErr != nil {
			return downloadErr
		}
		process, startErr := coreprocess.Start(runCtx, binary, cfg)
		if startErr != nil {
			return startErr
		}
		defer func() { cancel(); process.Stop() }()
		go func() {
			select {
			case <-process.Done():
				if runCtx.Err() == nil {
					cancelCause(process.ExitError())
				}
			case <-runCtx.Done():
			}
		}()
	}
	oldConfig := config.Get
	config.Get = func() *config.Config { return &cfg }
	defer func() { config.Get = oldConfig }()
	client, err := api.NewConfiguredClient(runCtx, &cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	oldClient := api.GetCore
	api.GetCore = func() *api.Client { return client }
	defer func() { api.GetCore = oldClient }()

	messageability.SetOutboundReady(false)
	defer messageability.SetOutboundReady(false)
	if cfg.Core != nil {
		if err = coreprocess.WaitOnline(runCtx, client, *cfg.Core); err != nil {
			cancel()
			return err
		}
	}
	profile, err := userapi.Get().GetProfile()
	if err != nil {
		return fmt.Errorf("请先启动 core 并完成 Mac 登录: %w", err)
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
	contactability.Initial(cacheDir)
	chatroomability.Initial(cacheDir)
	defer ability.Destroy()
	// 启动失败也回收已经启动的插件；停止回调先于缓存落盘及连接释放。
	defer func() { messageability.SetOutboundReady(false); cancel(); plugin.Destroy() }()
	if err = plugin.Initial(runCtx, cfg.PluginsDir); err != nil {
		return fmt.Errorf("插件初始化失败: %w", err)
	}
	if runCtx.Err() != nil {
		return nil
	}
	messageability.SetOutboundReady(true)
	slog.Info("we-clay gateway 已启动", "core", cfg.CoreAddress, "transport", "grpc")
	return eventsync.RunEvents(runCtx, client)
}
