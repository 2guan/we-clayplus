// 文件名：voice.go
// 主要功能：将模型生成的 WAV 转为微信原生 SILK，验证时长、编码和工具执行边界。
// 作者：2guan
package console

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// encodeVoice 只接受 WAV；工具路径来自部署环境，不允许浏览器指定命令。
func encodeVoice(ctx context.Context, data []byte) ([]byte, uint32, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" || len(data) > 16<<20 {
		return nil, 0, fmt.Errorf("语音必须是有效的 WAV，且不超过 16 MB")
	}
	tool := func(env, fallback string) (string, error) {
		name := os.Getenv(env)
		if name == "" {
			name = fallback
		}
		path, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("缺少 %s，请安装工具或配置 %s", fallback, env)
		}
		return path, nil
	}
	ffmpeg, err := tool("WE_CLAY_FFMPEG", "ffmpeg")
	if err != nil {
		return nil, 0, err
	}
	encoder, err := tool("WE_CLAY_SILK_ENCODER", "silk_v3_encoder")
	if err != nil {
		return nil, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "weclay-voice-")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(dir)
	pcm, silk := filepath.Join(dir, "voice.pcm"), filepath.Join(dir, "voice.silk")
	// 固定格式和协议，不解析播放列表或外部 URL。最多解码 61 秒，用于拒绝超长语音而非静默截断。
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-protocol_whitelist", "pipe", "-f", "wav", "-i", "pipe:0", "-t", "61", "-ac", "1", "-ar", "24000", "-f", "s16le", pcm)
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Run(); err != nil {
		return nil, 0, fmt.Errorf("WAV 解码失败，请检查音频或 ffmpeg 安装")
	}
	info, err := os.Stat(pcm)
	if err != nil {
		return nil, 0, err
	}
	// 24 kHz、单声道、16 位 PCM：每毫秒 48 字节。微信语音最长 60 秒。
	duration := uint32((info.Size() + 47) / 48)
	if duration < 20 || duration > 60000 {
		return nil, 0, fmt.Errorf("语音长度需在 0.02 至 60 秒内，请缩短文字后重新合成")
	}
	cmd = exec.CommandContext(ctx, encoder, pcm, silk, "-Fs_API", "24000", "-rate", "24000", "-tencent")
	if err := cmd.Run(); err != nil {
		return nil, 0, fmt.Errorf("SILK 编码失败，请检查编码器安装")
	}
	encoded, err := os.ReadFile(silk)
	if err != nil {
		return nil, 0, err
	}
	if len(encoded) <= 12 || len(encoded) > 1<<20 || !bytes.HasPrefix(encoded, []byte("\x02#!SILK_V3")) {
		return nil, 0, fmt.Errorf("编码器未产生有效的腾讯 SILK 数据")
	}
	// 标准 SILK 结束标记不参与微信分片负载。
	encoded = bytes.TrimSuffix(encoded, []byte{0xff, 0xff})
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	return encoded, duration, nil
}
