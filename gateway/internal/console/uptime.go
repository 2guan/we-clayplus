// 主要功能：提供独立 core 的只读运行时长，dashboard 重启不重置计时。
package console

import (
	"context"
	"fmt"
	"time"

	"github.com/we-clay/we-clay/gateway/internal/coreprocess"
)

func (b *Bridge) SetCoreAddress(address string) {
	b.uptimeMu.Lock()
	defer b.uptimeMu.Unlock()
	if b.coreAddress != address {
		b.coreAddress, b.uptimeChecked = address, time.Time{}
	}
}

func (b *Bridge) CoreUptime(ctx context.Context) (time.Duration, error) {
	b.uptimeMu.Lock()
	defer b.uptimeMu.Unlock()
	address := b.coreAddress
	if b.uptimeAddress != address || time.Since(b.uptimeChecked) >= 5*time.Second {
		b.coreStarted, b.uptimeError = coreprocess.CoreStartedAt(ctx, address)
		b.uptimeAddress, b.uptimeChecked = address, time.Now()
	}
	if b.uptimeError != nil {
		return 0, b.uptimeError
	}
	if b.coreStarted.IsZero() || time.Now().Before(b.coreStarted) {
		return 0, fmt.Errorf("core 启动时间不可用")
	}
	return time.Since(b.coreStarted), nil
}
