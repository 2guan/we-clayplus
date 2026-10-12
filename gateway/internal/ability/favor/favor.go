// Package favorability 提供收藏能力的实现。
package favorability

import (
	sdk "github.com/we-clay/we-clay/sdk/favor"

	favorapi "github.com/we-clay/we-clay/gateway/internal/api/favor"
	sdkroot "github.com/we-clay/we-clay/sdk"
)

// ability 收藏能力实现（直连型）
type ability struct {
	api favorapi.FavorService
}

func init() {
	sdk.Instance = &ability{api: favorapi.Get()}
}

// GetInfo 获取收藏容量信息
func (a ability) GetInfo() (*sdk.GetInfo_Info, error) {
	resp, err := a.api.GetInfo()
	if resp == nil || err != nil {
		return nil, err
	}
	return &sdk.GetInfo_Info{
		UsedSize:        resp.GetUsedSize(),
		TotalSize:       resp.GetTotalSize(),
		MaxFileSize:     resp.GetMaxFileSize(),
		MaxAutoUpload:   resp.GetMaxAutoUploadSize(),
		MaxAutoDownload: resp.GetMaxAutoDownloadSize(),
	}, nil
}

// GetItem 获取收藏项详情
func (a ability) GetItem(favId int32) ([]*sdk.Item, error) {
	return nil, sdkroot.NotImplemented(
		"favor.GetItem")
}

// Delete 删除收藏项
func (a ability) Delete(favId int32) error {
	return sdkroot.NotImplemented("favor.Delete")
}

// Sync 同步收藏列表
func (a ability) Sync(key []byte) (*sdk.Sync_Response, error) {
	resp, err := a.api.Sync(key)
	if resp == nil || err != nil {
		return nil, err
	}
	result := sdk.Sync_Response{
		Items:   make([]*sdk.Item, 0, len(resp.Items)),
		Key:     resp.GetKey(),
		HasMore: resp.GetHasMore(),
	}
	for _, item := range resp.Items {
		result.Items = append(result.Items, mapSyncItem(item))
	}
	return &result, nil
}

func mapItem(item *favorapi.FavorItem) *sdk.Item {
	if item == nil {
		return nil
	}
	return &sdk.Item{
		Id:         item.GetId(),
		Status:     item.GetStatus(),
		Object:     item.GetObject(),
		Flag:       item.GetFlag(),
		UpdateTime: item.GetUpdateTime(),
		UpdateSeq:  item.GetUpdateSequence(),
	}
}

func mapSyncItem(item *favorapi.SyncFavorItem) *sdk.Item {
	if item == nil {
		return nil
	}
	return &sdk.Item{
		Id:     item.GetType(),
		Object: item.GetObject(),
	}
}
