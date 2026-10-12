package moments

import "context"

// Client 实现 Ability 接口，通过 gRPC 调用远程朋友圈服务。
type Client struct {
	Client MomentsServiceClient
}

var _ Ability = (*Client)(nil)

// Timeline 获取朋友圈时间线
func (c Client) Timeline(firstPageMd5 string, maxId uint64) (*Timeline_Response, error) {
	return c.Client.Timeline(context.Background(), &Timeline_Request{
		FirstPageMd5: firstPageMd5,
		MaxId:        maxId,
	})
}

// UserPage 获取用户朋友圈主页
func (c Client) UserPage(username, firstPageMd5 string, maxId uint64) (*UserPage_Response, error) {
	return c.Client.UserPage(context.Background(), &UserPage_Request{
		Username:     username,
		FirstPageMd5: firstPageMd5,
		MaxId:        maxId,
	})
}

// Detail 获取单条朋友圈详情
func (c Client) Detail(id uint64) (*Detail_Response, error) {
	return c.Client.Detail(context.Background(), &Detail_Request{Id: id})
}

// Comment 评论朋友圈
func (c Client) Comment(id uint64, content string, typ, replyCommentId int32) (*Comment_Response, error) {
	return c.Client.Comment(context.Background(), &Comment_Request{
		Id:             id,
		Content:        content,
		Type:           typ,
		ReplyCommentId: replyCommentId,
	})
}

// Like 点赞朋友圈
func (c Client) Like(id uint64) (*Like_Response, error) {
	return c.Client.Like(context.Background(), &Like_Request{Id: id})
}

// Unlike 取消点赞
func (c Client) Unlike(id uint64) (*Unlike_Response, error) {
	return c.Client.Unlike(context.Background(), &Unlike_Request{Id: id})
}

// Delete 删除朋友圈
func (c Client) Delete(id uint64) (*Delete_Response, error) {
	return c.Client.Delete(context.Background(), &Delete_Request{Id: id})
}

// DeleteComment 删除朋友圈评论
func (c Client) DeleteComment(id uint64, commentId uint32) (*DeleteComment_Response, error) {
	return c.Client.DeleteComment(context.Background(), &DeleteComment_Request{
		Id:        id,
		CommentId: commentId,
	})
}

// Post 发布朋友圈
func (c Client) Post(content string, blacklist, withUsers []string) (*Post_Response, error) {
	return c.Client.Post(context.Background(), &Post_Request{
		Content:   content,
		Blacklist: blacklist,
		WithUsers: withUsers,
	})
}

// Upload 上传朋友圈媒体
func (c Client) Upload(data []byte) (*Upload_Response, error) {
	return c.Client.Upload(context.Background(), &Upload_Request{Data: data})
}

// Sync 同步朋友圈数据
func (c Client) Sync(key []byte) (*Sync_Response, error) {
	return c.Client.Sync(context.Background(), &Sync_Request{Key: key})
}

// SetPrivacy 设置朋友圈隐私
func (c Client) SetPrivacy(function int32, value uint32) (*SetPrivacy_Response, error) {
	return c.Client.SetPrivacy(context.Background(), &SetPrivacy_Request{
		Function: function,
		Value:    value,
	})
}

// Server 实现 MomentsServiceServer 接口，将 gRPC 请求委托给 Ability 实现。
type Server struct {
	UnimplementedMomentsServiceServer
	Impl Ability
}

// Timeline 获取朋友圈时间线
func (s Server) Timeline(ctx context.Context, request *Timeline_Request) (*Timeline_Response, error) {
	return s.Impl.Timeline(request.FirstPageMd5, request.MaxId)
}

// UserPage 获取用户朋友圈主页
func (s Server) UserPage(ctx context.Context, request *UserPage_Request) (*UserPage_Response, error) {
	return s.Impl.UserPage(request.Username, request.FirstPageMd5, request.MaxId)
}

// Detail 获取单条朋友圈详情
func (s Server) Detail(ctx context.Context, request *Detail_Request) (*Detail_Response, error) {
	return s.Impl.Detail(request.Id)
}

// Comment 评论朋友圈
func (s Server) Comment(ctx context.Context, request *Comment_Request) (*Comment_Response, error) {
	return s.Impl.Comment(request.Id, request.Content, request.Type, request.ReplyCommentId)
}

// Like 点赞朋友圈
func (s Server) Like(ctx context.Context, request *Like_Request) (*Like_Response, error) {
	return s.Impl.Like(request.Id)
}

// Unlike 取消点赞
func (s Server) Unlike(ctx context.Context, request *Unlike_Request) (*Unlike_Response, error) {
	return s.Impl.Unlike(request.Id)
}

// Delete 删除朋友圈
func (s Server) Delete(ctx context.Context, request *Delete_Request) (*Delete_Response, error) {
	return s.Impl.Delete(request.Id)
}

// DeleteComment 删除朋友圈评论
func (s Server) DeleteComment(ctx context.Context, request *DeleteComment_Request) (*DeleteComment_Response, error) {
	return s.Impl.DeleteComment(request.Id, request.CommentId)
}

// Post 发布朋友圈
func (s Server) Post(ctx context.Context, request *Post_Request) (*Post_Response, error) {
	return s.Impl.Post(request.Content, request.Blacklist, request.WithUsers)
}

// Upload 上传朋友圈媒体
func (s Server) Upload(ctx context.Context, request *Upload_Request) (*Upload_Response, error) {
	return s.Impl.Upload(request.Data)
}

// Sync 同步朋友圈数据
func (s Server) Sync(ctx context.Context, request *Sync_Request) (*Sync_Response, error) {
	return s.Impl.Sync(request.Key)
}

// SetPrivacy 设置朋友圈隐私
func (s Server) SetPrivacy(ctx context.Context, request *SetPrivacy_Request) (*SetPrivacy_Response, error) {
	return s.Impl.SetPrivacy(request.Function, request.Value)
}
