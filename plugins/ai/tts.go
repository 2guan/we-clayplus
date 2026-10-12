// 文件名: plugins/ai/tts.go
// 主要功能: 小米 mimo 语音合成与微信语音消息发送适配
// 作者: 2guan

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/message"
)

// TTSService 语音服务管理器
type TTSService struct {
	router *Router
	cfg    VoiceReplyConfig
}

// NewTTSService 实例化语音服务
func NewTTSService(router *Router, cfg VoiceReplyConfig) *TTSService {
	return &TTSService{
		router: router,
		cfg:    cfg,
	}
}

// SendVoiceMessage 使用 mimo 语音引擎合成文本并以原生微信语音发送
func (s *TTSService) SendVoiceMessage(ctx context.Context, msgAbility message.Ability, receiver string, text string) error {
	if msgAbility == nil {
		return fmt.Errorf("消息发送能力未就绪")
	}

	voiceName := s.cfg.VoiceName
	if voiceName == "" {
		voiceName = "xiaomi-sweet"
	}

	slog.Info("[tts] 正在调用 mimo 语音合成引擎...", "receiver", receiver, "voice", voiceName)

	audioBytes, err := s.router.SynthesizeSpeech(ctx, text, voiceName)
	if err != nil {
		return fmt.Errorf("mimo 语音合成失败: %w", err)
	}

	// 组装原生微信语音消息结构体
	formatMp3 := int32(2) // 2=MP3
	voiceMsg := &message.Message{
		Receiver: &contact.Contact{Username: receiver},
		Type:     message.TypeVoice,
		Content:  text, // 附带文本摘要
		Data: &message.Message_Voice{
			Voice: &message.VoiceData{
				Media: &message.Media{
					Data: audioBytes,
					Size: uint32(len(audioBytes)),
				},
				Duration: 3000, // 默认估算 3 秒
				Format:   &formatMp3,
			},
		},
	}

	_, err = msgAbility.Send(voiceMsg)
	if err != nil {
		return fmt.Errorf("发送微信语音消息失败: %w", err)
	}

	slog.Info("[tts] 微信语音消息发送成功", "receiver", receiver, "bytes", len(audioBytes))
	return nil
}
