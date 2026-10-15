// 文件名：incoming_media.go
// 主要功能：校验当前账号及会话绑定，下载入站图片或语音并将 SILK 解码为 WAV。
// 作者：2guan
package console

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/protobuf/encoding/protojson"
)

func incomingMedia(ctx context.Context, c control.Command) ([]byte, error) {
	var m message.Message
	if len(c.Params) > 256<<10 || protojson.Unmarshal(c.Params, &m) != nil {
		return nil, fmt.Errorf("媒体票据无效")
	}
	self := contact.Instance.GetSelf()
	if self == nil {
		return nil, fmt.Errorf("微信未就绪")
	}
	target := m.GetSender().GetUsername()
	if target == self.Username {
		target = m.GetReceiver().GetUsername()
	}
	if target != c.Target || strconv.FormatUint(uint64(m.GetId()), 10) != c.ID || (m.GetType().GetCode() != 3 && m.GetType().GetCode() != 34 && m.GetType().GetCode() != message.TypeAppFileAttach.Code) {
		return nil, fmt.Errorf("媒体与目标会话不匹配")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, err := message.Instance.Download(&m)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, (16<<20)+1))
	if err != nil || len(data) == 0 || len(data) > 16<<20 {
		return nil, fmt.Errorf("下载媒体为空或过大")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.GetType().GetCode() == 34 {
		return decodeIncomingVoice(ctx, data)
	}
	return data, nil
}

// 解码只处理有腾讯/标准 SILK 文件头的本机数据；命令参数固定，不读取网络地址。
func decodeIncomingVoice(ctx context.Context, data []byte) ([]byte, error) {
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		return data, nil
	}
	if !bytes.HasPrefix(data, []byte("\x02#!SILK_V3")) && !bytes.HasPrefix(data, []byte("#!SILK_V3")) {
		return nil, fmt.Errorf("暂不支持此入站语音编码")
	}
	decoder := os.Getenv("WE_CLAY_SILK_DECODER")
	if decoder == "" {
		decoder = "silk_v3_decoder"
	}
	path, err := exec.LookPath(decoder)
	if err != nil {
		return nil, fmt.Errorf("缺少 silk_v3_decoder，请安装或配置 WE_CLAY_SILK_DECODER")
	}
	ffmpeg := os.Getenv("WE_CLAY_FFMPEG")
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "weclay-incoming-voice-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	input, pcm := filepath.Join(dir, "input.silk"), filepath.Join(dir, "output.pcm")
	if err = os.WriteFile(input, data, 0600); err != nil {
		return nil, err
	}
	if err = exec.CommandContext(ctx, path, input, pcm, "-Fs_API", "24000").Run(); err != nil {
		return nil, fmt.Errorf("SILK 语音解码失败")
	}
	info, err := os.Stat(pcm)
	if err != nil || info.Size() == 0 || info.Size() > 60000*48 {
		return nil, fmt.Errorf("入站语音长度无效或超过 60 秒")
	}
	out, err := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-f", "s16le", "-ar", "24000", "-ac", "1", "-i", pcm, "-f", "wav", "pipe:1").Output()
	if err != nil {
		return nil, fmt.Errorf("入站语音 WAV 转换失败")
	}
	return out, nil
}
