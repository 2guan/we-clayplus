package plugin

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/phsym/console-slog"
)

var clients = make(map[string]*plugin.Client)
var clientsMu sync.Mutex

// 插件握手配置
var handshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "we-clay",
	MagicCookieValue: "we-clay",
}

func Start(p Plugin) {
	clientLogger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Debug,
		JSONFormat: true,
	})

	slog.SetDefault(slog.New(Adapt(clientLogger)))
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig:  handshakeConfig,
		TLSProvider:      nil,
		Plugins:          map[string]plugin.Plugin{"plugin": &wrapper{impl: p}},
		VersionedPlugins: nil,
		GRPCServer:       plugin.DefaultGRPCServer,
		Logger:           clientLogger,
		Test:             nil,
	})
}

func Get(path string) (*Metadata, *Plugin, error) {
	return GetContext(context.Background(), path)
}

// GetContext 加载插件；ctx 取消时终止子进程，使阻塞中的初始化及 RPC 调用能够退出。
func GetContext(ctx context.Context, path string) (*Metadata, *Plugin, error) {
	return GetContextWithDeniedEnv(ctx, path, "WE_CLAY_AUTH_KEY")
}

// GetContextWithDeniedEnv additionally removes the authorization environment
// name selected by the embedding application from a plugin child process.
func GetContextWithDeniedEnv(ctx context.Context, path, deniedEnv string) (*Metadata, *Plugin, error) {
	cmd := exec.CommandContext(ctx, path)
	// Preserve the environment used by existing plugins, including their own
	// API credentials. Filter only credentials owned by the gateway/core.
	// Plugins run as the same OS user; this is not a sandbox.
	cmd.Env = pluginProcessEnv(os.Environ(), deniedEnv)
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  handshakeConfig,
		Plugins:          plugin.PluginSet{"plugin": &wrapper{}},
		VersionedPlugins: map[int]plugin.PluginSet{},
		Cmd:              cmd,
		SkipHostEnv:      true,
		//Reattach:         &plugin.ReattachConfig{},
		//RunnerFunc: func(l hclog.Logger, cmd *exec.Cmd, tmpDir string) (runner.Runner, error) {
		//	panic("TODO")
		//},
		//SecureConfig:        &plugin.SecureConfig{},
		//TLSConfig:           &tls.Config{},
		//Managed:             false,
		//MinPort:             0,
		//MaxPort:             0,
		//StartTimeout:        0,
		//Stderr:              nil,
		//SyncStdout:          nil,
		//SyncStderr:          nil,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		Logger:           creatManagerLogger(),
		//PluginLogBufferSize: 0,
		//AutoMTLS:            false,
		//GRPCDialOptions: []grpc.DialOption{grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(50*1024*1024), grpc.MaxCallSendMsgSize(50*1024*1024))},
		//GRPCBrokerMultiplex: false,
		//SkipHostEnv:         false,
		//UnixSocketConfig:    &plugin.UnixSocketConfig{},
	})

	// 失败时也释放子进程，不能让一次启动失败留下孤立插件。
	success := false
	defer func() {
		if !success {
			client.Kill()
		}
	}()
	// 连接插件
	rpc, err := client.Client()
	if err != nil {
		return nil, nil, err
	}

	// 获取插件实例
	raw, err := rpc.Dispense("plugin")
	if err != nil {
		return nil, nil, err
	}

	// 类型断言获取插件接口
	p, ok := raw.(Plugin)
	if !ok {
		return nil, nil, errors.New("plugin does not implement Plugin interface")
	}

	metadata := p.GetMetadata()
	if metadata == nil {
		return nil, nil, errors.New("获取插件信息失败，元数据为nil")
	}

	clientsMu.Lock()
	if _, exists := clients[metadata.Name]; exists {
		clientsMu.Unlock()
		return nil, nil, errors.New("插件名称已加载")
	}
	clients[metadata.Name] = client
	clientsMu.Unlock()
	success = true

	return metadata, &p, nil
}

func pluginProcessEnv(input []string, deniedEnv string) []string {
	deniedEnv = strings.ToUpper(deniedEnv)
	out := make([]string, 0, len(input))
	for _, item := range input {
		key, _, ok := strings.Cut(item, "=")
		upper := strings.ToUpper(key)
		if !ok || upper == deniedEnv || sensitiveEnvironmentName(upper) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func sensitiveEnvironmentName(upper string) bool {
	switch upper {
	case "WE_CLAY_AUTH_KEY", "WE_CLAY_GITHUB_TOKEN",
		"WE_CLAY_CORE_TOKEN", "WE_CLAY_GATEWAY_CORE_TOKEN",
		"WE_CLAY_STORAGE_PASSWORD", "SDK_SOURCE_TOKEN",
		"OSS_ACCESS_KEY_ID", "OSS_ACCESS_KEY_SECRET", "OSS_SESSION_TOKEN":
		return true
	}
	return false
}

func Kill(name string) {
	clientsMu.Lock()
	client := clients[name]
	delete(clients, name)
	clientsMu.Unlock()
	if client != nil {
		client.Kill()
	}
	slog.Debug("插件退出", "name", name)
}

func creatManagerLogger() hclog.Logger {
	levelVar := slog.LevelVar{}
	//levelVar.Set(slog.Level(4))
	levelVar.Set(slog.LevelDebug)
	logger := slog.New(console.NewHandler(os.Stderr, &console.HandlerOptions{Level: slog.LevelDebug}))
	//slog.SetDefault(logger)
	return newLogger(logger, &levelVar)
}
