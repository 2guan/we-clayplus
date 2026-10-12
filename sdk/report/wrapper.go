package report

import "context"

// Client 实现 Ability 接口，通过 gRPC 调用远程状态通知服务。
type Client struct {
	Client ReportServiceClient
}

var _ Ability = (*Client)(nil)

// StartTyping 通知对方正在输入
func (c Client) StartTyping(receiver string) error {
	_, err := c.Client.StartTyping(context.Background(), &StartTyping_Request{Receiver: receiver})
	return err
}

// StopTyping 通知对方停止输入
func (c Client) StopTyping(receiver string) error {
	_, err := c.Client.StopTyping(context.Background(), &StopTyping_Request{Receiver: receiver})
	return err
}

// ReadMessage 通知对方消息已读
func (c Client) ReadMessage(receiver string) error {
	_, err := c.Client.ReadMessage(context.Background(), &ReadMessage_Request{Receiver: receiver})
	return err
}

// Server 实现 ReportServiceServer 接口，将 gRPC 请求委托给 Ability 实现。
type Server struct {
	UnimplementedReportServiceServer
	Impl Ability
}

// StartTyping 通知对方正在输入
func (s Server) StartTyping(ctx context.Context, request *StartTyping_Request) (*StartTyping_Response, error) {
	if err := s.Impl.StartTyping(request.Receiver); err != nil {
		return nil, err
	}
	return &StartTyping_Response{}, nil
}

// StopTyping 通知对方停止输入
func (s Server) StopTyping(ctx context.Context, request *StopTyping_Request) (*StopTyping_Response, error) {
	if err := s.Impl.StopTyping(request.Receiver); err != nil {
		return nil, err
	}
	return &StopTyping_Response{}, nil
}

// ReadMessage 通知对方消息已读
func (s Server) ReadMessage(ctx context.Context, request *ReadMessage_Request) (*ReadMessage_Response, error) {
	if err := s.Impl.ReadMessage(request.Receiver); err != nil {
		return nil, err
	}
	return &ReadMessage_Response{}, nil
}
