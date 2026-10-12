// Package paymentapi 提供支付服务的远程实现（通过 gRPC 调用 core）。
package paymentapi

import (
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/api"
)

// remote 支付服务 远程实现（通过 gRPC 调用 core）。
type remote struct{}

// Get 获取 PaymentService 单例（gRPC 模式）。
var Get = sync.OnceValue(func() PaymentService {
	return &remote{}
})

// GeneratePayQCode 生成支付收款二维码
func (w remote) GeneratePayQCode() (*F2FQrcodeResponse, error) {
	var resp F2FQrcodeResponse
	if err := api.GetCore().Get("/api/payment/qrcode").DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetBandCardList 获取已绑定银行卡及余额
func (w remote) GetBandCardList() (*TenPayResponse, error) {
	var resp TenPayResponse
	if err := api.GetCore().Get("/api/payment/cards").DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateHongBao 创建红包
func (w remote) CreateHongBao(params CreateHongBaoParams) (*HongBaoResponse, error) {
	var resp HongBaoResponse
	if err := api.GetCore().Post("/api/payment/hongbao/create").Body(map[string]any{
		"hb_type":  params.HBType,
		"username": params.Username,
		"in_way":   params.InWay,
		"count":    params.Count,
		"amount":   params.Amount,
		"wishing":  params.Wishing,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ReceiveHongBao 接收红包
func (w remote) ReceiveHongBao(params ReceiveHongBaoParams) (*HongBaoResponse, error) {
	var resp HongBaoResponse
	if err := api.GetCore().Post("/api/payment/hongbao/receive").Body(map[string]any{
		"native_url": params.NativeURL,
		"in_way":     params.InWay,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// OpenHongBao 打开红包
func (w remote) OpenHongBao(params OpenHongBaoParams) (*HongBaoResponse, error) {
	var resp HongBaoResponse
	if err := api.GetCore().Post("/api/payment/hongbao/open").Body(map[string]any{
		"native_url":        params.NativeURL,
		"timing_identifier": params.TimingIdentifier,
		"send_username":     params.SendUserName,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GrabHongBao 抢红包
func (w remote) GrabHongBao(params GrabHongBaoParams) (*HongBaoResponse, error) {
	var resp HongBaoResponse
	if err := api.GetCore().Post("/api/payment/hongbao/grab").Body(map[string]any{
		"native_url": params.NativeURL,
		"in_way":     params.InWay,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// QueryHongBaoDetail 查询红包领取详情
func (w remote) QueryHongBaoDetail(params QueryHongBaoDetailParams) (*HongBaoResponse, error) {
	var resp HongBaoResponse
	if err := api.GetCore().Post("/api/payment/hongbao/detail").Body(map[string]any{
		"native_url":    params.NativeURL,
		"send_username": params.SendUserName,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// QueryHongBaoList 查询红包领取列表
func (w remote) QueryHongBaoList(params QueryHongBaoListParams) (*HongBaoResponse, error) {
	var resp HongBaoResponse
	if err := api.GetCore().Post("/api/payment/hongbao/list").Body(map[string]any{
		"native_url":    params.NativeURL,
		"send_username": params.SendUserName,
		"offset":        params.Offset,
		"limit":         params.Limit,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreatePreTransfer 创建转账
func (w remote) CreatePreTransfer(params CreatePreTransferParams) (*TenPayResponse, error) {
	var resp TenPayResponse
	if err := api.GetCore().Post("/api/payment/transfer/create").Body(map[string]any{
		"to_username": params.ToUserName,
		"fee":         params.Fee,
		"description": params.Description,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ConfirmPreTransfer 确认转账
func (w remote) ConfirmPreTransfer(params ConfirmPreTransferParams) (*TenPayResponse, error) {
	var resp TenPayResponse
	if err := api.GetCore().Post("/api/payment/transfer/confirm").Body(map[string]any{
		"bank_type":    params.BankType,
		"bank_serial":  params.BankSerial,
		"req_key":      params.ReqKey,
		"pay_password": params.PayPassword,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// CollectMoney 确认收款
func (w remote) CollectMoney(params CollectMoneyParams) (*TenPayResponse, error) {
	var resp TenPayResponse
	if err := api.GetCore().Post("/api/payment/collect").Body(map[string]any{
		"invalid_time":   params.InvalidTime,
		"transfer_id":    params.TransferID,
		"transaction_id": params.TransactionID,
		"to_username":    params.ToUserName,
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
