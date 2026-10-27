// 文件名：message_core.go
// 主要功能：适配 core 消息接口，包括表情资源引用与原文件上传。
// 作者：2guan
// Package messageapi 提供消息服务的远程实现（通过 gRPC 调用 core）。
package messageapi

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	baseapi "github.com/we-clay/we-clay/gateway/internal/api/base"
	"google.golang.org/protobuf/proto"
	"io"
	"strconv"
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/api"
)

// remote 消息服务 gRPC 实现
type remote struct{ client *api.Client }

func New(client *api.Client) MessageService { return &remote{client: client} }
func (w remote) core() *api.Client {
	if w.client != nil {
		return w.client
	}
	return api.GetCore()
}

// Get 获取 MessageService 单例（gRPC 模式）
var Get = sync.OnceValue(func() MessageService {
	return &remote{}
})

// Sync 同步消息
func (w remote) Sync(selector uint32) (*SyncResult, error) {
	var resp SyncResult
	if err := w.core().Get("/api/message/sync").Query("selector", selector).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendText 发送文本消息
func (w remote) SendText(receiver, content, remind string) (*SendMessageResponse, error) {
	var resp SendMessageResponse
	body := map[string]any{
		"receiver": receiver,
		"content":  content,
		"remind":   remind,
	}
	if err := w.core().Post("/api/message/text").Body(body).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendImage 发送图片消息（multipart/form-data）
func (w remote) SendImage(receiver string, reader io.Reader) (*UploadImageResponse, error) {
	imageData, err := prepareImageUpload(reader)
	if err != nil {
		return nil, err
	}
	var resp UploadImageResponse
	if err := w.core().Post("/api/message/image").Multipart(
		map[string][]byte{"image": imageData},
		map[string]string{"receiver": receiver},
	).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendVideo 发送视频消息（multipart/form-data）
func (w remote) SendVideo(receiver string, thumb, video io.Reader, duration uint32) (*UploadVideoResponse, error) {
	videoData, err := io.ReadAll(video)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{"video": videoData}
	if thumb != nil {
		thumbData, err := io.ReadAll(thumb)
		if err != nil {
			return nil, err
		}
		files["thumb"] = thumbData
	}
	var resp UploadVideoResponse
	if err := w.core().Post("/api/message/video").Multipart(files,
		map[string]string{
			"receiver": receiver,
			"duration": fmt.Sprintf("%d", duration),
		},
	).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendVoice 发送语音消息（multipart/form-data）
func (w remote) SendVoice(receiver string, reader io.Reader, duration, format int32) (*UploadVoiceResponse, error) {
	voiceData, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var resp UploadVoiceResponse
	if err := w.core().Post("/api/message/voice").Multipart(
		map[string][]byte{"voice": voiceData},
		map[string]string{
			"receiver": receiver,
			"duration": fmt.Sprintf("%d", duration),
			"format":   fmt.Sprintf("%d", format),
		},
	).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendEmoji 发送表情消息（multipart/form-data）
func (w remote) SendEmoji(receiver, md5 string, data []byte) (*UploadEmojiResponse, error) {
	var resp UploadEmojiResponse
	files := map[string][]byte{}
	if len(data) > 0 {
		files["emoji"] = data
	}
	if err := w.core().Post("/api/message/emoji").Multipart(
		files,
		map[string]string{"receiver": receiver, "md5": md5},
	).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendApp 发送应用消息
func (w remote) SendApp(receiver, xml, extendXML string, typ int32) (*SendAppMessageResponse, error) {
	var resp SendAppMessageResponse
	if err := w.core().Post("/api/message/app").Body(map[string]any{
		"receiver":   receiver,
		"xml":        xml,
		"type":       typ,
		"extend_xml": extendXML,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendLink 发送链接消息
func (w remote) SendLink(receiver, title, desc, url, thumbUrl string) (*SendAppMessageResponse, error) {
	var resp SendAppMessageResponse
	if err := w.core().Post("/api/message/link").Body(map[string]any{
		"receiver":  receiver,
		"title":     title,
		"desc":      desc,
		"url":       url,
		"thumb_url": thumbUrl,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendCard 发送名片消息
func (w remote) SendCard(receiver, cardUsername, cardNickname, cardAlias string) (*SendMessageResponse, error) {
	var resp SendMessageResponse
	if err := w.core().Post("/api/message/card").Body(map[string]any{
		"receiver":      receiver,
		"card_username": cardUsername,
		"card_nickname": cardNickname,
		"card_alias":    cardAlias,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendPosition 发送位置消息
func (w remote) SendPosition(receiver, label, poiName string, lon, lat, scale float64) (*SendMessageResponse, error) {
	var resp SendMessageResponse
	if err := w.core().Post("/api/message/position").Body(map[string]any{
		"receiver": receiver,
		"label":    label,
		"poi_name": poiName,
		"lon":      lon,
		"lat":      lat,
		"scale":    scale,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ForwardImage 转发 CDN 图片
func (w remote) ForwardImage(receiver string, reader io.Reader) (*UploadImageResponse, error) {
	b, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 4<<20 {
		return nil, errors.New("forward XML exceeds 4 MiB")
	}
	var resp UploadImageResponse
	if err = w.core().Post("/api/message/forward/image").Body(map[string]any{"receiver": receiver, "xml": string(b)}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ForwardVideo 转发 CDN 视频
func (w remote) ForwardVideo(receiver string, reader io.Reader) (*UploadVideoResponse, error) {
	b, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 4<<20 {
		return nil, errors.New("forward XML exceeds 4 MiB")
	}
	var resp UploadVideoResponse
	if err = w.core().Post("/api/message/forward/video").Body(map[string]any{"receiver": receiver, "xml": string(b)}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ForwardFile 转发文件
func (w remote) ForwardFile(receiver, xml string) (*SendAppMessageResponse, error) {
	var resp SendAppMessageResponse
	if err := w.core().Post("/api/message/forward/file").Body(map[string]any{"receiver": receiver, "xml": xml}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Revoke 撤回消息
func (w remote) Revoke(receiver string, newMsgId, clientMsgId, timestamp uint64) (*RevokeMessageResponse, error) {
	if receiver == "" || newMsgId == 0 || clientMsgId == 0 || timestamp == 0 {
		return nil, errors.New("撤回需要原始 receiver、new_id、client_id 和 create_time")
	}
	var resp RevokeMessageResponse
	if err := w.core().Post("/api/message/revoke").Body(map[string]any{
		"receiver":    receiver,
		"new_id":      newMsgId,
		"client_id":   clientMsgId,
		"create_time": timestamp,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (w remote) RevokeWithClientID(receiver string, newMsgId uint64, clientMsgId string, timestamp uint64) (*RevokeMessageResponse, error) {
	if receiver == "" || newMsgId == 0 || clientMsgId == "" || timestamp == 0 {
		return nil, errors.New("撤回需要原始 receiver、new_id、string_client_id 和 create_time")
	}
	var resp RevokeMessageResponse
	if err := w.core().Post("/api/message/revoke").Body(map[string]any{
		"receiver":         receiver,
		"new_id":           newMsgId,
		"string_client_id": clientMsgId,
		"create_time":      timestamp,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Pat 通过 core 的 Invoke 发送一次拍一拍；网络结果不明时不自动重试。
func (w remote) Pat(receiver, pattedUsername string) (*SendPatResponse, error) {
	if receiver == "" || pattedUsername == "" {
		return nil, errors.New("拍一拍需要会话 ID 和被拍者微信 ID")
	}
	var resp SendPatResponse
	if err := w.core().Post("/api/message/pat").Body(map[string]any{
		"receiver": receiver, "patted_username": pattedUsername, "experimental": true,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DownloadImg 下载消息图片
func (w remote) DownloadImg(receiver, fileId, aesKey string, totalSize uint32) (*DownloadImageResponse, error) {
	b, err := w.core().Get("/api/cdn/download/image").Query("id", fileId, "key", aesKey).DoBinary()
	if err != nil {
		return nil, err
	}
	n := uint32(len(b))
	return &DownloadImageResponse{Size: &n, Offset: proto.Uint32(0), ChunkSize: &n, Chunk: &baseapi.Buffer{Size: &n, Data: b}}, nil
}

// DownloadVideo 下载视频
func (w remote) DownloadVideo(receiver, fileId, aesKey string, totalSize uint32) (*DownloadVideoResponse, error) {
	b, err := w.core().Get("/api/cdn/download/video").Query("id", fileId, "key", aesKey).DoBinary()
	if err != nil {
		return nil, err
	}
	n := uint32(len(b))
	return &DownloadVideoResponse{Size: &n, Offset: proto.Uint32(0), Chunk: &baseapi.Buffer{Size: &n, Data: b}}, nil
}

// DownloadVoice 下载语音
func (w remote) DownloadVoice(receiver, fileId, aesKey string, totalSize, voiceLength uint32) (*DownloadVoiceResponse, error) {
	id, err := strconv.ParseUint(fileId, 10, 64)
	if err != nil {
		return nil, errors.New("voice fileId must be a numeric message ID")
	}
	var resp DownloadVoiceResponse
	if err = w.core().Post("/api/message/download/voice").Body(map[string]any{"new_id": id, "chatroom_id": receiver, "length": totalSize}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

const (
	maxAttachmentDownloadSize = 1 << 30
	maxCDNCiphertextSize      = maxAttachmentDownloadSize + 16 // PKCS#7 末块可比明文多 16 字节
	maxAttachmentChunkSize    = 64 << 10
	maxCDNAttachmentChunkSize = 256 << 10
)

// CDNAttachmentChunkLimit 只为已确认的 type7 大文件 ID 放宽至 256 KiB。
// 其他 CDN ID 继续使用普通附件的 64 KiB 上限。
func CDNAttachmentChunkLimit(fileID string) uint32 {
	if len(fileID) != 228 {
		return maxAttachmentChunkSize
	}
	decoded, err := hex.DecodeString(fileID)
	if err != nil || len(decoded) != 114 || decoded[0] != 0x7f {
		return maxAttachmentChunkSize
	}
	return maxCDNAttachmentChunkSize
}

// DownloadCDNAttachmentChunk 使用独立的 CDN 分片接口。fileID 与 aesKey
// 只放在 gRPC 请求体中，不进入 URL 或日志。
func (w remote) DownloadCDNAttachmentChunk(fileID, aesKey string, offset, chunkSize uint32) ([]byte, error) {
	if fileID == "" || len(fileID) > 4096 || len(aesKey) != 32 ||
		offset >= maxCDNCiphertextSize || offset%16 != 0 || chunkSize == 0 || chunkSize%16 != 0 || chunkSize > CDNAttachmentChunkLimit(fileID) ||
		uint64(offset)+uint64(chunkSize) > maxCDNCiphertextSize {
		return nil, errors.New("invalid CDN attachment identity, offset or chunk size")
	}
	if _, err := hex.DecodeString(aesKey); err != nil {
		return nil, errors.New("invalid CDN attachment AES key")
	}
	data, err := w.core().Post("/api/cdn/download/attachment/chunk").Body(map[string]any{
		"file_id": fileID, "aes_key": aesKey, "offset": offset, "chunk_size": chunkSize,
	}).DoBinary()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || uint32(len(data)) > chunkSize {
		return nil, errors.New("CDN attachment chunk size mismatch")
	}
	return data, nil
}

// DownloadFileChunk 只取一个分块，避免大附件在 gateway 内组装成一条响应。
func (w remote) DownloadFileChunk(receiver, fileId, appID string, totalSize, offset, chunkSize uint32) (*DownloadFileResponse, error) {
	if receiver == "" || fileId == "" || totalSize == 0 || totalSize > maxAttachmentDownloadSize ||
		offset >= totalSize || chunkSize == 0 || chunkSize > maxAttachmentChunkSize || chunkSize > totalSize-offset {
		return nil, errors.New("invalid attachment identity, size, offset or chunk size")
	}
	var resp DownloadFileResponse
	if err := w.core().Post("/api/message/download/file").Body(map[string]any{
		"app_id": appID, "attach_id": fileId, "username": receiver, "size": totalSize, "offset": offset, "chunk_size": chunkSize,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	chunk := resp.GetChunk().GetData()
	if resp.GetBaseResponse() != nil && resp.GetBaseResponse().GetCode() != 0 {
		return nil, fmt.Errorf("attachment download rejected with code %d", resp.GetBaseResponse().GetCode())
	}
	if resp.GetChunk() == nil || resp.GetOffset() != offset || resp.GetSize() != totalSize || len(chunk) == 0 ||
		uint32(len(chunk)) > chunkSize || resp.GetChunkSize() != uint32(len(chunk)) ||
		(resp.GetChunk().Size != nil && resp.GetChunk().GetSize() != uint32(len(chunk))) ||
		(resp.GetMediaId() != "" && resp.GetMediaId() != fileId) {
		return nil, errors.New("attachment chunk receipt mismatch")
	}
	return &resp, nil
}

// DownloadFile 保留小附件整块响应，SDK 下载改用 DownloadFileChunk 流式读取。
func (w remote) DownloadFile(receiver, fileId, aesKey string, totalSize uint32) (*DownloadFileResponse, error) {
	if aesKey != "" {
		return nil, errors.New("attachment download requires an attach ID, not a CDN AES key")
	}
	if fileId == "" || totalSize == 0 || totalSize > 32<<20 {
		return nil, errors.New("attachment must contain 1..32 MiB and have an attach ID")
	}
	data := make([]byte, 0, totalSize)
	var last *DownloadFileResponse
	for offset := uint32(0); offset < totalSize; {
		length := min(totalSize-offset, uint32(32<<10))
		resp, err := w.DownloadFileChunk(receiver, fileId, "", totalSize, offset, length)
		if err != nil {
			return nil, err
		}
		chunk := resp.GetChunk().GetData()
		data = append(data, chunk...)
		offset += uint32(len(chunk))
		last = resp
	}
	last.Offset = proto.Uint32(0)
	last.ChunkSize = proto.Uint32(uint32(len(data)))
	last.Chunk = &baseapi.Buffer{Size: proto.Uint32(uint32(len(data))), Data: data}
	return last, nil
}

// SendFile preserves the original filename in the multipart upload.
const MaxFileSendBytes = 32 << 20 // 与当前 core 普通附件接口保持一致。

func (w remote) SendFile(receiver, filename string, reader io.Reader) (*SendAppMessageResponse, error) {
	return w.sendFile(nil, receiver, filename, reader)
}

// SendFileContext 供进程内文件上传使用；不在 JSON 中携带 Base64。
func SendFileContext(ctx context.Context, receiver, filename string, reader io.Reader) (*SendAppMessageResponse, error) {
	return (remote{}).sendFile(ctx, receiver, filename, reader)
}

func (w remote) sendFile(ctx context.Context, receiver, filename string, reader io.Reader) (*SendAppMessageResponse, error) {
	b, err := io.ReadAll(io.LimitReader(reader, MaxFileSendBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) == 0 || len(b) > MaxFileSendBytes {
		return nil, errors.New("file must contain 1..32 MiB")
	}
	var resp SendAppMessageResponse
	if err = w.core().Post("/api/message/file").WithContext(ctx).MultipartFiles([]api.FilePart{{Name: "file", Filename: filename, Data: b}}, map[string]string{"receiver": receiver}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
