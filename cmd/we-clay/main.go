// Command we-clay runs the embedded gateway, dashboard and AI from a single binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/we-clay/we-clay/gateway"
	"github.com/we-clay/we-clay/sdk/control"
)

// version is set by the release workflow. It is informational and never selects
// the independently versioned core binary.
var version = "dev"

const defaultCoreVersion = "v0.4.6"

type arguments struct {
	dataDir          string
	coreDataDir      string
	licenseFile      string
	coreVersion      string
	coreAddress      string
	loginAddress     string
	dashboardAddress string
	deviceName       string
	externalPlugins  bool
	check            bool
	stopCore         bool
	prepareCore      bool
	showVersion      bool
}

func main() {
	args, err := parseArgs(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}
	if args.showVersion {
		fmt.Println("we-clay", version)
		return
	}
	if args.stopCore {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := stopCore(ctx, args); err != nil {
			log.Fatal(err)
		}
		fmt.Println("独立 core 停止检查已完成；未退出微信登录")
		return
	}
	if args.prepareCore {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := prepareCore(ctx, args); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("core %s 已下载并校验（或复用已校验缓存）\n", args.coreVersion)
		return
	}
	cfg, key, err := prepare(args)
	if err != nil {
		log.Fatal(err)
	}
	if args.check {
		fmt.Printf("本地配置格式有效（未联系授权服务）：we-clay %s；core %s；账号目录 %s；控制台 http://%s\n", version, cfg.Core.Version, cfg.Core.DataDir, cfg.Console.Address)
		return
	}
	// The key never enters argv, TOML or logs. The gateway forwards it only to
	// core; external plugin processes are disabled unless explicitly requested.
	if err := os.Setenv("WE_CLAY_AUTH_KEY", key); err != nil {
		log.Fatal(err)
	}
	key = ""
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	options := []gateway.Option{gateway.WithoutExternalPlugins(), gateway.WithCoreConsoleControl()}
	if args.externalPlugins {
		options = []gateway.Option{gateway.WithCoreConsoleControl()}
	}
	log.Printf("启动 we-clay %s；控制台 http://%s（从“微信连接”扫码并管理 core）", version, cfg.Console.Address)
	if err := gateway.Run(ctx, cfg, options...); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func parseArgs(argv []string) (arguments, error) {
	var a arguments
	fs := flag.NewFlagSet("we-clay", flag.ContinueOnError)
	fs.StringVar(&a.dataDir, "data-dir", defaultDataDir(), "本机运行目录；升级时沿用旧目录")
	fs.StringVar(&a.coreDataDir, "core-data-dir", "", "已有微信账号目录的绝对路径；留空使用 <data-dir>/core")
	fs.StringVar(&a.licenseFile, "license-file", "", "授权 Key 文件；默认 <data-dir>/secrets/license.key")
	fs.StringVar(&a.coreVersion, "core-version", defaultCoreVersion, "已发布的固定 core 版本")
	fs.StringVar(&a.coreAddress, "core-address", "127.0.0.1:8088", "本机 core gRPC 监听地址")
	fs.StringVar(&a.loginAddress, "login-address", "127.0.0.1:8201", "本机微信登录页面地址")
	fs.StringVar(&a.dashboardAddress, "dashboard-address", "127.0.0.1:8208", "本机控制台地址")
	fs.StringVar(&a.deviceName, "device-name", "", "微信显示的设备名称；留空沿用已保存名称")
	fs.BoolVar(&a.externalPlugins, "external-plugins", false, "显式启用可信外部插件进程")
	fs.BoolVar(&a.check, "check", false, "只检查授权文件和本机配置，不连接微信")
	fs.BoolVar(&a.stopCore, "stop-core", false, "停止当前配置管理的独立 core，不退出微信登录；无需授权 Key 文件")
	fs.BoolVar(&a.prepareCore, "prepare-core", false, "下载并校验指定 core 版本，不启动 core 或访问账号数据")
	fs.BoolVar(&a.showVersion, "version", false, "显示 we-clay 版本")
	if err := fs.Parse(argv); err != nil {
		return a, err
	}
	if fs.NArg() != 0 {
		return a, fmt.Errorf("不支持额外参数：%s", fs.Arg(0))
	}
	if a.stopCore && (a.check || a.showVersion || a.prepareCore) {
		return a, errors.New("--stop-core 不能与 --check、--prepare-core 或 --version 同时使用")
	}
	if a.prepareCore && (a.check || a.showVersion) {
		return a, errors.New("--prepare-core 不能与 --check 或 --version 同时使用")
	}
	return a, nil
}

func prepare(a arguments) (gateway.Config, string, error) {
	cfg, err := prepareConfig(a)
	if err != nil {
		return gateway.Config{}, "", err
	}
	licenseFile := a.licenseFile
	if licenseFile == "" {
		licenseFile = filepath.Join(filepath.Clean(a.dataDir), "secrets", "license.key")
	}
	key, err := readLicense(licenseFile)
	if err != nil {
		return gateway.Config{}, "", err
	}
	return cfg, key, nil
}

func stopCore(ctx context.Context, a arguments) error {
	cfg, err := prepareConfig(a)
	if err != nil {
		return err
	}
	// A misspelt installation directory must not create a new core directory
	// merely by acquiring the StopCore process lock.
	info, err := os.Stat(cfg.Core.DataDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("微信账号目录不存在：%s", cfg.Core.DataDir)
	}
	return gateway.StopCore(ctx, cfg)
}

func prepareCore(ctx context.Context, a arguments) error {
	cfg, key, err := prepare(a)
	if err != nil {
		return err
	}
	previous, existed := os.LookupEnv("WE_CLAY_AUTH_KEY")
	if err := os.Setenv("WE_CLAY_AUTH_KEY", key); err != nil {
		return err
	}
	key = ""
	defer func() {
		if existed {
			_ = os.Setenv("WE_CLAY_AUTH_KEY", previous)
		} else {
			_ = os.Unsetenv("WE_CLAY_AUTH_KEY")
		}
	}()
	return gateway.PrepareCore(ctx, cfg)
}

func prepareConfig(a arguments) (gateway.Config, error) {
	var cfg gateway.Config
	if !filepath.IsAbs(a.dataDir) {
		return cfg, errors.New("--data-dir 必须是绝对路径")
	}
	base := filepath.Clean(a.dataDir)
	coreDataDir := a.coreDataDir
	if coreDataDir == "" {
		coreDataDir = filepath.Join(base, "core")
	} else {
		if !filepath.IsAbs(coreDataDir) {
			return cfg, errors.New("--core-data-dir 必须是绝对路径")
		}
		// A misspelt migration path must not silently create another device.
		info, err := os.Stat(coreDataDir)
		if err != nil || !info.IsDir() {
			return cfg, fmt.Errorf("指定的旧微信账号目录不存在：%s", coreDataDir)
		}
	}
	cfg = gateway.Config{
		CoreAddress: a.coreAddress,
		DataDir:     filepath.Join(base, "gateway", "data"),
		PluginsDir:  filepath.Join(base, "gateway", "plugins"),
		Core: &gateway.CoreConfig{
			Version:      a.coreVersion,
			OSS:          &gateway.OSSConfig{Bucket: "weclay", Region: "cn-beijing", Prefix: "core"},
			KeepAlive:    true,
			CacheDir:     filepath.Join(base, "core-bin"),
			DataDir:      coreDataDir,
			LoginAddress: a.loginAddress,
			DeviceName:   a.deviceName,
		},
		Console: &control.Config{Address: a.dashboardAddress, DataDir: filepath.Join(base, "console")},
	}
	cfg, err := cfg.Resolve()
	if err != nil {
		return gateway.Config{}, err
	}
	return cfg, nil
}

func readLicense(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("读取授权 Key 文件失败（%s）：%w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return "", errors.New("授权 Key 文件必须是长度不超过 4096 字节的普通文件")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return "", errors.New("授权 Key 文件权限过宽，请设置为 0600")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取授权 Key 文件失败：%w", err)
	}
	key := strings.TrimSpace(string(raw))
	if strings.ContainsAny(key, "\r\n") || !validAuthKey(key) {
		return "", errors.New("授权 Key 文件格式无效；应为 wca_ 后接 64 位小写十六进制字符")
	}
	return key, nil
}

func validAuthKey(key string) bool {
	if len(key) != 68 || !strings.HasPrefix(key, "wca_") {
		return false
	}
	for _, ch := range key[4:] {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "we-clay")
	case "windows":
		if base := os.Getenv("LOCALAPPDATA"); filepath.IsAbs(base) {
			return filepath.Join(base, "we-clay")
		}
		return filepath.Join(home, "AppData", "Local", "we-clay")
	default:
		if base := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(base) {
			return filepath.Join(base, "we-clay")
		}
		return filepath.Join(home, ".local", "share", "we-clay")
	}
}
