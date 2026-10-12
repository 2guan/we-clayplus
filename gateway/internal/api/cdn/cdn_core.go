// Package cdnapi 提供 CDN 文件上传/下载服务的远程实现（通过 gRPC 调用 core）。
package cdnapi

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/api"
)

// remote CDN 服务 远程实现（通过 gRPC 调用 core）
type remote struct{}

// Get 获取 CDNService 单例（gRPC 模式）
var Get = sync.OnceValue(func() CDNService {
	return &remote{}
})

// UploadImage CDN 上传聊天图片
func (w remote) UploadImage(receiver string, reader io.Reader) (*UploadImageResult, error) {
	imageData, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var resp UploadImageResult
	if err := api.GetCore().Post("/api/cdn/upload/image").Multipart(
		map[string][]byte{"image": imageData},
		map[string]string{"receiver": receiver},
	).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UploadMomentsImage CDN 上传朋友圈图片
func (w remote) UploadMomentsImage(imageData []byte) (*UploadSnsImageResult, error) {
	var resp UploadSnsImageResult
	if err := api.GetCore().Post("/api/cdn/upload/moments/image").Multipart(
		map[string][]byte{"image": imageData},
		nil,
	).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UploadVideo CDN 上传聊天视频
func (w remote) UploadVideo(receiver string, thumb []byte, reader io.Reader, duration uint32) (*UploadVideoResult, error) {
	videoData, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var resp UploadVideoResult
	if err := api.GetCore().Post("/api/cdn/upload/video").Multipart(
		map[string][]byte{"video": videoData, "thumb": thumb},
		map[string]string{"receiver": receiver, "duration": fmt.Sprintf("%d", duration)},
	).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UploadMomentsVideo CDN 上传朋友圈视频
func (w remote) UploadMomentsVideo(videoData, thumbData []byte) (*UploadSnsVideoResult, error) {
	var resp UploadSnsVideoResult
	if err := api.GetCore().Post("/api/cdn/upload/moments/video").Multipart(
		map[string][]byte{"video": videoData, "thumb": thumbData},
		nil,
	).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DownloadImage CDN 下载高清图片（返回 ReadCloser 流）
func (w remote) DownloadImage(fileID, fileAesKey string) (io.ReadCloser, error) {
	raw, err := api.GetCore().Get("/api/cdn/download/image").Query("id", fileID, "key", fileAesKey).Do()
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}

// DownloadVideoCover CDN 下载视频封面
func (w remote) DownloadVideoCover(fileID, fileAesKey string) ([]byte, error) {
	return api.GetCore().Get("/api/cdn/download/video/cover").Query("id", fileID, "key", fileAesKey).Do()
}

// DownloadVideo CDN 下载聊天视频（返回 ReadCloser 流）
func (w remote) DownloadVideo(fileID, fileAesKey string) (io.ReadCloser, error) {
	raw, err := api.GetCore().Get("/api/cdn/download/video").Query("id", fileID, "key", fileAesKey).Do()
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}

// DownloadSnsVideo CDN 下载朋友圈视频
func (w remote) DownloadSnsVideo(videoURL string, encKey uint64) ([]byte, error) {
	return api.GetCore().Get("/api/cdn/download/moments/video").Query("url", videoURL, "key", encKey).Do()
}
