// 文件名: plugins/ai/provider/mimo.go
// 主要功能: 小米 mimo 多模态对话与语音合成 (TTS) 全系列驱动实现
// 作者: 2guan

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MimoProvider 小米 mimo 多模态驱动封装
type MimoProvider struct {
	*OpenAICompatibleClient
	apiKey     string
	baseURL    string
	audioModel string
	httpClient *http.Client
}

// NewMimoProvider 实例化小米 mimo 驱动
func NewMimoProvider(apiKey, baseURL, defaultModel, audioModel string, timeoutSec int) *MimoProvider {
	if baseURL == "" {
		baseURL = "https://api.mimo.mi.com/v1"
	}
	if audioModel == "" {
		audioModel = "mimo-v1-speech"
	}

	timeout := time.Duration(timeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	client := &http.Client{Timeout: timeout}

	compatibleClient := NewOpenAICompatibleClient(OpenAICompatibleConfig{
		Name:       "mimo",
		BaseURL:    baseURL,
		APIKey:     apiKey,
		Timeout:    timeout,
		HTTPClient: client,
	})

	return &MimoProvider{
		OpenAICompatibleClient: compatibleClient,
		apiKey:                 apiKey,
		baseURL:                compatibleClient.baseURL,
		audioModel:             audioModel,
		httpClient:             client,
	}
}

// SynthesizeSpeech 调用小米 mimo 语音合成 API 将文本转换为音频流 (MP3/SILK)
func (m *MimoProvider) SynthesizeSpeech(ctx context.Context, text string, voice string) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("[mimo] 语音合成文本不能为空")
	}
	if voice == "" {
		voice = "xiaomi-sweet"
	}

	reqPayload := map[string]any{
		"model": m.audioModel,
		"input": text,
		"voice": voice,
	}

	bodyBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("[mimo] 序列化语音合成请求失败: %w", err)
	}

	endpoint := fmt.Sprintf("%s/audio/speech", m.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("[mimo] 创建语音合成 HTTP 请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if m.apiKey != "" {
		httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", m.apiKey))
	}

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("[mimo] 语音合成请求网络失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("[mimo] 语音合成失败，状态码 %d: %s", resp.StatusCode, string(errBody))
	}

	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("[mimo] 读取语音合成音频流失败: %w", err)
	}

	return audioData, nil
}
