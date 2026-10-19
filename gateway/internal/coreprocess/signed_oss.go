package coreprocess

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/we-clay/we-clay/gateway/internal/config"
)

type signedDownload struct {
	ManifestURL string `json:"manifest_url"`
	AssetURL    string `json:"asset_url"`
	ExpiresAt   string `json:"expires_at"`
}

// ensureSignedOSS 使用运行授权 Key 取得短时 OSS 链接。下载请求中的 Key 只发送至授权服务；
// 签名 URL 只发给配置的 OSS 域名，既不持久化，也不包含在错误和日志中。
func (d downloader) ensureSignedOSS(ctx context.Context, cfg config.CoreConfig, want receipt) (string, error) {
	key := os.Getenv(cfg.AuthKeyEnv)
	if key == "" {
		return "", fmt.Errorf("私有 OSS 下载需要 %s 授权 Key，或 OSS 只读凭据", cfg.AuthKeyEnv)
	}
	name := want.Asset
	requestBody, err := json.Marshal(struct {
		Product string `json:"product"`
		Version string `json:"version"`
		OS      string `json:"goos"`
		Arch    string `json:"goarch"`
		Asset   string `json:"asset"`
	}{Product: "core", Version: cfg.Version, OS: d.goos, Arch: d.goarch, Asset: "bundle"})
	if err != nil {
		return "", errors.New("构造 core 下载请求失败")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.DownloadAuthBaseURL+"/v1/download", bytes.NewReader(requestBody))
	if err != nil {
		return "", errors.New("授权下载服务地址无效")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "we-clay-gateway")
	client := *d.client
	client.Timeout = 30 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("请求授权下载服务失败；请检查网络和授权服务")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("授权下载服务返回 HTTP %d；请检查授权 Key 和下载权限", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, (16<<10)+1))
	var links signedDownload
	if err != nil || len(raw) > 16<<10 || json.Unmarshal(raw, &links) != nil {
		return "", errors.New("授权下载服务响应无效")
	}
	expires, err := time.Parse(time.RFC3339, links.ExpiresAt)
	if err != nil || !expires.After(time.Now()) {
		return "", errors.New("授权下载链接已过期或缺少有效期")
	}
	if !signedOSSURLAllowed(links.ManifestURL, cfg.OSS, cfg.Version, "manifest.json") || !signedOSSURLAllowed(links.AssetURL, cfg.OSS, cfg.Version, name) {
		return "", errors.New("授权下载服务返回了不匹配的 OSS 签名链接")
	}
	manifest, err := d.fetchSignedOSS(ctx, links.ManifestURL)
	if err != nil {
		return "", err
	}
	raw, err = io.ReadAll(io.LimitReader(manifest, (2<<20)+1))
	manifest.Close()
	if err != nil || len(raw) > 2<<20 {
		return "", errors.New("OSS 版本清单无效或版本/协议不匹配")
	}
	want, err = verifyOSSManifest(raw, cfg, d, want)
	if err != nil {
		return "", err
	}
	return saveDownload(ctx, cfg, want, func(ctx context.Context) (io.ReadCloser, error) {
		return d.fetchSignedOSS(ctx, links.AssetURL)
	})
}

func signedOSSURLAllowed(raw string, cfg *config.OSSConfig, version, name string) bool {
	if cfg == nil {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" || u.RawQuery == "" {
		return false
	}
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return false
	}
	allowedHost := endpoint.Hostname()
	if endpoint.Port() != "" {
		if u.Port() != endpoint.Port() {
			return false
		}
	} else if u.Port() != "" && u.Port() != "443" {
		return false
	}
	objectPath := "/" + cfg.Prefix + "/" + version + "/" + name
	if cfg.CName {
		return strings.EqualFold(u.Hostname(), allowedHost) && u.Path == objectPath
	}
	if strings.EqualFold(u.Hostname(), cfg.Bucket+"."+allowedHost) {
		return u.Path == objectPath
	}
	return strings.EqualFold(u.Hostname(), allowedHost) && u.Path == "/"+cfg.Bucket+objectPath
}

func (d downloader) fetchSignedOSS(ctx context.Context, signedURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, nil)
	if err != nil {
		return nil, errors.New("OSS 签名下载链接无效")
	}
	req.Header.Set("User-Agent", "we-clay-gateway")
	client := *d.client
	client.Timeout = 10 * time.Minute
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("OSS 签名链接下载失败；请检查网络或重新运行以获取新链接")
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("OSS 签名链接下载返回 HTTP %d；请检查授权或重新运行", res.StatusCode)
	}
	return res.Body, nil
}
