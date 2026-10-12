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
func WaitOnline(ctx context.Context, client *api.Client, c config.CoreConfig) error {
	waitCtx, startupCancel := context.WithTimeout(ctx, c.StartupTimeout)
	defer startupCancel()
	ready := false
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
				if c.LoginAddress != "-" {
					slog.Info("core 已启动，请完成 Mac 登录", "url", "http://"+c.LoginAddress)
				}
			}
			if view.Online {
				return nil
			}
			if strings.HasPrefix(view.Status, "登录流程已暂停") {
				return fmt.Errorf("core 登录流程已暂停；请查看登录页面和本地 core.log，修复后重试")
			}
		}
		timer.Reset(500 * time.Millisecond)
	}
}
