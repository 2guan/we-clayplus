// Package momentsapi 提供朋友圈服务的远程实现（通过 gRPC 调用 core）。
package momentsapi

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/api"
	baseapi "github.com/we-clay/we-clay/gateway/internal/api/base"
)

// remote 朋友圈服务 远程实现（通过 gRPC 调用 core）。
type remote struct{}

// Get 获取 MomentsService 单例（gRPC 模式）。
var Get = sync.OnceValue(func() MomentsService {
	return &remote{}
})

// Timeline 获取朋友圈时间线
func (w remote) Timeline(firstPageMd5 string, maxId uint64) (*TimelineResponse, error) {
	var resp TimelineResponse
	if err := api.GetCore().Get("/api/moments/timeline").
		Query("first_page_md5", firstPageMd5, "max_id", maxId).
		DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UserPage 获取用户朋友圈主页
func (w remote) UserPage(username, firstPageMd5 string, maxId uint64) (*UserPageResponse, error) {
	var resp UserPageResponse
	if err := api.GetCore().Get(fmt.Sprintf("/api/moments/user/%s", url.PathEscape(username))).
		Query("first_page_md5", firstPageMd5, "max_id", maxId).
		DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Detail 获取单条朋友圈详情
func (w remote) Detail(id uint64) (*DetailResponse, error) {
	var resp DetailResponse
	if err := api.GetCore().Get(fmt.Sprintf("/api/moments/%d", id)).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Comment 评论朋友圈
func (w remote) Comment(id uint64, content string, typ, replyCommentId int32) (*CommentResponse, error) {
	var resp CommentResponse
	if err := api.GetCore().Post(fmt.Sprintf("/api/moments/comment/%d", id)).Body(map[string]any{
		"content":          content,
		"type":             typ,
		"reply_comment_id": replyCommentId,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Like 点赞朋友圈
func (w remote) Like(id uint64) (*CommentResponse, error) {
	var resp CommentResponse
	if err := api.GetCore().Post(fmt.Sprintf("/api/moments/like/%d", id)).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Unlike 取消点赞
func (w remote) Unlike(id uint64) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Delete(fmt.Sprintf("/api/moments/like/%d", id)).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Delete 删除朋友圈
func (w remote) Delete(id uint64) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Delete(fmt.Sprintf("/api/moments/%d", id)).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteComment 删除朋友圈评论
func (w remote) DeleteComment(id uint64, commentId uint32) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Delete(fmt.Sprintf("/api/moments/comment/%d", id)).
		Query("comment_id", commentId).
		DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Post 发布朋友圈
func (w remote) Post(content string, blacklist, withUsers []string) (*PostResponse, error) {
	var resp PostResponse
	if err := api.GetCore().Post("/api/moments").Body(map[string]any{
		"content":    content,
		"blacklist":  blacklist,
		"with_users": withUsers,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Upload 上传朋友圈媒体
func (w remote) Upload(reader io.Reader) (*UploadResponse, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var resp UploadResponse
	if err := api.GetCore().Post("/api/moments/upload").Multipart(
		map[string][]byte{"media": data},
		nil,
	).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Sync 同步朋友圈数据
func (w remote) Sync(syncKey []byte) (*SyncResponse, error) {
	var resp SyncResponse
	if err := api.GetCore().Post("/api/moments/sync").
		Query("key", base64.StdEncoding.EncodeToString(syncKey)).
		DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetPrivacy 设置朋友圈隐私
func (w remote) SetPrivacy(function int32, value uint32) (*baseapi.OperateResponse, error) {
	var resp baseapi.OperateResponse
	if err := api.GetCore().Put("/api/moments/privacy").Body(map[string]any{
		"function": function,
		"value":    value,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
