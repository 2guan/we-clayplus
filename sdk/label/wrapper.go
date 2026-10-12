package label

import "context"

// Client 实现 Ability 接口，通过 gRPC 调用远程标签服务
type Client struct {
	Client LabelServiceClient
}

var _ Ability = (*Client)(nil)

// List 获取标签列表
func (c Client) List() ([]*LabelPair, error) {
	resp, err := c.Client.List(context.Background(), &List_Request{})
	if err != nil {
		return nil, err
	}
	return resp.Labels, nil
}

// Add 添加标签
func (c Client) Add(name string) (*LabelPair, error) {
	resp, err := c.Client.Add(context.Background(), &Add_Request{Name: name})
	if err != nil {
		return nil, err
	}
	return resp.Label, nil
}

// Delete 删除标签
func (c Client) Delete(labelIds string) (*Delete_Response, error) {
	return c.Client.Delete(context.Background(), &Delete_Request{LabelIds: labelIds})
}

// Update 更新标签名称
func (c Client) Update(labelId uint32, name string) (*Update_Response, error) {
	return c.Client.Update(context.Background(), &Update_Request{
		LabelId: labelId,
		Name:    name,
	})
}

// ModifyContactLabels 修改联系人标签
func (c Client) ModifyContactLabels(usernames []string, labelIds string) (*ModifyContact_Response, error) {
	return c.Client.ModifyContactLabels(context.Background(), &ModifyContact_Request{
		Usernames: usernames,
		LabelIds:  labelIds,
	})
}

// Server 实现 LabelServiceServer 接口，将 gRPC 请求委托给 Ability 实现
type Server struct {
	UnimplementedLabelServiceServer
	Impl Ability
}

// List 获取标签列表
func (s Server) List(ctx context.Context, request *List_Request) (*List_Response, error) {
	labels, err := s.Impl.List()
	if err != nil {
		return nil, err
	}
	return &List_Response{Labels: labels}, nil
}

// Add 添加标签
func (s Server) Add(ctx context.Context, request *Add_Request) (*Add_Response, error) {
	label, err := s.Impl.Add(request.Name)
	if err != nil {
		return nil, err
	}
	return &Add_Response{Label: label}, nil
}

// Delete 删除标签
func (s Server) Delete(ctx context.Context, request *Delete_Request) (*Delete_Response, error) {
	return s.Impl.Delete(request.LabelIds)
}

// Update 更新标签名称
func (s Server) Update(ctx context.Context, request *Update_Request) (*Update_Response, error) {
	return s.Impl.Update(request.LabelId, request.Name)
}

// ModifyContactLabels 修改联系人标签
func (s Server) ModifyContactLabels(ctx context.Context, request *ModifyContact_Request) (*ModifyContact_Response, error) {
	return s.Impl.ModifyContactLabels(request.Usernames, request.LabelIds)
}
