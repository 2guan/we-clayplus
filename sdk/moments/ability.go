package moments

// Ability 朋友圈能力接口（供插件嵌入使用）
type Ability interface {
	// Timeline 获取朋友圈时间线
	Timeline(firstPageMd5 string, maxId uint64) (*Timeline_Response, error)
	// UserPage 获取用户朋友圈主页
	UserPage(username, firstPageMd5 string, maxId uint64) (*UserPage_Response, error)
	// Detail 获取单条朋友圈详情
	Detail(id uint64) (*Detail_Response, error)
	// Comment 评论朋友圈
	Comment(id uint64, content string, typ, replyCommentId int32) (*Comment_Response, error)
	// Like 点赞朋友圈
	Like(id uint64) (*Like_Response, error)
	// Unlike 取消点赞
	Unlike(id uint64) (*Unlike_Response, error)
	// Delete 删除朋友圈
	Delete(id uint64) (*Delete_Response, error)
	// DeleteComment 删除朋友圈评论
	DeleteComment(id uint64, commentId uint32) (*DeleteComment_Response, error)
	// Post 发布朋友圈
	Post(content string, blacklist, withUsers []string) (*Post_Response, error)
	// Upload 上传朋友圈媒体
	Upload(data []byte) (*Upload_Response, error)
	// Sync 同步朋友圈数据
	Sync(key []byte) (*Sync_Response, error)
	// SetPrivacy 设置朋友圈隐私
	SetPrivacy(function int32, value uint32) (*SetPrivacy_Response, error)
}

// Instance 朋友圈能力实例（由 gateway/internal/ability 层注入）
var Instance Ability
