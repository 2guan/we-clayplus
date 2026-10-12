// Package labelapi 提供标签服务的 API 接口定义。
package labelapi

// LabelService 标签服务 API 接口（返回 API proto 类型）
type LabelService interface {
	// List 获取标签列表
	List() (*ListLabelsResponse, error)
	// Add 添加标签
	Add(name string) (*AddLabelResponse, error)
	// Delete 删除标签
	Delete(labelIds string) (*OperateResponse, error)
	// Update 更新标签名称
	Update(labelId uint32, name string) (*OperateResponse, error)
	// ModifyContactLabels 修改联系人标签
	ModifyContactLabels(usernames []string, labelIds string) (*OperateResponse, error)
}
