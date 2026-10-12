package sync

import (
	"github.com/we-clay/we-clay/gateway/internal/plugin"
	"github.com/we-clay/we-clay/sdk/message"
	sdkplugin "github.com/we-clay/we-clay/sdk/plugin"
	"google.golang.org/protobuf/proto"
)

// Hooks 只在本次事件处理器内持有，不改变 SDK 或线上协议。
type Hooks struct {
	Ready          func() error
	Message        func(*message.Message) error
	Event          func(*sdkplugin.Event) error
	WithoutPlugins bool
}

type processor struct{ hooks Hooks }
type callbackError struct{ err error }

func (e *callbackError) Error() string { return e.err.Error() }
func (e *callbackError) Unwrap() error { return e.err }

func (p *processor) publish(event *sdkplugin.Event) error {
	if p.hooks.Event != nil {
		if err := p.hooks.Event(proto.Clone(event).(*sdkplugin.Event)); err != nil {
			return &callbackError{err}
		}
	}
	if !p.hooks.WithoutPlugins {
		plugin.Publish(event)
	}
	return nil
}
