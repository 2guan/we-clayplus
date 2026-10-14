// 文件名：incoming_media_test.go
// 主要功能：使用合成音频验证 SILK 解码链路和无效输入拒绝，不连接真实微信。
// 作者：2guan
package console

import (
	"context"
	"os/exec"
	"testing"
)

func TestIncomingVoiceRoundTrip(t *testing.T) {
	requireVoiceTools(t)
	if _, err := exec.LookPath("silk_v3_decoder"); err != nil {
		t.Skip("缺少 SILK 解码器")
	}
	encoded, _, err := encodeVoice(context.Background(), voiceWAV(1))
	if err != nil {
		t.Fatal(err)
	}
	wav, err := decodeIncomingVoice(context.Background(), encoded)
	if err != nil || len(wav) < 48000 || string(wav[:4]) != "RIFF" {
		t.Fatalf("解码失败 %d %v", len(wav), err)
	}
	if _, err := decodeIncomingVoice(context.Background(), []byte("bad")); err == nil {
		t.Fatal("接受无效音频")
	}
}
