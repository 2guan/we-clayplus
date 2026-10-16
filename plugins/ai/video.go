// 文件名：video.go
// 主要功能：执行 OpenAI Videos 兼容协议的视频创建、状态轮询和受限下载，保护已受理任务不被重复生成。
// 作者：2guan
package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// 已提交任务的状态未知时停止流水线，防止切换厂商再次生成并产生重复费用。
// 用户取消只停止本地等待；远端任务是否结束由供应商决定。
type videoPendingError struct{ cause error }

func (e *videoPendingError) Error() string {
	return "视频任务可能已受理，但未取得最终结果；已停止自动切换，请在厂商后台查看任务"
}
func (e *videoPendingError) Unwrap() error { return e.cause }
func VideoJobPending(err error) bool       { var e *videoPendingError; return errors.As(err, &e) }

func generateVideo(ctx context.Context, c Config, r Request) (Result, error) {
	if c.Provider != "openai" {
		return Result{}, fmt.Errorf("视频生成需要 OpenAI Videos 兼容协议；此厂商适配器暂不支持")
	}
	if r.Size == "" {
		r.Size = "1280x720"
	}
	if r.Seconds == "" {
		r.Seconds = "4"
	}
	if !slices.Contains([]string{"1280x720", "720x1280", "1792x1024", "1024x1792"}, r.Size) || !slices.Contains([]string{"4", "8", "12"}, r.Seconds) {
		return Result{}, fmt.Errorf("视频尺寸或时长不受支持")
	}
	// 参考图只接收内嵌图片，避免把用户 URL 作为服务端下载目标。
	body := map[string]any{"model": c.Model, "prompt": r.Prompt, "size": r.Size, "seconds": r.Seconds}
	if r.Media != "" {
		data, err := base64.StdEncoding.DecodeString(r.Media)
		mime := http.DetectContentType(data)
		if err != nil || len(data) == 0 || len(data) > 16<<20 || !slices.Contains([]string{"image/png", "image/jpeg", "image/webp"}, mime) {
			return Result{}, fmt.Errorf("视频参考图需要 PNG、JPEG 或 WebP，最大 16 MB")
		}
		body["input_reference"] = map[string]string{"image_url": "data:" + mime + ";base64," + r.Media}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}
	endpoint := strings.TrimRight(c.Endpoint, "/") + "/videos"
	raw, _, err := request(ctx, endpoint, c.Key, "application/json", payload, nil)
	if err != nil {
		var status *HTTPError
		// 明确的 HTTP 拒绝允许切换；连接中断无法判断任务是否已受理。
		if errors.As(err, &status) {
			return Result{}, err
		}
		return Result{}, &videoPendingError{err}
	}
	var job struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &job) != nil || !validVideoID(job.ID) {
		return Result{}, &videoPendingError{fmt.Errorf("任务回执无效")}
	}
	id := job.ID
	// 每次只查询同一个任务；请求超时覆盖创建、轮询和下载的完整生命周期。
	for {
		switch job.Status {
		case "completed":
			data, mime, err := videoGet(ctx, endpoint+"/"+url.PathEscape(id)+"/content", c.Key, 64<<20)
			if err != nil {
				return Result{}, &videoPendingError{err}
			}
			if !strings.HasPrefix(mime, "video/mp4") || http.DetectContentType(data) != "video/mp4" {
				return Result{}, &videoPendingError{fmt.Errorf("视频文件无效")}
			}
			return Result{Data: data, MIME: "video/mp4"}, nil
		case "failed":
			return Result{}, fmt.Errorf("厂商视频生成失败，请检查提示词、参考图及模型参数")
		case "queued", "in_progress":
		default:
			return Result{}, &videoPendingError{fmt.Errorf("视频状态无效")}
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Result{}, &videoPendingError{ctx.Err()}
		case <-timer.C:
		}
		raw, _, err = videoGet(ctx, endpoint+"/"+url.PathEscape(id), c.Key, 1<<20)
		if err != nil {
			return Result{}, &videoPendingError{err}
		}
		job.Status = ""
		if json.Unmarshal(raw, &job) != nil || job.ID != id {
			return Result{}, &videoPendingError{fmt.Errorf("视频状态回执无效")}
		}
	}
}
func validVideoID(id string) bool {
	if len(id) == 0 || len(id) > 256 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// 状态和媒体始终从已配置厂商的同源地址读取，禁止认证头跟随重定向。
func videoGet(ctx context.Context, endpoint, key string, limit int64) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", fmt.Errorf("视频服务地址无效")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client := http.Client{Transport: debugTransport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", safeConnectionError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", &HTTPError{resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, "", fmt.Errorf("视频响应读取失败或超过大小限制")
	}
	return data, resp.Header.Get("Content-Type"), nil
}
