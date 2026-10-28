package gateway

import (
	"context"
	"fmt"

	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/message"
)

// SendVideo 发送本机 MP4 字节，自动读取视频时长并生成 JPEG 封面。
// 需要本机 ffprobe 和 ffmpeg；已有 Message.Send 可继续手工传入时长和封面。
// ctx 用于取消本机视频预处理；消息发送仍遵循当前 Client 的生命周期。
func (c *Client) SendVideo(ctx context.Context, receiver string, video []byte) (*message.Send_Response, error) {
	if receiver == "" {
		return nil, fmt.Errorf("视频接收人不可为空")
	}
	thumb, duration, err := message.PrepareVideo(ctx, video)
	if err != nil {
		return nil, err
	}
	return c.Message.Send(&message.Message{
		Type:     message.TypeVideo,
		Receiver: &contact.Contact{Username: receiver},
		Data: &message.Message_Video{Video: &message.VideoData{
			Media: &message.Media{Data: video, Size: uint32(len(video))},
			Thumb: thumb, Duration: duration,
		}},
	})
}
