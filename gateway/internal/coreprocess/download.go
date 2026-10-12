// Package coreprocess 下载经过校验的 core，并管理本次 Run 创建的子进程。
package coreprocess

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/we-clay/we-clay/gateway/internal/config"
)

const maxBinarySize = 512 << 20

type asset struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
	State  string `json:"state"`
}
type release struct {
	Tag    string  `json:"tag_name"`
	Draft  bool    `json:"draft"`
	Assets []asset `json:"assets"`
}
type receipt struct {
	Repository string `json:"repository"`
	Version    string `json:"version"`
	Asset      string `json:"asset"`
	SHA256     string `json:"sha256"`
	Size       int64  `json:"size"`
}
type downloader struct {
	client       *http.Client
	baseURL      string
	goos, goarch string
}

// Ensure 在首次使用时访问 GitHub；有效缓存可离线复用，每次启动均重新计算哈希。
func Ensure(ctx context.Context, cfg config.CoreConfig) (string, error) {
	d := downloader{client: &http.Client{Timeout: 10 * time.Minute, CheckRedirect: checkRedirect}, baseURL: "https://api.github.com", goos: runtime.GOOS, goarch: runtime.GOARCH}
	return d.ensure(ctx, cfg)
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("GitHub 附件重定向次数过多")
	}
	if req.URL.Scheme != "https" {
		return errors.New("GitHub 附件不允许重定向到非 HTTPS 地址")
	}
	// GitHub 会跳转至签名附件地址；即使是子域名也不向其转发令牌。
	req.Header.Del("Authorization")
	return nil
}

func assetName(goos, goarch string) string {
	name := "we-clay-core-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func assetNames(version, goos, goarch string) []string {
	name := "we-clay-core_" + version + "_" + goos + "_" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return []string{name, assetName(goos, goarch)}
}

func cacheDirectory(cfg config.CoreConfig, name string) string {
	key := sha256.Sum256([]byte(cfg.Repository + "\x00" + cfg.Version + "\x00" + name))
	return filepath.Join(cfg.CacheDir, hex.EncodeToString(key[:]))
}

func (d downloader) ensure(ctx context.Context, cfg config.CoreConfig) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	names := assetNames(cfg.Version, d.goos, d.goarch)
	want := receipt{Repository: cfg.Repository, Version: cfg.Version}
	// 两种命名均只接受精确的平台和版本，保留已有缓存的离线复用。
	for _, name := range names {
		want.Asset = name
		if path, err := cached(ctx, cacheDirectory(cfg, name), want); !errors.Is(err, os.ErrNotExist) {
			return path, err
		}
	}
	token := os.Getenv(cfg.GitHubTokenEnv)
	res, err := d.get(ctx, "/repos/"+cfg.Repository+"/releases/tags/"+url.PathEscape(cfg.Version), token, false)
	if err != nil {
		return "", err
	}
	var rel release
	err = json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&rel)
	res.Body.Close()
	if err != nil {
		return "", errors.New("GitHub Release 元数据无效")
	}
	if rel.Draft || rel.Tag != cfg.Version {
		return "", errors.New("GitHub Release 与指定版本不匹配，或仍为草稿")
	}
	var binary, sums asset
	for _, name := range names {
		for _, a := range rel.Assets {
			if a.Name == name && a.State == "uploaded" && a.ID > 0 {
				binary = a
				break
			}
		}
		if binary.ID > 0 {
			break
		}
	}
	for _, a := range rel.Assets {
		if a.Name == "SHA256SUMS" {
			sums = a
		}
	}
	if binary.ID <= 0 || binary.State != "uploaded" {
		return "", fmt.Errorf("Release %s 缺少当前平台附件（%s）", cfg.Version, strings.Join(names, " 或 "))
	}
	name := binary.Name
	want.Asset = name
	dir := cacheDirectory(cfg, name)
	if binary.Size <= 0 || binary.Size > maxBinarySize {
		return "", errors.New("core 附件大小无效或超过 512 MiB")
	}
	// 新附件优先采用 GitHub 提供的 SHA-256，旧附件可使用同一 Release 的 SHA256SUMS。
	if binary.Digest != "" {
		if !strings.HasPrefix(binary.Digest, "sha256:") {
			return "", errors.New("GitHub 附件摘要算法不受支持")
		}
		want.SHA256 = strings.TrimPrefix(binary.Digest, "sha256:")
	} else {
		if sums.ID <= 0 || sums.State != "uploaded" {
			return "", errors.New("Release 附件缺少 SHA-256 摘要，请上传 SHA256SUMS")
		}
		res, err = d.get(ctx, fmt.Sprintf("/repos/%s/releases/assets/%d", cfg.Repository, sums.ID), token, true)
		if err != nil {
			return "", err
		}
		want.SHA256, err = checksum(io.LimitReader(res.Body, 1<<20), name)
		res.Body.Close()
		if err != nil {
			return "", err
		}
	}
	want.SHA256 = strings.ToLower(want.SHA256)
	if !validHash(want.SHA256) {
		return "", errors.New("Release 的 SHA-256 摘要无效")
	}
	want.Size = binary.Size
	if err = os.MkdirAll(cfg.CacheDir, 0700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(cfg.CacheDir, ".download-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	file, err := os.OpenFile(filepath.Join(tmp, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	slog.Info("正在下载 core", "repository", cfg.Repository, "version", cfg.Version, "asset", name)
	res, err = d.get(ctx, fmt.Sprintf("/repos/%s/releases/assets/%d", cfg.Repository, binary.ID), token, true)
	if err != nil {
		file.Close()
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, h), io.LimitReader(res.Body, want.Size+1))
	res.Body.Close()
	closeErr := file.Close()
	if err != nil {
		return "", errors.New("下载 core 中断；未保存为可执行文件")
	}
	if closeErr != nil {
		return "", closeErr
	}
	if n != want.Size || hex.EncodeToString(h.Sum(nil)) != want.SHA256 {
		return "", errors.New("core 大小或 SHA-256 校验失败，拒绝执行")
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = os.Chmod(filepath.Join(tmp, name), 0700); err != nil {
		return "", err
	}
	raw, _ := json.Marshal(want)
	if err = os.WriteFile(filepath.Join(tmp, "receipt.json"), raw, 0600); err != nil {
		return "", err
	}
	// 目录整体原子发布；其他进程若抢先完成，只接受其通过校验的同版本缓存。
	if err = os.Rename(tmp, dir); err != nil {
		if path, cacheErr := cached(ctx, dir, want); cacheErr == nil {
			return path, nil
		}
		return "", fmt.Errorf("发布 core 缓存失败: %w", err)
	}
	return filepath.Join(dir, name), nil
}

func (d downloader) get(ctx context.Context, path, token string, binary bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if binary {
		req.Header.Set("Accept", "application/octet-stream")
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "we-clay-gateway")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := d.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// url.Error 可能含临时签名链接，错误中不返回该 URL 或响应体。
		return nil, errors.New("GitHub 下载请求失败，请检查网络、代理或 HTTPS 重定向")
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("GitHub 返回 HTTP %d；检查 Release tag、附件及仓库读取权限", res.StatusCode)
	}
	return res, nil
}

func checksum(r io.Reader, name string) (string, error) {
	s := bufio.NewScanner(r)
	found := ""
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			if found != "" {
				return "", errors.New("SHA256SUMS 含重复附件")
			}
			found = fields[0]
		}
	}
	if s.Err() != nil || !validHash(found) {
		return "", errors.New("SHA256SUMS 缺少当前附件的有效摘要")
	}
	return found, nil
}
func validHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size
}

func cached(ctx context.Context, dir string, want receipt) (string, error) {
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	bad := fmt.Errorf("core 缓存无效：%s；请移除该版本缓存后重试", dir)
	if !dirInfo.IsDir() {
		return "", bad
	}
	info, err := os.Lstat(filepath.Join(dir, "receipt.json"))
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return "", bad
	}
	raw, err := os.ReadFile(filepath.Join(dir, "receipt.json"))
	if err != nil {
		return "", bad
	}
	var got receipt
	if json.Unmarshal(raw, &got) != nil || got.Repository != want.Repository || got.Version != want.Version || got.Asset != want.Asset || !validHash(got.SHA256) || got.Size <= 0 || got.Size > maxBinarySize {
		return "", bad
	}
	if want.SHA256 != "" && (want.SHA256 != got.SHA256 || want.Size != got.Size) {
		return "", bad
	}
	path := filepath.Join(dir, want.Asset)
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != got.Size {
		return "", bad
	}
	f, err := os.Open(path)
	if err != nil {
		return "", bad
	}
	defer f.Close()
	h := sha256.New()
	buffer := make([]byte, 128<<10)
	for {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := f.Read(buffer)
		h.Write(buffer[:n])
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", bad
		}
	}
	if hex.EncodeToString(h.Sum(nil)) != got.SHA256 {
		return "", bad
	}
	return path, nil
}
