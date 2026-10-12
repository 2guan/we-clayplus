// Package labelapi 提供标签服务的远程实现（通过 gRPC 调用 core）。
package labelapi

import (
	"fmt"
	"strings"
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/api"
)

// remote 标签服务 gRPC 实现
type remote struct{}

// Get 获取 LabelService 单例（gRPC 模式）
var Get = sync.OnceValue(func() LabelService {
	return &remote{}
})

// List 获取标签列表
func (w remote) List() (*ListLabelsResponse, error) {
	var resp ListLabelsResponse
	if err := api.GetCore().Get("/api/labels").DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Add 添加标签
func (w remote) Add(name string) (*AddLabelResponse, error) {
	var resp AddLabelResponse
	if err := api.GetCore().Post("/api/labels").Body(map[string]any{"label_name": name}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Delete 删除标签
func (w remote) Delete(labelIds string) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Delete(fmt.Sprintf("/api/labels/%s", labelIds)).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Update 更新标签名称
func (w remote) Update(labelId uint32, name string) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Put(fmt.Sprintf("/api/labels/%d", labelId)).Body(map[string]any{
		"label_name": name,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ModifyContactLabels 修改联系人标签
func (w remote) ModifyContactLabels(usernames []string, labelIds string) (*OperateResponse, error) {
	var resp OperateResponse
	if err := api.GetCore().Put(fmt.Sprintf("/api/contacts/%s/labels", strings.Join(usernames, ","))).Body(map[string]any{
		"label_ids": labelIds,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
