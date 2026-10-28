package console

import (
	"context"
	"fmt"

	"github.com/we-clay/we-clay/sdk/message"
)

// AI 操作继续保持较小的媒体和时长上限；通用发送可使用完整上限。
func prepareAIVideo(ctx context.Context, data []byte) ([]byte, uint32, error) {
	if len(data) < 12 || len(data) > 16<<20 || string(data[4:8]) != "ftyp" {
		return nil, 0, fmt.Errorf("视频需为不超过16MB的MP4")
	}
	thumb, duration, err := message.PrepareVideo(ctx, data)
	if err != nil {
		return nil, 0, err
	}
	if duration > 600 {
		return nil, 0, fmt.Errorf("视频时长需在10分钟以内")
	}
	return thumb, duration, nil
}
