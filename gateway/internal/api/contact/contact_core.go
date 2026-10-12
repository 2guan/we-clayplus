// Package contactapi 提供联系人服务的远程实现（通过 gRPC 调用 core）。
package contactapi

import (
	"fmt"
	"net/url"
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/api"
)

// remote 联系人服务 远程实现（通过 gRPC 调用 core）
type remote struct{}

// Get 获取 ContactService 单例（gRPC 模式）
var Get = sync.OnceValue(func() ContactService {
	return &remote{}
})

// List 获取联系人列表（增量同步）
func (w remote) List() ([]string, error) {
	var resp []string
	if err := api.GetCore().Get("/api/contacts").DoJson(&resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Detail 获取联系人详细信息
func (w remote) Detail(usernames []string) (*GetContactDetailResponse, error) {
	var resp GetContactDetailResponse
	if err := api.GetCore().Post("/api/contacts/detail").Body(usernames).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SetRemark 设置联系人备注
func (w remote) SetRemark(username, remark string) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Put(fmt.Sprintf("/api/contacts/remark/%s", url.PathEscape(username))).Body(map[string]any{
		"remark": remark,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Search 搜索联系人
func (w remote) Search(keyword string, fromScene, searchScene uint32) (*SearchContactResponse, error) {
	var resp SearchContactResponse
	if err := api.GetCore().Post("/api/contacts/search").Body(map[string]any{
		"keyword":      keyword,
		"from_scene":   fromScene,
		"search_scene": searchScene,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Verify 通过好友验证
func (w remote) Verify(v1, v2 string, scene int) (*VerifyUserResponse, error) {
	var resp VerifyUserResponse
	if err := api.GetCore().Post("/api/contacts/friend-requests/verify").Body(map[string]any{
		"v1":    v1,
		"v2":    v2,
		"scene": int32(scene),
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Request 发送好友申请
func (w remote) Request(v1, v2, content string, operate, scene int) (*VerifyUserResponse, error) {
	var resp VerifyUserResponse
	if err := api.GetCore().Post("/api/contacts/friend-requests").Body(map[string]any{
		"v1":      v1,
		"v2":      v2,
		"content": content,
		"operate": int32(operate),
		"scene":   int32(scene),
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BlacklistAdd 添加到黑名单
func (w remote) BlacklistAdd(username string) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Post(fmt.Sprintf("/api/contacts/blacklist/%s", url.PathEscape(username))).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BlacklistRemove 从黑名单移除
func (w remote) BlacklistRemove(username string) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Delete(fmt.Sprintf("/api/contacts/blacklist/%s", url.PathEscape(username))).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Delete 删除联系人
func (w remote) Delete(username string) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Delete(fmt.Sprintf("/api/contacts/%s", url.PathEscape(username))).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// LbsFind 附近的人
func (w remote) LbsFind(latitude, longitude float32, operate uint32) (*LbsResponse, error) {
	var resp LbsResponse
	if err := api.GetCore().Post("/api/contacts/lbs").Body(map[string]any{
		"latitude":  latitude,
		"longitude": longitude,
		"operate":   operate,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UploadContact 上传通讯录匹配好友
func (w remote) UploadContact(phones []string, currentPhone string, operate int32) (*UploadContactResponse, error) {
	var resp UploadContactResponse
	if err := api.GetCore().Post("/api/contacts/upload").Body(map[string]any{
		"phones":        phones,
		"current_phone": currentPhone,
		"operate":       operate,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
