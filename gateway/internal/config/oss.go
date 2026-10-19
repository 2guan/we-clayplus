package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// OSSConfig 只保存下载位置。凭据从 OSS_ACCESS_KEY_ID、OSS_ACCESS_KEY_SECRET、
// 可选 OSS_SESSION_TOKEN 环境变量读取，不能写入配置文件。
type OSSConfig struct {
	Bucket   string `toml:"bucket"`
	Region   string `toml:"region"`
	Prefix   string `toml:"prefix"`   // 默认 core，版本文件位于 prefix/version/。
	Endpoint string `toml:"endpoint"` // 默认地域 HTTPS endpoint。
	CName    bool   `toml:"cname"`    // 自定义 Bucket 域名时设为 true。
}

func (c OSSConfig) Resolve() (OSSConfig, error) {
	if c.Prefix == "" {
		c.Prefix = "core"
	}
	if c.Endpoint == "" {
		c.Endpoint = "https://oss-" + c.Region + ".aliyuncs.com"
	}
	c.Endpoint = strings.TrimRight(c.Endpoint, "/")
	return c, c.Validate()
}

func (c OSSConfig) Validate() error {
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`).MatchString(c.Bucket) {
		return fmt.Errorf("core.oss.bucket 无效")
	}
	if !regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)+$`).MatchString(c.Region) {
		return fmt.Errorf("core.oss.region 无效")
	}
	if c.Prefix == "" {
		return fmt.Errorf("core.oss.prefix 不能为空")
	}
	for _, part := range strings.Split(c.Prefix, "/") {
		if part == "." || part == ".." || !regexp.MustCompile(`^[A-Za-z0-9._-]+$`).MatchString(part) {
			return fmt.Errorf("core.oss.prefix 必须为不含相对路径的对象前缀")
		}
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("core.oss.endpoint 必须为不含凭据、路径或查询参数的 HTTPS 地址")
	}
	return nil
}

// SourceID 用于缓存和进程复用隔离，不包含凭据。
func (c OSSConfig) SourceID() string {
	return fmt.Sprintf("oss:%s|%s|%s|%s|%t", c.Region, c.Bucket, c.Prefix, c.Endpoint, c.CName)
}
