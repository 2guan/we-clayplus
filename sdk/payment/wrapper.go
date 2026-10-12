package payment

import "context"

// Client 实现 Ability 接口，通过 gRPC 调用远程支付服务。
type Client struct {
	Client PaymentServiceClient
}

var _ Ability = (*Client)(nil)

// GeneratePayQCode 生成支付收款二维码
func (c Client) GeneratePayQCode() (*F2FQrcodeResponse, error) {
	resp, err := c.Client.GeneratePayQCode(context.Background(), &GeneratePayQCode_Request{})
	if resp == nil || err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// GetBandCardList 获取已绑定银行卡及余额
func (c Client) GetBandCardList() (*TenPayResponse, error) {
	resp, err := c.Client.GetBandCardList(context.Background(), &GetBandCardList_Request{})
	if resp == nil || err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// CreateHongBao 创建红包
func (c Client) CreateHongBao(params CreateHongBaoParams) (*HongBaoResponse, error) {
	resp, err := c.Client.CreateHongBao(context.Background(), &CreateHongBao_Request{
		HbType:   int32(params.HBType),
		Username: params.Username,
		InWay:    int32(params.InWay),
		Count:    int32(params.Count),
		Amount:   int32(params.Amount),
		Wishing:  params.Wishing,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// ReceiveHongBao 接收红包
func (c Client) ReceiveHongBao(params ReceiveHongBaoParams) (*HongBaoResponse, error) {
	resp, err := c.Client.ReceiveHongBao(context.Background(), &ReceiveHongBao_Request{
		NativeUrl: params.NativeURL,
		InWay:     int32(params.InWay),
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// OpenHongBao 打开红包
func (c Client) OpenHongBao(params OpenHongBaoParams) (*HongBaoResponse, error) {
	resp, err := c.Client.OpenHongBao(context.Background(), &OpenHongBao_Request{
		NativeUrl:        params.NativeURL,
		TimingIdentifier: params.TimingIdentifier,
		SendUsername:     params.SendUserName,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// GrabHongBao 抢红包
func (c Client) GrabHongBao(params GrabHongBaoParams) (*HongBaoResponse, error) {
	resp, err := c.Client.GrabHongBao(context.Background(), &GrabHongBao_Request{
		NativeUrl: params.NativeURL,
		InWay:     int32(params.InWay),
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// QueryHongBaoDetail 查询红包领取详情
func (c Client) QueryHongBaoDetail(params QueryHongBaoDetailParams) (*HongBaoResponse, error) {
	resp, err := c.Client.QueryHongBaoDetail(context.Background(), &QueryHongBaoDetail_Request{
		NativeUrl:    params.NativeURL,
		SendUsername: params.SendUserName,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// QueryHongBaoList 查询红包领取列表
func (c Client) QueryHongBaoList(params QueryHongBaoListParams) (*HongBaoResponse, error) {
	resp, err := c.Client.QueryHongBaoList(context.Background(), &QueryHongBaoList_Request{
		NativeUrl:    params.NativeURL,
		SendUsername: params.SendUserName,
		Offset:       int32(params.Offset),
		Limit:        int32(params.Limit),
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// CreatePreTransfer 创建转账
func (c Client) CreatePreTransfer(params CreatePreTransferParams) (*TenPayResponse, error) {
	resp, err := c.Client.CreatePreTransfer(context.Background(), &CreatePreTransfer_Request{
		ToUsername:  params.ToUserName,
		Fee:         params.Fee,
		Description: params.Description,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// ConfirmPreTransfer 确认转账
func (c Client) ConfirmPreTransfer(params ConfirmPreTransferParams) (*TenPayResponse, error) {
	resp, err := c.Client.ConfirmPreTransfer(context.Background(), &ConfirmPreTransfer_Request{
		BankType:    params.BankType,
		BankSerial:  params.BankSerial,
		ReqKey:      params.ReqKey,
		PayPassword: params.PayPassword,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// CollectMoney 确认收款
func (c Client) CollectMoney(params CollectMoneyParams) (*TenPayResponse, error) {
	resp, err := c.Client.CollectMoney(context.Background(), &CollectMoney_Request{
		InvalidTime:   params.InvalidTime,
		TransferId:    params.TransferID,
		TransactionId: params.TransactionID,
		ToUsername:    params.ToUserName,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// Server 实现 PaymentServiceServer 接口，将 gRPC 请求委托给 Ability 实现。
type Server struct {
	UnimplementedPaymentServiceServer
	Impl Ability
}

// GeneratePayQCode 生成支付收款二维码
func (s Server) GeneratePayQCode(ctx context.Context, request *GeneratePayQCode_Request) (*GeneratePayQCode_Response, error) {
	value, err := s.Impl.GeneratePayQCode()
	if err != nil {
		return nil, err
	}
	return &GeneratePayQCode_Response{Value: value}, nil
}

// GetBandCardList 获取已绑定银行卡及余额
func (s Server) GetBandCardList(ctx context.Context, request *GetBandCardList_Request) (*GetBandCardList_Response, error) {
	value, err := s.Impl.GetBandCardList()
	if err != nil {
		return nil, err
	}
	return &GetBandCardList_Response{Value: value}, nil
}

// CreateHongBao 创建红包
func (s Server) CreateHongBao(ctx context.Context, request *CreateHongBao_Request) (*CreateHongBao_Response, error) {
	value, err := s.Impl.CreateHongBao(CreateHongBaoParams{
		HBType:   int(request.HbType),
		Username: request.Username,
		InWay:    int(request.InWay),
		Count:    int(request.Count),
		Amount:   int(request.Amount),
		Wishing:  request.Wishing,
	})
	if err != nil {
		return nil, err
	}
	return &CreateHongBao_Response{Value: value}, nil
}

// ReceiveHongBao 接收红包
func (s Server) ReceiveHongBao(ctx context.Context, request *ReceiveHongBao_Request) (*ReceiveHongBao_Response, error) {
	value, err := s.Impl.ReceiveHongBao(ReceiveHongBaoParams{
		NativeURL: request.NativeUrl,
		InWay:     int(request.InWay),
	})
	if err != nil {
		return nil, err
	}
	return &ReceiveHongBao_Response{Value: value}, nil
}

// OpenHongBao 打开红包
func (s Server) OpenHongBao(ctx context.Context, request *OpenHongBao_Request) (*OpenHongBao_Response, error) {
	value, err := s.Impl.OpenHongBao(OpenHongBaoParams{
		NativeURL:        request.NativeUrl,
		TimingIdentifier: request.TimingIdentifier,
		SendUserName:     request.SendUsername,
	})
	if err != nil {
		return nil, err
	}
	return &OpenHongBao_Response{Value: value}, nil
}

// GrabHongBao 抢红包
func (s Server) GrabHongBao(ctx context.Context, request *GrabHongBao_Request) (*GrabHongBao_Response, error) {
	value, err := s.Impl.GrabHongBao(GrabHongBaoParams{
		NativeURL: request.NativeUrl,
		InWay:     int(request.InWay),
	})
	if err != nil {
		return nil, err
	}
	return &GrabHongBao_Response{Value: value}, nil
}

// QueryHongBaoDetail 查询红包领取详情
func (s Server) QueryHongBaoDetail(ctx context.Context, request *QueryHongBaoDetail_Request) (*QueryHongBaoDetail_Response, error) {
	value, err := s.Impl.QueryHongBaoDetail(QueryHongBaoDetailParams{
		NativeURL:    request.NativeUrl,
		SendUserName: request.SendUsername,
	})
	if err != nil {
		return nil, err
	}
	return &QueryHongBaoDetail_Response{Value: value}, nil
}

// QueryHongBaoList 查询红包领取列表
func (s Server) QueryHongBaoList(ctx context.Context, request *QueryHongBaoList_Request) (*QueryHongBaoList_Response, error) {
	value, err := s.Impl.QueryHongBaoList(QueryHongBaoListParams{
		NativeURL:    request.NativeUrl,
		SendUserName: request.SendUsername,
		Offset:       int(request.Offset),
		Limit:        int(request.Limit),
	})
	if err != nil {
		return nil, err
	}
	return &QueryHongBaoList_Response{Value: value}, nil
}

// CreatePreTransfer 创建转账
func (s Server) CreatePreTransfer(ctx context.Context, request *CreatePreTransfer_Request) (*CreatePreTransfer_Response, error) {
	value, err := s.Impl.CreatePreTransfer(CreatePreTransferParams{
		ToUserName:  request.ToUsername,
		Fee:         request.Fee,
		Description: request.Description,
	})
	if err != nil {
		return nil, err
	}
	return &CreatePreTransfer_Response{Value: value}, nil
}

// ConfirmPreTransfer 确认转账
func (s Server) ConfirmPreTransfer(ctx context.Context, request *ConfirmPreTransfer_Request) (*ConfirmPreTransfer_Response, error) {
	value, err := s.Impl.ConfirmPreTransfer(ConfirmPreTransferParams{
		BankType:    request.BankType,
		BankSerial:  request.BankSerial,
		ReqKey:      request.ReqKey,
		PayPassword: request.PayPassword,
	})
	if err != nil {
		return nil, err
	}
	return &ConfirmPreTransfer_Response{Value: value}, nil
}

// CollectMoney 确认收款
func (s Server) CollectMoney(ctx context.Context, request *CollectMoney_Request) (*CollectMoney_Response, error) {
	value, err := s.Impl.CollectMoney(CollectMoneyParams{
		InvalidTime:   request.InvalidTime,
		TransferID:    request.TransferId,
		TransactionID: request.TransactionId,
		ToUserName:    request.ToUsername,
	})
	if err != nil {
		return nil, err
	}
	return &CollectMoney_Response{Value: value}, nil
}
