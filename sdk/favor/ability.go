package favor

// Ability 收藏能力接口（供插件嵌入使用）
type Ability interface {
	// GetInfo 获取收藏容量信息
	GetInfo() (*GetInfo_Info, error)
	// GetItem 获取收藏项详情
	GetItem(favId int32) ([]*Item, error)
	// Delete 删除收藏项
	Delete(favId int32) error
	// Sync 同步收藏列表（key 为空时从头同步，否则增量同步）
	Sync(key []byte) (*Sync_Response, error)
}

// Instance 收藏能力实例（由 gateway/internal/ability 层注入）
var Instance Ability
