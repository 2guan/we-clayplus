package label

// Ability 标签能力接口（供插件嵌入使用）
type Ability interface {
	// List 获取标签列表
	List() ([]*LabelPair, error)
	// Add 添加标签
	Add(name string) (*LabelPair, error)
	// Delete 删除标签
	Delete(labelIds string) (*Delete_Response, error)
	// Update 更新标签名称
	Update(labelId uint32, name string) (*Update_Response, error)
	// ModifyContactLabels 修改联系人标签
	ModifyContactLabels(usernames []string, labelIds string) (*ModifyContact_Response, error)
}

// Instance 标签能力实例（由 gateway/internal/ability 层注入）
var Instance Ability
