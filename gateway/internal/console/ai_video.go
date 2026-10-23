package console

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

// 仅接受本轮选定的本机 MP4 字节，禁用网络协议和 MOV 外部文件引用。
func prepareAIVideo(ctx context.Context, data []byte) ([]byte, uint32, error) {
	if len(data) < 12 || len(data) > 16<<20 || string(data[4:8]) != "ftyp" {
		return nil, 0, fmt.Errorf("视频需为不超过16MB的MP4")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ffmpeg, e := aiVideoTool("WE_CLAY_FFMPEG", "ffmpeg")
	if e != nil {
		return nil, 0, e
	}
	probe, e := aiVideoTool("WE_CLAY_FFPROBE", "ffprobe")
	if e != nil {
		return nil, 0, e
	}
	dir, e := os.MkdirTemp("", "weclay-ai-video-")
	if e != nil {
		return nil, 0, e
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "video.mp4")
	if e = os.WriteFile(path, data, 0600); e != nil {
		return nil, 0, e
	}
	raw, e := exec.CommandContext(ctx, probe, "-v", "error", "-protocol_whitelist", "file,pipe", "-f", "mov", "-enable_drefs", "0", "-show_entries", "format=duration:stream=codec_type,width,height", "-of", "json", path).Output()
	if e != nil {
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
	seconds, e := strconv.ParseFloat(meta.Format.Duration, 64)
	if e != nil || seconds <= 0 || seconds > 600 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return nil, 0, fmt.Errorf("视频时长需在10分钟以内")
	}
	thumb, e := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-protocol_whitelist", "file,pipe", "-f", "mov", "-enable_drefs", "0", "-i", path, "-frames:v", "1", "-vf", "scale=320:-2", "-f", "image2pipe", "-c:v", "mjpeg", "pipe:1").Output()
	if e != nil || len(thumb) == 0 || len(thumb) > 1<<20 {
		return nil, 0, fmt.Errorf("视频封面生成失败")
	}
	return thumb, uint32(math.Ceil(seconds)), nil
}

func aiVideoTool(env, fallback string) (string, error) {
	name := os.Getenv(env)
	if name == "" {
		name = fallback
	}
	path, e := exec.LookPath(name)
	if e != nil {
		return "", fmt.Errorf("缺少%s，请安装或配置%s", fallback, env)
	}
	return path, nil
}
