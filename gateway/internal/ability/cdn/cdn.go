// Package cdnability 提供 CDN 能力的实现。
package cdnability

import (
	"errors"
	"io"

	"github.com/we-clay/we-clay/gateway/internal/api"
	sdk "github.com/we-clay/we-clay/sdk/cdn"

	cdnapi "github.com/we-clay/we-clay/gateway/internal/api/cdn"
	sdkroot "github.com/we-clay/we-clay/sdk"
)

// ability CDN 能力实现
type ability struct {
	api cdnapi.CDNService
}

func init() {
	sdk.Instance = &ability{api: cdnapi.Get()}
}

// UploadImage CDN 上传聊天图片
func (a ability) UploadImage(receiver string, reader io.Reader) (*sdk.UploadImage_Response, error) {
	resp, err := a.api.UploadImage(receiver, reader)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("cdn 回执缺失，结果未确认")
	}
	var result sdk.UploadImage_Response
	if resp.GetFileId() == "" || resp.GetAesKey() == "" || resp.GetFileSize() == 0 {
		return nil, errors.New("图片上传回执缺少文件 ID、密钥或长度")
	}
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UploadMomentsImage CDN 上传朋友圈图片
func (a ability) UploadMomentsImage(imageData []byte) (*sdk.UploadMomentsImage_Response, error) {
	resp, err := a.api.UploadMomentsImage(imageData)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("cdn 回执缺失，结果未确认")
	}
	var result sdk.UploadMomentsImage_Response
	if resp.GetFileUrl() == "" || resp.GetThumbUrl() == "" {
		return nil, errors.New("朋友圈图片上传回执缺少图片或缩略图地址")
	}
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UploadVideo CDN 上传聊天视频
func (a ability) UploadVideo(receiver string, thumb []byte, reader io.Reader, duration uint32) (*sdk.UploadVideo_Response, error) {
	resp, err := a.api.UploadVideo(receiver, thumb, reader, duration)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("cdn 回执缺失，结果未确认")
	}
	var result sdk.UploadVideo_Response
	if resp.GetFileId() == "" || resp.GetAesKey() == "" || resp.GetFileSize() == 0 {
		return nil, errors.New("视频上传回执缺少文件 ID、密钥或长度")
	}
	if err := api.TransformProto(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UploadMomentsVideo CDN 上传朋友圈视频
func (a ability) UploadMomentsVideo(videoData, thumbData []byte) (*sdk.UploadMomentsVideo_Response, error) {
	return nil, sdkroot.NotImplemented("cdn.UploadMomentsVideo")
}

// DownloadImage CDN 下载高清图片
func (a ability) DownloadImage(fileID, fileAesKey string) (io.ReadCloser, error) {
	return a.api.DownloadImage(fileID, fileAesKey)
}

// DownloadVideoCover CDN 下载视频封面
func (a ability) DownloadVideoCover(fileID, fileAesKey string) ([]byte, error) {
	return a.api.DownloadVideoCover(fileID, fileAesKey)
}

// DownloadVideo CDN 下载聊天视频
func (a ability) DownloadVideo(fileID, fileAesKey string) (io.ReadCloser, error) {
	return a.api.DownloadVideo(fileID, fileAesKey)
}

// DownloadMomentsVideo CDN 下载朋友圈视频
func (a ability) DownloadMomentsVideo(videoURL string, encKey uint64) ([]byte, error) {
	return nil, sdkroot.NotImplemented("cdn.DownloadMomentsVideo")
}
