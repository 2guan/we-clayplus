package gateway

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/we-clay/we-clay/sdk/message"
)

type videoMessageStub struct {
	message.Ability
	sent *message.Message
}

func (s *videoMessageStub) Send(msg *message.Message) (*message.Send_Response, error) {
	s.sent = msg
	return &message.Send_Response{NewId: 42}, nil
}

func TestSendVideoPreparesMediaBeforeSending(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("未安装可选 ffmpeg")
	}
	if _, err = exec.LookPath("ffprobe"); err != nil {
		t.Skip("未安装可选 ffprobe")
	}
	path := filepath.Join(t.TempDir(), "fixture.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=32x32:d=0.25", "-c:v", "mpeg4", "-y", path).CombinedOutput(); err != nil {
		t.Fatalf("生成离线视频失败: %v (%s)", err, output)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stub := new(videoMessageStub)
	client := &Client{state: &clientState{ctx: ctx}}
	client.Message = scopedMessage{state: client.state, impl: stub}
	if _, err := client.SendVideo(ctx, "", data); err == nil || stub.sent != nil {
		t.Fatal("空接收人未在发送前拒绝")
	}
	if _, err := client.SendVideo(ctx, "test-peer", []byte("not mp4")); err == nil || stub.sent != nil {
		t.Fatal("非 MP4 未在发送前拒绝")
	}
	resp, err := client.SendVideo(ctx, "test-peer", data)
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetNewId() != 42 || stub.sent.GetReceiver().GetUsername() != "test-peer" || stub.sent.GetType().GetCode() != message.TypeVideo.Code {
		t.Fatal("视频发送未保留回执或目标")
	}
	video := stub.sent.GetVideo()
	if video.GetDuration() != 1 || !bytes.Equal(video.GetMedia().GetData(), data) || video.GetMedia().GetSize() != uint32(len(data)) {
		t.Fatal("视频数据或时长错误")
	}
	if len(video.GetThumb()) < 3 || video.GetThumb()[0] != 0xff || video.GetThumb()[1] != 0xd8 {
		t.Fatal("未生成 JPEG 缩略图")
	}
	client.state.close()
	if _, err := client.SendVideo(ctx, "test-peer", data); !errors.Is(err, ErrClientClosed) {
		t.Fatalf("关闭后的客户端不应发送: %v", err)
	}
}
