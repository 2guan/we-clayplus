package gateway

import (
	"context"
	"errors"
	"github.com/we-clay/we-clay/gateway/internal/coreprocess"
)

// StopCore 显式停止由 KeepAlive 管理的独立 core，不调用微信退出登录接口。
// 停止前核对本地进程记录和 gRPC 鉴权；不会结束来源不明的端口进程。
// 正常结束 Run 无需调用此函数。升级 core 或停机维护时才调用。
func StopCore(ctx context.Context, cfg Config) error {
	if ctx == nil {
		return errors.New("StopCore 需要非 nil context")
	}
	if cfg.Core == nil || !cfg.Core.KeepAlive {
		return errors.New("StopCore 仅用于 KeepAlive core")
	}
	resolved, err := cfg.Resolve()
	if err != nil {
		return err
	}
	return coreprocess.StopIndependent(ctx, resolved)
}
