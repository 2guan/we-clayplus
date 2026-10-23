package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/config"
	consolebridge "github.com/we-clay/we-clay/gateway/internal/console"
	"github.com/we-clay/we-clay/gateway/internal/coreprocess"
)

// coreConsoleController owns only this installation's independent core. The
// Dashboard lives in the enclosing Run, while each connection gets fresh
// clients, caches, callbacks and event subscriptions.
type coreConsoleController struct {
	ctx    context.Context
	cfg    Config
	opts   runOptions
	bridge *consolebridge.Bridge

	mu         sync.Mutex
	state      string
	status     string
	closed     bool
	stopping   bool
	generation uint64
	cancel     context.CancelCauseFunc
	done       chan struct{}
	stopDone   chan struct{}

	client atomic.Pointer[api.Client]
	config atomic.Pointer[config.Config]
}

func newCoreConsoleController(ctx context.Context, cfg Config, opts runOptions) *coreConsoleController {
	return &coreConsoleController{ctx: ctx, cfg: cfg, opts: opts, state: "stopped", status: "core 尚未启动"}
}

func runCoreConsoleControl(c *coreConsoleController, bridge *consolebridge.Bridge) error {
	c.bridge = bridge
	bridge.SetCoreController(c.execute)
	defer bridge.SetCoreController(nil)
	if err := c.start(); err != nil {
		return err
	}
	<-c.ctx.Done()
	c.shutdown()
	return nil
}

func (c *coreConsoleController) snapshot() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]any{"managed": true, "state": c.state, "status": c.status}
}

func (c *coreConsoleController) setState(state, status string) {
	c.mu.Lock()
	if !c.closed && !c.stopping {
		c.state, c.status = state, status
	}
	c.mu.Unlock()
}

func (c *coreConsoleController) start() error {
	c.mu.Lock()
	if c.closed || c.ctx.Err() != nil {
		c.mu.Unlock()
		return errors.New("控制台正在退出")
	}
	if c.stopping || c.done != nil || (c.state != "stopped" && c.state != "error") {
		c.mu.Unlock()
		return errors.New("core 正在启动、运行或停止，请稍后重试")
	}
	sessionCtx, cancel := context.WithCancelCause(c.ctx)
	done := make(chan struct{})
	c.generation++
	generation := c.generation
	c.cancel, c.done = cancel, done
	c.state, c.status = "starting", "正在启动或连接 core…"
	c.mu.Unlock()
	go c.runSession(sessionCtx, cancel, done, generation)
	return nil
}

func (c *coreConsoleController) runSession(ctx context.Context, cancel context.CancelCauseFunc, done chan struct{}, generation uint64) {
	observer := &coreSessionObserver{
		clientReady: func(client *api.Client, cfg Config) {
			c.config.Store(&cfg)
			c.client.Store(client)
		},
		clientClosed: func() {
			c.client.Store(nil)
			c.config.Store(nil)
		},
		connection: func(view ConnectionState) {
			if !view.Online {
				c.bridge.Ready.Store(false)
				_ = c.bridge.Dashboard.SetConnectionStatus("offline")
				c.setState("waiting_login", view.Status)
			} else {
				c.setState("waiting_login", "微信已连接，正在准备消息订阅…")
			}
		},
		ready: func() { c.setState("online", "微信已连接") },
	}
	err := runCoreSession(ctx, cancel, c.cfg, c.opts, c.bridge, true, observer)
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		err = cause
	}
	cancel(nil)
	c.bridge.Ready.Store(false)
	_ = c.bridge.Dashboard.SetConnectionStatus("offline")
	c.mu.Lock()
	if c.generation == generation {
		c.cancel, c.done = nil, nil
		if !c.closed && !c.stopping {
			if err != nil && !errors.Is(err, context.Canceled) {
				if errors.Is(err, ErrLoginRequired) {
					c.state, c.status = "error", "微信会话已离线，请在本页重新连接"
				} else {
					c.state, c.status = "error", "core 启动或连接失败，请检查本机 core.log"
				}
				slog.Warn("core 会话已结束", "error_type", fmt.Sprintf("%T", err))
			} else {
				c.state, c.status = "stopped", "core 连接已结束"
			}
		}
	}
	c.mu.Unlock()
	close(done)
}

func (c *coreConsoleController) stop() error {
	c.mu.Lock()
	if c.closed || c.ctx.Err() != nil {
		c.mu.Unlock()
		return errors.New("控制台正在退出")
	}
	if c.stopping {
		c.mu.Unlock()
		return errors.New("core 正在停止，请稍后重试")
	}
	if c.state == "stopped" && c.done == nil {
		c.mu.Unlock()
		return nil
	}
	c.stopping = true
	c.state, c.status = "stopping", "正在停止 core；微信设备数据会保留…"
	cancel, done := c.cancel, c.done
	stopDone := make(chan struct{})
	c.stopDone = stopDone
	c.mu.Unlock()
	go func() {
		defer close(stopDone)
		if cancel != nil {
			cancel(context.Canceled)
		}
		if done != nil {
			<-done
		}
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := coreprocess.StopIndependent(stopCtx, c.cfg)
		stopCancel()
		c.mu.Lock()
		c.stopping = false
		c.stopDone = nil
		if !c.closed {
			if err != nil {
				c.state, c.status = "error", "停止 core 失败，请检查本机 core.log"
				slog.Warn("停止 core 失败", "error_type", fmt.Sprintf("%T", err))
			} else {
				c.state, c.status = "stopped", "core 已停止；微信设备数据已保留"
			}
		}
		c.mu.Unlock()
	}()
	return nil
}

func (c *coreConsoleController) loginView(ctx context.Context) (any, error) {
	c.mu.Lock()
	state, description := c.state, c.status
	c.mu.Unlock()
	if state != "waiting_login" && state != "online" {
		return map[string]any{"online": false, "phase": state, "status": description, "can_retry": state == "error"}, nil
	}
	client := c.client.Load()
	if client == nil {
		return map[string]any{"online": false, "phase": state, "status": description}, nil
	}
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var view json.RawMessage
	if err := client.Get("/api/login/view").WithContext(callCtx).DoJson(&view); err != nil {
		return nil, fmt.Errorf("暂时无法读取微信登录状态: %w", err)
	}
	return view, nil
}

func (c *coreConsoleController) retryLogin(ctx context.Context) error {
	c.mu.Lock()
	state := c.state
	c.mu.Unlock()
	if state == "error" || state == "stopped" {
		return c.start()
	}
	if state != "waiting_login" {
		return errors.New("当前登录流程不需要重新扫码")
	}
	client := c.client.Load()
	if client == nil {
		return errors.New("core 尚未连接，请稍后重试")
	}
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var view ConnectionState
	if err := client.Get("/api/login/view").WithContext(callCtx).DoJson(&view); err != nil {
		return errors.New("暂时无法核实 core 登录状态，请稍后重试")
	}
	if view.Online || (!view.CanRetry && !strings.HasPrefix(view.Status, "登录流程已暂停")) {
		return errors.New("仅在登录流程暂停时允许重新扫码")
	}
	var result string
	if err := client.Post("/api/login/retry").WithContext(callCtx).Body(map[string]any{}).DoJson(&result); err != nil {
		return fmt.Errorf("请求 core 重新登录失败: %w", err)
	}
	return nil
}

func (c *coreConsoleController) pendingVerification(ctx context.Context) (*api.Client, struct {
	Online      bool   `json:"online"`
	Phase       string `json:"phase"`
	VerifyURL   string `json:"verify_url"`
	PINRequired bool   `json:"pin_required"`
}, error) {
	var view struct {
		Online      bool   `json:"online"`
		Phase       string `json:"phase"`
		VerifyURL   string `json:"verify_url"`
		PINRequired bool   `json:"pin_required"`
	}
	c.mu.Lock()
	state := c.state
	c.mu.Unlock()
	if state != "waiting_login" {
		return nil, view, errors.New("当前没有待处理的登录验证")
	}
	client := c.client.Load()
	if client == nil {
		return nil, view, errors.New("core 尚未连接")
	}
	if err := client.Get("/api/login/view").WithContext(ctx).DoJson(&view); err != nil {
		return nil, view, errors.New("暂时无法核实登录验证状态")
	}
	if view.Online || view.Phase != "verification_required" {
		return nil, view, errors.New("当前没有待处理的登录验证")
	}
	return client, view, nil
}

func (c *coreConsoleController) verifyCode(ctx context.Context, params json.RawMessage) (any, error) {
	var request struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(params, &request) != nil || len(request.Code) == 0 || len(request.Code) > 32 || strings.ContainsAny(request.Code, "\r\n") {
		return nil, errors.New("验证码格式无效")
	}
	client, view, err := c.pendingVerification(ctx)
	if err != nil {
		return nil, err
	}
	if !view.PINRequired {
		return nil, errors.New("当前验证不需要输入验证码")
	}
	var result json.RawMessage
	if err := client.Post("/api/login/verification-code").WithContext(ctx).Body(map[string]string{"code": request.Code}).DoJson(&result); err != nil {
		return nil, errors.New("验证码提交失败，请查看本机 core.log")
	}
	return map[string]bool{"submitted": true}, nil
}

func (c *coreConsoleController) confirmDevice(ctx context.Context) (any, error) {
	client, view, err := c.pendingVerification(ctx)
	if err != nil {
		return nil, err
	}
	if view.VerifyURL == "" || view.PINRequired {
		return nil, errors.New("当前验证不支持设备确认")
	}
	// The URL is read from this core session, never accepted from the browser.
	// It may contain a login ticket and must not enter logs or responses.
	for _, path := range []string{"/api/login/device-confirm/get", "/api/login/device-confirm/ok"} {
		var result json.RawMessage
		if err := client.Post(path).WithContext(ctx).Body(map[string]string{"url": view.VerifyURL}).DoJson(&result); err != nil {
			return nil, errors.New("设备确认失败，请查看本机 core.log")
		}
	}
	return map[string]bool{"submitted": true}, nil
}

func (c *coreConsoleController) execute(ctx context.Context, operation string, params json.RawMessage) (any, error) {
	switch operation {
	case "core-status":
		return c.snapshot(), nil
	case "core-start":
		if err := c.start(); err != nil {
			return nil, err
		}
		return c.snapshot(), nil
	case "core-stop":
		if err := c.stop(); err != nil {
			return nil, err
		}
		return c.snapshot(), nil
	case "login-view":
		return c.loginView(ctx)
	case "login-retry":
		if err := c.retryLogin(ctx); err != nil {
			return nil, err
		}
		return map[string]bool{"restarting": true}, nil
	case "login-verify-code":
		return c.verifyCode(ctx, params)
	case "login-device-confirm":
		return c.confirmDevice(ctx)
	default:
		return nil, errors.New("不支持的 core 控制命令")
	}
}

func (c *coreConsoleController) shutdown() {
	c.mu.Lock()
	c.closed = true
	cancel, done, stopDone := c.cancel, c.done, c.stopDone
	c.mu.Unlock()
	if cancel != nil {
		cancel(context.Canceled)
	}
	if done != nil {
		<-done
	}
	if stopDone != nil {
		<-stopDone
	}
}
