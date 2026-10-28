// Package favorapi 提供收藏服务的远程实现（通过 gRPC 调用 core）。
package favorapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/api"
)

// remote 收藏服务 远程实现（通过 gRPC 调用 core）
type remote struct{}

// Get 获取 FavorService 单例（gRPC 模式）
var Get = sync.OnceValue(func() FavorService {
	return &remote{}
})

// GetInfo 获取收藏容量信息
func (w remote) GetInfo() (*GetInfoResponse, error) {
	var resp GetInfoResponse
	if err := api.GetCore().Get("/api/favor/info").DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetItem 获取收藏项详情
func (w remote) GetItem(favId int32) (*GetFavItemResponse, error) {
	var resp BatchGetFavItemsResponse
	if err := api.GetCore().Get(fmt.Sprintf("/api/favor/item/%d", favId)).DoProto(&resp); err != nil {
		return nil, err
	}
	return &GetFavItemResponse{Items: resp.GetObjects()}, nil
}

// BatchGetItems 通过 core 的批量协议一次查询收藏项。
func (w remote) BatchGetItems(favIds []int32) (*BatchGetFavItemsResponse, error) {
	var resp BatchGetFavItemsResponse
	if err := api.GetCore().Post("/api/favor/items/batch-get").Body(map[string]any{"ids": favIds}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Delete 删除收藏项
func (w remote) Delete(favId int32) (*DeleteFavItemResponse, error) {
	var resp BatchDeleteFavItemsResponse
	if err := api.GetCore().Delete(fmt.Sprintf("/api/favor/item/%d", favId)).DoProto(&resp); err != nil {
		return nil, err
	}
	return &DeleteFavItemResponse{Results: resp.GetResults()}, nil
}

// BatchDelete 通过 core 的批量协议一次删除收藏项。
func (w remote) BatchDelete(favIds []int32) (*BatchDeleteFavItemsResponse, error) {
	var resp BatchDeleteFavItemsResponse
	if err := api.GetCore().Post("/api/favor/items/batch-delete").Body(map[string]any{"ids": favIds}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Sync 同步收藏列表
func (w remote) Sync(key []byte) (*SyncFavorResponse, error) {
	req := api.GetCore().Post("/api/favor/sync")
	if len(key) > 0 {
		req = req.Query("key", base64.StdEncoding.EncodeToString(key))
	}
	var resp struct {
		Items   *[]*SyncFavorItem `json:"items"`
		Key     []byte            `json:"key"`
		HasMore *bool             `json:"hasMore"`
		Unknown []json.RawMessage `json:"unknown"`
	}
	if err := req.DoJson(&resp); err != nil {
		return nil, err
	}
	if resp.Items == nil || resp.HasMore == nil {
		return nil, fmt.Errorf("收藏同步回执不完整，未推进游标")
	}
	if len(resp.Unknown) != 0 {
		return nil, fmt.Errorf("收藏同步包含尚未支持的命令，未推进游标")
	}
	return &SyncFavorResponse{
		Items:   *resp.Items,
		Key:     resp.Key,
		HasMore: *resp.HasMore,
	}, nil
}
