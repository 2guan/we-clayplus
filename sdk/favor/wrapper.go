package favor

import (
	"context"
	"errors"
	"fmt"
)

// Client 实现 Ability 接口，通过 gRPC 调用远程收藏服务
type Client struct {
	Client FavorServiceClient
}

var _ Ability = (*Client)(nil)

// GetInfo 获取收藏容量信息
func (c Client) GetInfo() (*GetInfo_Info, error) {
	resp, err := c.Client.GetInfo(context.Background(), &GetInfo_Request{})
	if err != nil {
		return nil, err
	}
	return resp.Info, nil
}

// GetItem 获取收藏项详情
func (c Client) GetItem(favId int32) ([]*Item, error) {
	resp, err := c.Client.GetItem(context.Background(), &GetItem_Request{FavId: favId})
	if err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// Delete 删除收藏项
func (c Client) Delete(favId int32) error {
	resp, err := c.Client.Delete(context.Background(), &Delete_Request{FavId: favId})
	if err != nil {
		return err
	}
	var s string
	for _, result := range resp.Results {
		if result.Code != 0 {
			s += fmt.Sprintf("[%d] 删除失败 ", result.FavId)
		}
	}
	if s != "" {
		return errors.New(s)
	}
	return nil
}

// Sync 同步收藏列表
func (c Client) Sync(key []byte) (*Sync_Response, error) {
	return c.Client.Sync(context.Background(), &Sync_Request{Key: key})
}

// Server 实现 FavorServiceServer 接口，将 gRPC 请求委托给 Ability 实现
type Server struct {
	UnimplementedFavorServiceServer
	Impl Ability
}

// GetInfo 获取收藏容量信息
func (s Server) GetInfo(ctx context.Context, request *GetInfo_Request) (*GetInfo_Response, error) {
	info, err := s.Impl.GetInfo()
	if err != nil {
		return nil, err
	}
	return &GetInfo_Response{Info: info}, nil
}

// GetItem 获取收藏项详情
func (s Server) GetItem(ctx context.Context, request *GetItem_Request) (*GetItem_Response, error) {
	items, err := s.Impl.GetItem(request.FavId)
	if err != nil {
		return nil, err
	}
	return &GetItem_Response{Items: items}, nil
}

// Delete 删除收藏项
func (s Server) Delete(ctx context.Context, request *Delete_Request) (*Delete_Response, error) {
	err := s.Impl.Delete(request.FavId)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

// Sync 同步收藏列表
func (s Server) Sync(ctx context.Context, request *Sync_Request) (*Sync_Response, error) {
	return s.Impl.Sync(request.Key)
}
