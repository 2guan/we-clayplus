package coreprocess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/we-clay/we-clay/gateway/internal/config"
)

type ossManifest struct {
	SchemaVersion   int    `json:"schema_version"`
	ProtocolVersion int    `json:"protocol_version"`
	Version         string `json:"version"`
	Assets          []struct {
		Name   string `json:"name"`
		OS     string `json:"goos"`
		Arch   string `json:"goarch"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	} `json:"assets"`
}

func ossSource(c config.CoreConfig) string {
	if c.OSS == nil {
		return ""
	}
	return c.OSS.SourceID()
}

func (d downloader) ensureOSS(ctx context.Context, cfg config.CoreConfig) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	name := assetNames(cfg.Version, d.goos, d.goarch)[0]
	want := receipt{Source: ossSource(cfg), Repository: cfg.Repository, Version: cfg.Version, Asset: name}
	if path, err := cached(ctx, cacheDirectory(cfg, name), want); !errors.Is(err, os.ErrNotExist) {
		return path, err
	}
	if os.Getenv("OSS_ACCESS_KEY_ID") == "" || os.Getenv("OSS_ACCESS_KEY_SECRET") == "" {
		return "", errors.New("私有 OSS 下载需要 OSS_ACCESS_KEY_ID 和 OSS_ACCESS_KEY_SECRET；请配置只读凭据环境变量")
	}
	// 显式关闭 SDK 调试日志和重定向，签名与临时票据不输出、不转发给其他站点。
	client := *d.client
	client.Timeout = 10 * time.Minute
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	o := cfg.OSS
	c := oss.NewClient(oss.NewConfig().WithRegion(o.Region).WithEndpoint(o.Endpoint).WithUseCName(o.CName).
		WithCredentialsProvider(credentials.NewEnvironmentVariableCredentialsProvider()).WithHttpClient(&client).
		WithRetryMaxAttempts(3).WithLogLevel(oss.LogOff))
	fetch := func(ctx context.Context, filename string) (io.ReadCloser, error) {
		result, err := c.GetObject(ctx, &oss.GetObjectRequest{Bucket: oss.Ptr(o.Bucket), Key: oss.Ptr(o.Prefix + "/" + cfg.Version + "/" + filename)})
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var serviceErr *oss.ServiceError
			if errors.As(err, &serviceErr) {
				return nil, fmt.Errorf("OSS 下载返回 HTTP %d；检查版本、地域、Bucket 和读取权限", serviceErr.StatusCode)
			}
			return nil, errors.New("OSS 下载请求失败；请检查网络、HTTPS 和读取凭据")
		}
		return result.Body, nil
	}
	body, err := fetch(ctx, "manifest.json")
	if err != nil {
		return "", err
	}
	raw, err := io.ReadAll(io.LimitReader(body, (2<<20)+1))
	body.Close()
	var m ossManifest
	if err != nil || len(raw) > 2<<20 || json.Unmarshal(raw, &m) != nil || m.SchemaVersion != 1 || m.ProtocolVersion != 1 || m.Version != cfg.Version {
		return "", errors.New("OSS 版本清单无效或版本/协议不匹配")
	}
	found := false
	seen := map[string]bool{}
	for _, a := range m.Assets {
		if seen[a.Name] {
			return "", errors.New("OSS 清单包含重复附件")
		}
		seen[a.Name] = true
		if a.Name != name {
			continue
		}
		if a.OS != d.goos || a.Arch != d.goarch || a.Size <= 0 || a.Size > maxBinarySize || !validHash(a.SHA256) {
			return "", errors.New("OSS 当前平台附件信息无效")
		}
		want.Size, want.SHA256, found = a.Size, strings.ToLower(a.SHA256), true
	}
	if !found {
		return "", fmt.Errorf("OSS 版本 %s 缺少当前平台附件 %s", cfg.Version, name)
	}
	return saveDownload(ctx, cfg, want, func(ctx context.Context) (io.ReadCloser, error) { return fetch(ctx, name) })
}
