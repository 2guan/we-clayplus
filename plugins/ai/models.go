// 文件名：models.go
// 主要功能：获取厂商模型列表，适配 MiMo 三种语音合成及携带垫图的图片编辑请求。
// 作者：2guan
package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// 网络错误不回显 URL、请求头或供应商原文，保留超时语义供流水线判断。
func safeConnectionError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return errors.New("无法连接模型服务")
}

// Discover 使用各协议的模型目录；Mock 在创建网络请求之前返回独立的示例目录。
func Discover(ctx context.Context, c Config, mock bool) ([]string, error) {
	if mock {
		return []string{"mock-chat", "mock-vision", "mock-tts", "mimo-v2.5-tts", "mimo-v2.5-tts-voicedesign", "mimo-v2.5-tts-voiceclone", "mock-image", "mock-video"}, nil
	}
	endpoint, err := url.Parse(c.Endpoint)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, fmt.Errorf("API 地址无效")
	}
	if c.Key == "" {
		return nil, fmt.Errorf("请填写 API Key")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	result := []string{}
	next := ""
	// 限制分页和总响应，避免错误的供应商接口无限翻页。
	for page := 0; page < 20; page++ {
		u := strings.TrimRight(c.Endpoint, "/") + "/models"
		params := url.Values{}
		if c.Provider == "gemini" {
			params.Set("pageSize", "1000")
			if next != "" {
				params.Set("pageToken", next)
			}
		}
		if c.Provider == "anthropic" {
			params.Set("limit", "1000")
			if next != "" {
				params.Set("after_id", next)
			}
		}
		if len(params) > 0 {
			u += "?" + params.Encode()
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		switch c.Provider {
		case "gemini":
			req.Header.Set("x-goog-api-key", c.Key)
		case "anthropic":
			req.Header.Set("x-api-key", c.Key)
			req.Header.Set("anthropic-version", "2023-06-01")
		default:
			req.Header.Set("Authorization", "Bearer "+c.Key)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, safeConnectionError(err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, &HTTPError{resp.StatusCode}
		}
		if readErr != nil || len(raw) > 4<<20 {
			return nil, fmt.Errorf("模型列表过大或读取失败")
		}
		var v struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
			Next string `json:"nextPageToken"`
			More bool   `json:"has_more"`
			Last string `json:"last_id"`
		}
		if json.Unmarshal(raw, &v) != nil {
			return nil, fmt.Errorf("模型列表格式无效")
		}
		for _, m := range v.Data {
			if m.ID != "" {
				result = append(result, m.ID)
			}
		}
		for _, m := range v.Models {
			if m.Name != "" {
				result = append(result, strings.TrimPrefix(m.Name, "models/"))
			}
		}
		previous := next
		next = v.Next
		if c.Provider == "anthropic" && v.More {
			next = v.Last
		}
		if next == "" || next == previous {
			break
		}
	}
	slices.Sort(result)
	result = slices.Compact(result)
	if len(result) == 0 {
		return nil, fmt.Errorf("厂商未返回可用模型，可手动填写模型 ID")
	}
	return result, nil
}

// MiMo 合成遵循官方非流式契约：assistant 为合成文案，user 为声音设计，audio.voice 为音色或样本。
func mimoSpeech(ctx context.Context, c Config, r Request) (Result, error) {
	audio := map[string]any{"format": "wav"}
	switch {
	case strings.HasSuffix(c.Model, "-voicedesign"):
		if strings.TrimSpace(c.Design) == "" {
			return Result{}, fmt.Errorf("声音设计需要填写声音描述")
		}
	case strings.HasSuffix(c.Model, "-voiceclone"):
		if len(r.Media) > 10<<20 || r.Media == "" || !slices.Contains([]string{"audio/mpeg", "audio/mp3", "audio/wav", "audio/x-wav", "audio/wave"}, r.MIME) {
			return Result{}, fmt.Errorf("声音克隆需要 WAV 或 MP3 样本，Base64 不超过 10 MB")
		}
		if _, err := base64.StdEncoding.DecodeString(r.Media); err != nil {
			return Result{}, fmt.Errorf("音色样本编码无效")
		}
		mime := r.MIME
		if mime == "audio/x-wav" || mime == "audio/wave" {
			mime = "audio/wav"
		}
		audio["voice"] = "data:" + mime + ";base64," + r.Media
	default:
		voice := c.Voice
		if voice == "" || voice == "alloy" {
			voice = "mimo_default"
		}
		audio["voice"] = voice
	}
	body := map[string]any{"model": c.Model, "messages": []any{map[string]string{"role": "user", "content": c.Design}, map[string]string{"role": "assistant", "content": r.Prompt}}, "audio": audio}
	raw, _ := json.Marshal(body)
	response, _, err := request(ctx, strings.TrimRight(c.Endpoint, "/")+"/chat/completions", c.Key, "application/json", raw, nil)
	if err != nil {
		return Result{}, err
	}
	var v struct {
		Choices []struct {
			Message struct {
				Audio struct {
					Data string `json:"data"`
				} `json:"audio"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(response, &v) != nil || len(v.Choices) == 0 {
		return Result{}, fmt.Errorf("MiMo 未返回音频")
	}
	data, err := base64.StdEncoding.DecodeString(v.Choices[0].Message.Audio.Data)
	if err != nil || len(data) == 0 {
		return Result{}, fmt.Errorf("MiMo 音频内容无效")
	}
	return Result{Data: data, MIME: "audio/wav"}, nil
}

// 图片编辑使用 multipart，图片内容留在有大小限制的请求体，绝不拼进 URL。
func imageEdit(c Config, r Request) ([]byte, string, error) {
	data, err := base64.StdEncoding.DecodeString(r.Media)
	if err != nil || len(data) == 0 || len(data) > 16<<20 || !strings.HasPrefix(http.DetectContentType(data), "image/") {
		return nil, "", fmt.Errorf("垫图必须是有效图片，大小不超过 16 MB")
	}
	var b bytes.Buffer
	writer := multipart.NewWriter(&b)
	for k, v := range map[string]string{"model": c.Model, "prompt": r.Prompt, "size": c.Size, "n": "1"} {
		_ = writer.WriteField(k, v)
	}
	if c.Quality != "" {
		_ = writer.WriteField("quality", c.Quality)
	}
	if !strings.HasPrefix(c.Model, "gpt-image") {
		_ = writer.WriteField("response_format", "b64_json")
	}
	filename := "reference.png"
	if http.DetectContentType(data) == "image/jpeg" {
		filename = "reference.jpg"
	}
	part, err := writer.CreateFormFile("image", filename)
	if err != nil {
		return nil, "", err
	}
	_, _ = part.Write(data)
	_ = writer.Close()
	return b.Bytes(), writer.FormDataContentType(), nil
}
