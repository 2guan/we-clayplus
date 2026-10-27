// 主要功能：兼容模型返回的 WebP 图片，上传前转换为 core 使用的 PNG。
package messageapi

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"

	_ "golang.org/x/image/webp"
)

const maxImageUpload = 32 << 20

func prepareImageUpload(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, fmt.Errorf("缺少待发送的图片")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxImageUpload+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxImageUpload {
		return nil, fmt.Errorf("图片不能为空或超过 32 MiB")
	}
	if http.DetectContentType(data) != "image/webp" {
		return data, nil // 已有 PNG、JPEG 等发送行为不变，表情使用独立入口。
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 64*1024*1024 {
		return nil, fmt.Errorf("WebP 图片无效或像素尺寸过大")
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("WebP 图片解码失败：%w", err)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		return nil, fmt.Errorf("WebP 转换 PNG 失败：%w", err)
	}
	if out.Len() > maxImageUpload {
		return nil, fmt.Errorf("WebP 转换 PNG 后超过 32 MiB，请缩小图片")
	}
	return out.Bytes(), nil
}
