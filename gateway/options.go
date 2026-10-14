package gateway

import (
	"context"
	"fmt"

	"github.com/we-clay/we-clay/sdk/message"
	"github.com/we-clay/we-clay/sdk/plugin"
)

// Handlers 在应用进程内处理就绪、消息及联系人/群资料变更。
// 回调串行执行，应及时返回并遵守 ctx 取消。返回错误或 panic 会停止 Run，
// 不自动重试回调；未确认的事件在下次启动时可能重放，业务需按消息 ID 去重。
type Handlers struct {
	// OnConnectionChanged 通知 core 登录阶段变化，与其他回调串行。
	// 此回调可能先于 OnReady；业务调用必须等到 OnReady。掉线后 Run 返回 ErrLoginRequired。
	OnConnectionChanged func(context.Context, ConnectionState) error
	// OnReady 在登录、缓存初始化和首次事件订阅成功后调用一次；重连不重复调用。
	OnReady func(context.Context, *Client) error
	// OnMessage 接收正常解析的消息，先于内置命令和插件执行。
	OnMessage func(context.Context, *Client, *message.Message) error
	// OnEvent 接收原有分发事件；消息可能同时触发 OnMessage 和 OnEvent。
	// 已被内置命令消费的消息不再触发 OnEvent。
	OnEvent func(context.Context, *Client, *plugin.Event) error
}

type runOptions struct {
	handlers         Handlers
	withoutPlugins   bool
	withoutDashboard bool
	withoutAI        bool
}

// Option 配置直接接入行为，不写入 TOML。
type Option func(*runOptions)

// WithHandlers 注册应用回调。重复设置时以后一次为准。
func WithHandlers(handlers Handlers) Option {
	return func(o *runOptions) { o.handlers = handlers }
}

// WithoutPlugins 关闭插件加载、目录监听和内置管理命令；直接回调仍正常运行。
// 不传此项时保留原来的插件行为，也可与直接回调共用。
func WithoutPlugins() Option {
	return func(o *runOptions) { o.withoutPlugins = true }
}

func invokeHandler(name string, fn func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("%s 回调发生 panic", name)
		}
	}()
	if err = fn(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// WithoutDashboard 仅关闭 Web 监听，保留 AI 基础插件。
func WithoutDashboard() Option { return func(o *runOptions) { o.withoutDashboard = true } }

// WithoutAI 关闭 AI 基础插件，控制台和扩展插件继续运行。
func WithoutAI() Option { return func(o *runOptions) { o.withoutAI = true } }
