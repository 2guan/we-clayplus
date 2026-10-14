package config

import (
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CoreConfig 开启由 Run 管理的 core；不设置 Config.Core 则连接外部 core。
// Version 必须固定为实际发布的 tag；不会自动升级到 latest。
type CoreConfig struct {
	AuthKeyEnv       string        `toml:"auth_key_env"` // 运行授权 Key 的环境变量名；默认 WE_CLAY_AUTH_KEY。
	OSS              *OSSConfig    `toml:"oss"`          // 非 nil 时从私有 OSS 下载；不回退 GitHub。
	Repository       string        `toml:"repository"`   // 默认 we-clay/core-release。
	Version          string        `toml:"version"`
	DeviceName       string        `toml:"device_name"`       // 可选：启动 core 时设置显示名称；留空沿用保存值。
	KeepAlive        bool          `toml:"keep_alive"`        // 独立运行：Run 退出保留 core，后续 Run 复用。
	CacheDir         string        `toml:"cache_dir"`         // 默认 ./runtime/core-bin，按仓库/版本/平台隔离。
	DataDir          string        `toml:"data_dir"`          // 必填绝对路径，保存账号状态及 core.log。
	LoginAddress     string        `toml:"login_address"`     // 默认 127.0.0.1:8086；"-" 关闭页面。
	Stage            string        `toml:"stage"`             // auto（默认）、login 或 resume；auto 检查会话文件是否存在。
	GitHubTokenEnv   string        `toml:"github_token_env"`  // 默认 WE_CLAY_GITHUB_TOKEN；公开仓库可不设置。
	StorageEncrypted bool          `toml:"storage_encrypted"` // 密码读取 WE_CLAY_STORAGE_PASSWORD。
	StartupTimeout   time.Duration `toml:"-"`                 // 等待 gRPC 就绪，默认 2 分钟。
	LoginTimeout     time.Duration `toml:"-"`                 // 等待扫码/恢复，默认 10 分钟。
}

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
var envPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (c CoreConfig) Resolve() (CoreConfig, error) {
	if c.KeepAlive && !filepath.IsAbs(c.DataDir) {
		return c, fmt.Errorf("keep_alive 模式必须显式填写固定的绝对 core.data_dir")
	}
	if c.AuthKeyEnv == "" {
		c.AuthKeyEnv = "WE_CLAY_AUTH_KEY"
	}
	if c.Repository == "" {
		c.Repository = "we-clay/core-release"
	}
	if c.CacheDir == "" {
		c.CacheDir = "./runtime/core-bin"
	}
	if c.DataDir == "" || !filepath.IsAbs(c.DataDir) {
		return c, fmt.Errorf("core.data_dir 必须显式填写固定的绝对路径；升级时请填写现有账号目录，避免创建新设备")
	}
	if c.LoginAddress == "" {
		c.LoginAddress = "127.0.0.1:8086"
	}
	if c.Stage == "" {
		c.Stage = "auto"
	}
	if c.GitHubTokenEnv == "" {
		c.GitHubTokenEnv = "WE_CLAY_GITHUB_TOKEN"
	}
	if c.StartupTimeout == 0 {
		c.StartupTimeout = 2 * time.Minute
	}
	if c.LoginTimeout == 0 {
		c.LoginTimeout = 10 * time.Minute
	}
	var err error
	if c.CacheDir, err = filepath.Abs(c.CacheDir); err != nil {
		return c, err
	}
	if c.DataDir, err = filepath.Abs(c.DataDir); err != nil {
		return c, err
	}
	if c.OSS != nil {
		resolved, e := c.OSS.Resolve()
		if e != nil {
			return c, e
		}
		c.OSS = &resolved
	}
	return c, c.Validate()
}

func (c CoreConfig) Validate() error {
	if !envPattern.MatchString(c.AuthKeyEnv) {
		return fmt.Errorf("core.auth_key_env 不是有效环境变量名")
	}
	switch strings.ToUpper(c.AuthKeyEnv) {
	case "OSS_ACCESS_KEY_ID", "OSS_ACCESS_KEY_SECRET", "OSS_SESSION_TOKEN", "SDK_SOURCE_TOKEN", "GH_TOKEN", "GITHUB_TOKEN", "WE_CLAY_GATEWAY_CORE_TOKEN", "WE_CLAY_CORE_TOKEN":
		return fmt.Errorf("core.auth_key_env 不能复用下载凭据或本机 API 令牌的环境变量")
	}
	if strings.EqualFold(c.AuthKeyEnv, c.GitHubTokenEnv) {
		return fmt.Errorf("授权 Key 与 GitHub 下载令牌必须使用不同环境变量")
	}
	if c.OSS != nil {
		if err := c.OSS.Validate(); err != nil {
			return err
		}
	}
	if !repositoryPattern.MatchString(c.Repository) {
		return fmt.Errorf("core.repository 必须为 owner/repo")
	}
	if !versionPattern.MatchString(c.Version) || strings.EqualFold(c.Version, "latest") {
		return fmt.Errorf("core.version 必须填写已发布的固定 Release tag，不能使用 latest")
	}
	if !envPattern.MatchString(c.GitHubTokenEnv) {
		return fmt.Errorf("core.github_token_env 不是有效环境变量名")
	}
	if c.Stage != "auto" && c.Stage != "login" && c.Stage != "resume" {
		return fmt.Errorf("core.stage 必须为 auto、login 或 resume")
	}
	if c.StartupTimeout <= 0 || c.LoginTimeout <= 0 {
		return fmt.Errorf("core 超时时间必须大于零")
	}
	if c.LoginAddress != "-" {
		host, port, err := net.SplitHostPort(c.LoginAddress)
		n, e := strconv.Atoi(port)
		if err != nil || e != nil || n < 1 || n > 65535 || !net.ParseIP(host).IsLoopback() {
			return fmt.Errorf("core.login_address 必须为回环 IP:端口，或 -")
		}
	}
	return nil
}
