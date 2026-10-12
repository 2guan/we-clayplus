package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config 配置 gateway。令牌只能通过代码或环境传入，不从 TOML 读取。
type Config struct {
	Core         *CoreConfig `toml:"core"`           // 非 nil 时由 Run 下载并管理 core。
	CoreAddress  string      `toml:"core_address"`   // 默认 127.0.0.1:8088，仅允许回环 IP。
	CoreToken    string      `toml:"-"`              // 优先于 CoreTokenEnv；不要写入版本库。
	CoreTokenEnv string      `toml:"core_token_env"` // 默认 WE_CLAY_CORE_TOKEN；"-" 禁用环境读取。
	DataDir      string      `toml:"data_dir"`       // 缓存目录，默认 ./runtime/gateway/data。
	PluginsDir   string      `toml:"plugins_dir"`    // 插件及 config.toml 目录，默认 ./runtime/gateway/plugins。
	Owner        string      `toml:"owner"`          // 管理员微信 ID。
	Forbidden    string      `toml:"forbidden"`      // 无权限操作的回复文案。
}

// Resolve 补齐默认值，固定目录绝对路径，不改变进程工作目录。
func (c Config) Resolve() (Config, error) {
	if c.CoreAddress == "" {
		c.CoreAddress = "127.0.0.1:8088"
	}
	if c.CoreTokenEnv == "" {
		c.CoreTokenEnv = "WE_CLAY_CORE_TOKEN"
	}
	if c.CoreToken == "" && c.CoreTokenEnv != "-" {
		c.CoreToken = os.Getenv(c.CoreTokenEnv)
	}
	if c.Forbidden == "" {
		c.Forbidden = "没有权限执行此操作"
	}
	if c.DataDir == "" {
		c.DataDir = "./runtime/gateway/data"
	}
	if c.PluginsDir == "" {
		c.PluginsDir = "./runtime/gateway/plugins"
	}
	var err error
	if c.DataDir, err = filepath.Abs(c.DataDir); err != nil {
		return c, err
	}
	if c.PluginsDir, err = filepath.Abs(c.PluginsDir); err != nil {
		return c, err
	}
	if c.Core != nil {
		managed, err := c.Core.Resolve()
		if err != nil {
			return c, err
		}
		c.Core = &managed
	}
	return c, c.Validate()
}

func Load(path string) (Config, error) {
	var cfg Config
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err = toml.NewDecoder(strings.NewReader(string(raw))).DisallowUnknownFields().Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("gateway 配置无效: %w", err)
	}
	return cfg.Resolve()
}

func (c *Config) Validate() error {
	host, port, err := net.SplitHostPort(c.CoreAddress)
	number, e := strconv.Atoi(port)
	if err != nil || e != nil || number < 1 || number > 65535 || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("core_address 必须为本机 IP:端口；远程访问请通过加密隧道映射到本机")
	}
	if c.Core != nil {
		if err := c.Core.Validate(); err != nil {
			return err
		}
		if c.Core.LoginAddress == c.CoreAddress {
			return fmt.Errorf("core 登录页面与 gRPC 不能使用同一地址")
		}
	}
	return nil
}

// Get 由 gateway.Run 注入；导入包不会读取配置或访问网络。
var Get = func() *Config { return &Config{Forbidden: "没有权限执行此操作"} }
