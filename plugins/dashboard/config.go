// 文件名: plugins/dashboard/config.go
// 主要功能: Dashboard Web 控制台插件的基础配置模型与默认值定义
// 作者: 2guan

package main

import (
	"path/filepath"
)


// Config 定义 Dashboard 控制台的核心运行配置
// 包含 HTTP 监听参数、数据持久化目录、安全密钥与站点品牌化信息
type Config struct {
	// Host Web 服务器绑定的本地监听地址，默认 "0.0.0.0" 允许局域网访问，"127.0.0.1" 仅限本机访问
	Host string `toml:"host" comment:"Web 服务监听绑定的 IP 地址"`

	// Port Web 服务器监听的端口号，默认 8899
	Port int `toml:"port" comment:"Web 服务监听端口号"`

	// DataDir 控制台专属 SQLite 数据库及媒体缓存存储目录，默认 "runtime/gateway/data"
	DataDir string `toml:"data_dir" comment:"控制台数据存储目录"`

	// JWTSecret 用于签发和校验管理员/操作员 Token 的 HMAC-SHA256 密钥
	JWTSecret string `toml:"jwt_secret" comment:"JWT 签名安全密钥，首次生成随机字符串"`

	// SiteTitle 网页标题与左上角系统品牌名称，默认 "we-clay 机器人控制台"
	SiteTitle string `toml:"site_title" comment:"控制台系统名称"`

	// FooterText 页面底部展示的版权声明与企业/团队备注，默认 "© 2026 we-clayplus · 微信协议自动化运行系统"
	FooterText string `toml:"footer_text" comment:"页脚版权与备注信息"`

	// EnableCORS 是否开启跨域资源共享（默认开启以支持前后端分离开发与第三方内嵌）
	EnableCORS bool `toml:"enable_cors" comment:"是否开启跨域资源共享"`
}

// DefaultConfig 返回插件默认的初始配置实例
func DefaultConfig() Config {
	return Config{
		Host:       "0.0.0.0",
		Port:       8899,
		DataDir:    filepath.Join("runtime", "gateway", "data"),
		JWTSecret:  "we-clay-dashboard-secret-token-key-2026",
		SiteTitle:  "we-clay 机器人控制台",
		FooterText: "© 2026 we-clayplus · 微信协议自动化运行系统",
		EnableCORS: true,
	}
}

