package gateway

import (
	"context"
	"errors"

	"github.com/we-clay/we-clay/gateway/internal/coreprocess"
)

// PrepareCore 下载并校验指定版本的 core，或重新校验已有缓存。
// 此操作不启动 core，也不读写微信账号会话。
func PrepareCore(ctx context.Context, cfg Config) error {
	if ctx == nil {
		return errors.New("PrepareCore 需要非 nil context")
	}
	if cfg.Core == nil {
		return errors.New("PrepareCore 需要 core 配置")
	}
	resolved, err := cfg.Resolve()
	if err != nil {
		return err
	}
	_, err = coreprocess.Ensure(ctx, *resolved.Core)
	return err
}
