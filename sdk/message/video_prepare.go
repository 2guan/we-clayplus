package message

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// core 当前视频上传入口限制 40 MiB；此便利入口须在预处理前遵守同一上限。
const maxPreparedVideoBytes = 40 << 20

// PrepareVideo 从本机 MP4 读取时长并生成 JPEG 封面，供发送视频消息使用。
// 仅调用此便利函数时需要安装 ffprobe 和 ffmpeg；已有的 Message.Send 手工传参方式不受影响。
// WE_CLAY_FFPROBE 和 WE_CLAY_FFMPEG 可指定可执行文件路径。
// 返回的时长以秒为单位并向上取整。处理失败时不会发送消息。
func PrepareVideo(ctx context.Context, data []byte) ([]byte, uint32, error) {
	if ctx == nil {
		return nil, 0, fmt.Errorf("视频处理需要 context")
	}
	if len(data) < 12 || len(data) > maxPreparedVideoBytes || string(data[4:8]) != "ftyp" {
		return nil, 0, fmt.Errorf("视频需为不超过 40 MiB 的 MP4")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	ffmpeg, err := videoTool("WE_CLAY_FFMPEG", "ffmpeg")
	if err != nil {
		return nil, 0, err
	}
	probe, err := videoTool("WE_CLAY_FFPROBE", "ffprobe")
	if err != nil {
		return nil, 0, err
	}
	dir, err := os.MkdirTemp("", "weclay-video-")
	if err != nil {
		return nil, 0, fmt.Errorf("创建视频处理目录失败: %w", err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, 0, fmt.Errorf("写入待处理视频失败: %w", err)
	}
	raw, err := exec.CommandContext(ctx, probe, "-v", "error", "-protocol_whitelist", "file,pipe", "-f", "mov", "-enable_drefs", "0", "-show_entries", "format=duration:stream=codec_type,width,height", "-of", "json", path).Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, fmt.Errorf("视频时长解析失败")
	}
	var meta struct {
		Format  struct{ Duration string }
		Streams []struct {
			Kind          string `json:"codec_type"`
			Width, Height int
		}
	}
	if len(raw) > 1<<20 || json.Unmarshal(raw, &meta) != nil || len(meta.Streams) > 16 {
		return nil, 0, fmt.Errorf("视频元数据无效")
	}
	videos := 0
	for _, stream := range meta.Streams {
		if stream.Kind == "video" {
			videos++
			if stream.Width < 1 || stream.Height < 1 || stream.Width > 8192 || stream.Height > 8192 || int64(stream.Width)*int64(stream.Height) > 40000000 {
				return nil, 0, fmt.Errorf("视频画面尺寸超过支持范围")
			}
		}
	}
	if videos != 1 {
		return nil, 0, fmt.Errorf("视频需包含一个有效画面轨道")
	}
	seconds, err := strconv.ParseFloat(meta.Format.Duration, 64)
	if err != nil || seconds <= 0 || seconds > math.MaxUint32 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return nil, 0, fmt.Errorf("视频时长无效")
	}
	thumb, err := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-protocol_whitelist", "file,pipe", "-f", "mov", "-enable_drefs", "0", "-i", path, "-frames:v", "1", "-vf", "scale=320:-2", "-f", "image2pipe", "-c:v", "mjpeg", "pipe:1").Output()
	if err != nil || len(thumb) == 0 || len(thumb) > 1<<20 {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, fmt.Errorf("视频封面生成失败")
	}
	return thumb, uint32(math.Ceil(seconds)), nil
}

func videoTool(env, fallback string) (string, error) {
	name := os.Getenv(env)
	if name == "" {
		name = fallback
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("缺少 %s，请安装或配置 %s", fallback, env)
	}
	return path, nil
}
