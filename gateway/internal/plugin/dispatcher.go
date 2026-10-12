package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/duke-git/lancet/v2/strutil"
	"github.com/we-clay/we-clay/gateway/internal/config"
	"github.com/we-clay/we-clay/sdk/plugin"
)

var (
	eventPluginTimeout = time.Minute
)

type eventDispatchResult struct {
	handled    bool
	err        error
	panicValue any
	timedOut   bool
}

func Publish(e *plugin.Event) { publish(active.Load(), e) }

func publish(state *runtimeState, e *plugin.Event) {
	if state == nil || e == nil || state.ctx.Err() != nil {
		return
	}
	select {
	case <-state.ctx.Done():
	case state.events <- e:
	}
}

func dispatcher(state *runtimeState) {
	defer close(state.done)
	for {
		select {
		case <-state.ctx.Done():
			return
		case e := <-state.events:
			state.workers.Add(1)
			go func() {
				defer state.workers.Done()
				dispatchEventContext(state.ctx, e, pluginSnapshot(), &state.workers)
			}()
		}
	}
}

func dispatchEvent(e *plugin.Event, plugins []*wrapper) {
	dispatchEventContext(context.Background(), e, plugins, nil)
}

func dispatchEventContext(ctx context.Context, e *plugin.Event, plugins []*wrapper, workers *sync.WaitGroup) {
	for _, p := range plugins {
		if ctx.Err() != nil {
			return
		}
		if !shouldDispatchEvent(e, p) {
			continue
		}

		result := callEventPlugin(ctx, p, e, workers)
		slog.Debug("插件执行完成", "plugin", p.Name, "result", result)

		switch {
		case result.timedOut:
			slog.Error("插件处理事件超时", "plugin", p.Name, "timeout", eventPluginTimeout, "topic", e.Topic)
			continue
		case result.panicValue != nil:
			slog.Error("插件处理事件时发生崩溃", "plugin", p.Name, "error", result.panicValue)
			continue
		case result.err != nil:
			slog.Error("插件处理事件失败", "plugin", p.Name, "res", result.handled, "err", result.err)
			continue
		case result.handled:
			// 事件处理成功后刷新会话时间。
			if e.Sender != "" && isSessionActive(e.Sender) && p.Name == getSessionPlugin(e.Sender) {
				refreshSession(e.Sender)
			}
			if !p.Metadata.Next {
				slog.Debug("插件终止事件处理链", "plugin", p.Name, "topic", e.Topic)
				return
			}
		}
	}
}

func shouldDispatchEvent(e *plugin.Event, p *wrapper) bool {
	if p == nil || p.eventPlugin == nil {
		return false
	}
	if p.Config != nil && !p.Config.Enable {
		return false
	}
	if !strutil.HasPrefixAny(e.Topic, p.subscriptions) {
		return false
	}
	if e.Sender != "" && config.Get().Owner == e.Sender {
		return true
	}
	if e.Sender != "" && !isAllowed(e.Sender, p) {
		return false
	}
	return e.Sender == "" || isSessionAllowed(e.Sender, p.Metadata)
}

func callEventPlugin(ctx context.Context, p *wrapper, e *plugin.Event, workers *sync.WaitGroup) eventDispatchResult {
	resultCh := make(chan eventDispatchResult, 1)
	if workers != nil {
		workers.Add(1)
	}
	go func() {
		if workers != nil {
			defer workers.Done()
		}
		result := eventDispatchResult{}
		defer func() {
			if r := recover(); r != nil {
				result.panicValue = r
			}
			resultCh <- result
		}()

		result.handled, result.err = (*p.eventPlugin).OnEvent(e)
	}()

	timer := time.NewTimer(eventPluginTimeout)
	defer timer.Stop()

	select {
	case result := <-resultCh:
		return result
	case <-ctx.Done():
		return eventDispatchResult{err: ctx.Err()}
	case <-timer.C:
		return eventDispatchResult{timedOut: true}
	}
}

// DispatchCommand 分发命令给插件
func DispatchCommand(cmd *plugin.Command, plugins []*wrapper) {
	for _, p := range plugins {
		if p.Config != nil && !p.Config.Enable {
			continue
		}

		if !slices.Contains(p.commands, cmd.Main) {
			continue
		}

		sender := ""
		if cmd.Sender != nil {
			sender = cmd.Sender.GetUsername()
		}
		if sender != "" && !isAllowed(sender, p) {
			continue
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("插件处理命令时发生崩溃", "plugin", p.Name, "error", r)
				}
			}()

			if _, err := (*p.commandPlugin).OnCommand(cmd); err != nil {
				errMsg := fmt.Sprintf("插件[%s]处理命令失败: %v", p.Name, err)
				slog.Error(errMsg)
			}
		}()
	}
}
