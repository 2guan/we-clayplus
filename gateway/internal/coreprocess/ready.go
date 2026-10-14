// 文件名：ready.go
// 主要功能：等待 core 就绪；控制台模式在扫码暂停时持续运行，保留本地管理能力。
// 作者：2guan
package coreprocess

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// WaitOnline 经 gRPC 读取登录状态；浏览器 HTTP 仅用于用户扫码和验证。
func WaitOnline(ctx context.Context, client *api.Client, c config.CoreConfig, keepConsole bool) error {
	waitCtx, startupCancel := context.WithTimeout(ctx, c.StartupTimeout)
	defer startupCancel()
	ready := false
	pausedReported := false
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
		var view struct {
			Online bool   `json:"online"`
			Status string `json:"status"`
		}
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
