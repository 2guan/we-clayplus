// 文件名：ready.go
// 主要功能：等待 core 就绪；控制台模式在扫码暂停时持续运行，保留本地管理能力。
// 作者：2guan
package coreprocess

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ConnectionState 是 core 的登录状态投影，不包含账号凭据和二维码票据。
type ConnectionState struct {
	Online   bool   `json:"online"`
	Phase    string `json:"phase"`
	Status   string `json:"status"`
	CanRetry bool   `json:"can_retry"`
}

// WaitOnline 经 gRPC 读取登录状态；浏览器 HTTP 仅用于用户扫码和验证。
func WaitOnline(ctx context.Context, client *api.Client, c config.CoreConfig, keepConsole ...bool) error {
	keep := false
	if len(keepConsole) > 0 {
		keep = keepConsole[0]
	}
	return waitOnline(ctx, client, c, keep, nil)
}

func WaitOnlineNotify(ctx context.Context, client *api.Client, c config.CoreConfig, args ...any) error {
	keepConsole := false
	var report func(ConnectionState) error
	for _, a := range args {
		switch v := a.(type) {
		case bool:
			keepConsole = v
		case func(ConnectionState) error:
			report = v
		}
	}
	return waitOnline(ctx, client, c, keepConsole, report)
}

func waitOnline(ctx context.Context, client *api.Client, c config.CoreConfig, keepConsole bool, report func(ConnectionState) error) error {
	waitCtx, startupCancel := context.WithTimeout(ctx, c.StartupTimeout)
	defer startupCancel()
	ready := false
	pausedReported := false
	firstState := true
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if ready {
				return fmt.Errorf("等待 core 登录超时；请查看登录页面和本地 core.log")
			}
			return fmt.Errorf("等待 core gRPC 启动超时；请查看本地 core.log")
		case <-timer.C:
		}
		callCtx, stop := context.WithTimeout(waitCtx, 3*time.Second)
		var view ConnectionState
		err := client.Get("/api/login/view").WithContext(callCtx).DoJson(&view)
		stop()
		if err != nil {
			code := status.Code(err)
			if code != codes.Unavailable && code != codes.DeadlineExceeded && code != codes.Canceled {
				return fmt.Errorf("读取 core 登录状态失败: %w", err)
			}
		} else {
			if !ready {
				ready = true
				startupCancel()
				loginCtx, loginCancel := context.WithTimeout(ctx, c.LoginTimeout)
				defer loginCancel()
				waitCtx = loginCtx
				// 控制台独立于微信登录，等待扫码期间仅随宿主取消退出。
				if keepConsole {
					waitCtx = ctx
				}
				if c.LoginAddress != "-" {
					slog.Info("core 已启动，请完成 Mac 登录", "url", "http://"+c.LoginAddress)
				}
			}
			if report != nil {
				if err := report(view); err != nil {
					return err
				}
			}
			canRequestRetry := firstState && view.CanRetry
			firstState = false
			if canRequestRetry {
				// Run 由应用明确启动：请求 core 重新登录一次，不在 gateway 实现认证。
				callCtx, stop := context.WithTimeout(waitCtx, 3*time.Second)
				var result string
				err := client.Post("/api/login/retry").WithContext(callCtx).Body(map[string]any{}).DoJson(&result)
				stop()
				if err != nil {
					return fmt.Errorf("请求 core 重新登录失败: %w", err)
				}
				timer.Reset(500 * time.Millisecond)
				continue
			}
			if view.Online {
				return nil
			}
			if strings.HasPrefix(view.Status, "登录流程已暂停") && !keepConsole {
				return fmt.Errorf("core 登录流程已暂停；请查看登录页面和本地 core.log，修复后重试")
			}
			if keepConsole && strings.HasPrefix(view.Status, "登录流程已暂停") && !pausedReported {
				slog.Warn("微信登录已暂停，控制台继续运行，可配置模型与插件")
				pausedReported = true
			}
		}
		timer.Reset(500 * time.Millisecond)
	}
}

// ErrLoginRequired 表示 core 进程仍存活，但已登录的微信会话离线。
var ErrLoginRequired = errors.New("微信会话已离线，请打开登录页面重新扫码，完成后重新连接")

// WatchOnline 只通过本机 gRPC 查询状态，不发送微信心跳或尝试重新认证。
// 必须在 WaitOnline 成功后启动，启动期间等待扫码不算掉线。
func WatchOnline(ctx context.Context, client *api.Client, report func(ConnectionState) error) error {
	return watchOnline(ctx, client, 2*time.Second, report)
}
func watchOnline(ctx context.Context, client *api.Client, interval time.Duration, report func(ConnectionState) error) error {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		var view ConnectionState
		err := client.Get("/api/login/view").WithContext(callCtx).DoJson(&view)
		cancel()
		if err != nil {
			code := status.Code(err)
			if code == codes.Unavailable || code == codes.DeadlineExceeded {
				continue
			}
			return fmt.Errorf("检查 core 登录状态失败: %w", err)
		}
		if report != nil {
			if err := report(view); err != nil {
				return err
			}
		}
		if !view.Online {
			return ErrLoginRequired
		}
	}
}
