// Package paymentapi 提供支付服务的 API 接口定义。
package paymentapi

// PaymentService 支付服务 API 接口（返回 API proto 类型）。
type PaymentService interface {
	// GeneratePayQCode 生成支付收款二维码
	GeneratePayQCode() (*F2FQrcodeResponse, error)
	// GetBandCardList 获取已绑定银行卡及余额
	GetBandCardList() (*TenPayResponse, error)
	// CreateHongBao 创建红包
	CreateHongBao(params CreateHongBaoParams) (*HongBaoResponse, error)
	// ReceiveHongBao 接收红包
	ReceiveHongBao(params ReceiveHongBaoParams) (*HongBaoResponse, error)
	// OpenHongBao 打开红包
	OpenHongBao(params OpenHongBaoParams) (*HongBaoResponse, error)
	// GrabHongBao 抢红包
	GrabHongBao(params GrabHongBaoParams) (*HongBaoResponse, error)
	// QueryHongBaoDetail 查询红包领取详情
	QueryHongBaoDetail(params QueryHongBaoDetailParams) (*HongBaoResponse, error)
	// QueryHongBaoList 查询红包领取列表
	QueryHongBaoList(params QueryHongBaoListParams) (*HongBaoResponse, error)
	// CreatePreTransfer 创建转账
	CreatePreTransfer(params CreatePreTransferParams) (*TenPayResponse, error)
	// ConfirmPreTransfer 确认转账
	ConfirmPreTransfer(params ConfirmPreTransferParams) (*TenPayResponse, error)
	// CollectMoney 确认收款
	CollectMoney(params CollectMoneyParams) (*TenPayResponse, error)
}

// CreateHongBaoParams 创建红包参数。
type CreateHongBaoParams struct {
	HBType   int
	Username string
	InWay    int
	Count    int
	Amount   int
	Wishing  string
}

// ReceiveHongBaoParams 接收红包参数。
type ReceiveHongBaoParams struct {
	NativeURL string
	InWay     int
}

// OpenHongBaoParams 打开红包参数。
type OpenHongBaoParams struct {
	NativeURL        string
	TimingIdentifier string
	SendUserName     string
}

// GrabHongBaoParams 抢红包参数。
type GrabHongBaoParams struct {
	NativeURL string
	InWay     int
}

// QueryHongBaoDetailParams 查询红包详情参数。
type QueryHongBaoDetailParams struct {
	NativeURL    string
	SendUserName string
}

// QueryHongBaoListParams 查询红包列表参数。
type QueryHongBaoListParams struct {
	NativeURL    string
	SendUserName string
	Offset       int
	Limit        int
}

// CreatePreTransferParams 创建转账参数。
type CreatePreTransferParams struct {
	ToUserName  string
	Fee         uint32
	Description string
}

// ConfirmPreTransferParams 确认转账参数。
type ConfirmPreTransferParams struct {
	BankType    string
	BankSerial  string
	ReqKey      string
	PayPassword string
}

// CollectMoneyParams 确认收款参数。
type CollectMoneyParams struct {
	InvalidTime   string
	TransferID    string
	TransactionID string
	ToUserName    string
}
