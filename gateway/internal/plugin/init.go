package plugin

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"

	sdk "github.com/we-clay/we-clay/sdk/plugin"
)

type runtimeState struct {
	ctx       context.Context
	cancel    context.CancelFunc
	external  bool
	deniedEnv string
	events    chan *sdk.Event
	done      chan struct{}
	workers   sync.WaitGroup
}

var active atomic.Pointer[runtimeState]
var operations sync.Mutex

// Initial 初始化本次运行；即使失败也必须调用 Destroy 释放已加载插件。
func Initial(ctx context.Context, dir string, external bool, deniedEnv string) error {
	ctx, cancel := context.WithCancel(ctx)
	state := &runtimeState{ctx: ctx, cancel: cancel, external: external, deniedEnv: deniedEnv, events: make(chan *sdk.Event, 100), done: make(chan struct{})}
	active.Store(state)
	pluginDir = dir
	configPath = filepath.Join(dir, "config.toml")
	configs = map[string]*Config{}
	go dispatcher(state)
	if external {
		if err := loadConfig(); err != nil {
			return err
		}
	}
	// HostService 是既有 RPC 服务名；组件改名不改变线上协议。
	sdk.HostServiceImpl = &gatewayService{}
	if err := registerBuiltinPM(); err != nil {
		return err
	}
	if err := registerBuiltinCM(); err != nil {
		return err
	}
	if external {
		if err := LoadPlugins(); err != nil {
			return err
		}
		return startConfigWatcher()
	}
	return nil
}

func Destroy() {
	state := active.Swap(nil)
	if state == nil {
		return
	}
	state.cancel()
	// 阻止新加载并等待已开始的管理操作，随后关闭插件 RPC，解除正在等待的调用。
	operations.Lock()
	for _, w := range pluginSnapshot() {
		sdk.Kill(w.Name)
		slog.Info("插件退出", "name", w.Name)
	}
	operations.Unlock()
	stopConfigWatcher()
	<-state.done
	state.workers.Wait()
	sessionMu.Lock()
	for _, s := range sessions {
		s.Timer.Stop()
	}
	sessions = make(map[string]*session)
	sessionMu.Unlock()
	mu.Lock()
	plugins = nil
	configs = map[string]*Config{}
	commandIndex = map[string]*wrapper{}
	capabilityIndex = map[string]*wrapper{}
	mu.Unlock()
	sdk.HostServiceImpl = nil
}
