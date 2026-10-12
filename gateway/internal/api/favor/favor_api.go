// Package favorapi 提供收藏服务的 API 接口定义。
package favorapi

// FavorService 收藏服务 API 接口（返回 API proto 类型）
type FavorService interface {
	// GetInfo 获取收藏容量信息
	GetInfo() (*GetInfoResponse, error)
	// GetItem 获取收藏项详情
	GetItem(favId int32) (*GetFavItemResponse, error)
	// BatchGetItems 批量获取收藏项
	BatchGetItems(favIds []int32) (*BatchGetFavItemsResponse, error)
	// Delete 删除收藏项
	Delete(favId int32) (*DeleteFavItemResponse, error)
	// BatchDelete 批量删除收藏项
	BatchDelete(favIds []int32) (*BatchDeleteFavItemsResponse, error)
	// Sync 同步收藏列表（key 为空时从头同步，否则增量同步）
	Sync(key []byte) (*SyncFavorResponse, error)
}
