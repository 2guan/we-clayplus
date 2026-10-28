package message

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareVideoRejectsInvalidMP4BeforeToolLookup(t *testing.T) {
	t.Setenv("WE_CLAY_FFMPEG", filepath.Join(t.TempDir(), "missing-ffmpeg"))
	for _, data := range [][]byte{nil, []byte("https://example.test/video.mp4"), []byte("#EXTM3U\nfile:///fixture")} {
		if _, _, err := PrepareVideo(context.Background(), data); err == nil || !strings.Contains(err.Error(), "MP4") {
			t.Fatalf("非 MP4 未明确拒绝: %v", err)
		}
	}
	data := []byte{0, 0, 0, 16, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := PrepareVideo(ctx, data); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消的视频处理未保留 context 错误: %v", err)
	}
	if _, _, err := PrepareVideo(context.Background(), data); err == nil || !strings.Contains(err.Error(), "WE_CLAY_FFMPEG") {
		t.Fatalf("缺失可选工具未明确提示: %v", err)
	}
}
