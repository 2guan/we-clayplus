// 文件名：managed.go
// 主要功能：管理本次 Run 拥有的 core 子进程，串行重启并区分主动停止与异常退出。
// 作者：2guan
package coreprocess

import (
	"context"
	"fmt"
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/config"
)

type Managed struct {
	mu      sync.Mutex
	ctx     context.Context
	binary  string
	cfg     config.Config
	process *Process
	closed  bool
	fail    context.CancelCauseFunc
}

func StartManaged(ctx context.Context, binary string, cfg config.Config, fail context.CancelCauseFunc) (*Managed, error) {
	m := &Managed{ctx: ctx, binary: binary, cfg: cfg, fail: fail}
	m.mu.Lock()
	err := m.startLocked(cfg)
	m.mu.Unlock()
	return m, err
}

func (m *Managed) startLocked(cfg config.Config) error {
	p, err := Start(m.ctx, m.binary, cfg)
	if err != nil {
		return err
	}
	m.process = p
	go func() {
		<-p.Done()
		m.mu.Lock()
		defer m.mu.Unlock()
		// 旧进程主动重启后不再是 current，不能取消整个 Web 后台。
		if !m.closed && m.process == p && m.ctx.Err() == nil {
			m.fail(p.ExitError())
		}
	}()
	return nil
}

// RestartLogin 仅重启受管子进程；调用者在锁内核实当前确为暂停状态。
func (m *Managed) RestartLogin(ctx context.Context, check func(context.Context) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return fmt.Errorf("core 管理器已停止")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.process != nil {
		if err := check(ctx); err != nil {
			return err
		}
		old := m.process
		m.process = nil
		old.Stop()
	}
	// 强制重新扫码，保留原数据目录及目录归属，绝不删除会话或消息文件。
	cfg := m.cfg
	core := *cfg.Core
	core.Stage = "login"
	cfg.Core = &core
	return m.startLocked(cfg)
}

func (m *Managed) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	if m.process != nil {
		m.process.Stop()
		m.process = nil
	}
}
