// Package favorability 提供收藏能力的实现。
package favorability

import (
	"errors"
	"fmt"
	sdk "github.com/we-clay/we-clay/sdk/favor"

	favorapi "github.com/we-clay/we-clay/gateway/internal/api/favor"
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
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("favor 回执缺失，结果未确认")
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
	if favId <= 0 {
		return nil, errors.New("收藏 ID 必须为正数")
	}
	resp, err := a.api.GetItem(favId)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("收藏查询回执缺失，结果未确认")
	}
	items := make([]*sdk.Item, 0, len(resp.GetItems()))
	for _, item := range resp.GetItems() {
		if item == nil || item.GetId() != uint32(favId) {
			return nil, errors.New("收藏查询回执的 ID 不匹配")
		}
		items = append(items, mapItem(item))
	}
	return items, nil
}

// Delete 删除收藏项
func (a ability) Delete(favId int32) error {
	if favId <= 0 {
		return errors.New("收藏 ID 必须为正数")
	}
	resp, err := a.api.Delete(favId)
	if err != nil {
		return err
	}
	if resp == nil || len(resp.GetResults()) != 1 || resp.GetResults()[0] == nil {
		return errors.New("收藏删除回执缺失，结果未确认，请勿自动重试")
	}
	result := resp.GetResults()[0]
	if result.Code == nil {
		return errors.New("收藏删除回执缺少单项状态，结果未确认，请勿自动重试")
	}
	if result.GetId() != uint32(favId) || result.GetCode() != 0 {
		return fmt.Errorf("收藏删除未确认：ID %d，返回码 %d，请勿自动重试", result.GetId(), result.GetCode())
	}
	return nil
}

// Sync 同步收藏列表
func (a ability) Sync(key []byte) (*sdk.Sync_Response, error) {
	resp, err := a.api.Sync(key)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("favor 回执缺失，结果未确认")
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
		Type:       item.GetType(),
		Object:     item.GetObject(),
		ClientId:   item.GetClientId(),
		SourceType: item.GetSourceType(),
		SourceId:   item.GetSourceId(),
	}
}
