// Package paymentability 提供支付能力的实现（直连型）。
package paymentability

import (
	sdk "github.com/we-clay/we-clay/sdk/payment"

	baseapi "github.com/we-clay/we-clay/gateway/internal/api/base"
	paymentapi "github.com/we-clay/we-clay/gateway/internal/api/payment"
	sdkroot "github.com/we-clay/we-clay/sdk"
)

// ability 支付能力实现（直连型）。
type ability struct {
	api paymentapi.PaymentService
}

func init() {
	sdk.Instance = &ability{api: paymentapi.Get()}
}

// GeneratePayQCode 生成支付收款二维码
func (a ability) GeneratePayQCode() (*sdk.F2FQrcodeResponse, error) {
	return nil, sdkroot.NotImplemented("payment.GeneratePayQCode")
}

// GetBandCardList 获取已绑定银行卡及余额
func (a ability) GetBandCardList() (*sdk.TenPayResponse, error) {
	return nil, sdkroot.NotImplemented("payment.GetBandCardList")
}

// CreateHongBao 创建红包
func (a ability) CreateHongBao(params sdk.CreateHongBaoParams) (*sdk.HongBaoResponse, error) {
	return nil, sdkroot.NotImplemented("payment.CreateHongBao")
}

// ReceiveHongBao 接收红包
func (a ability) ReceiveHongBao(params sdk.ReceiveHongBaoParams) (*sdk.HongBaoResponse, error) {
	return nil, sdkroot.NotImplemented("payment.ReceiveHongBao")
}

// OpenHongBao 打开红包
func (a ability) OpenHongBao(params sdk.OpenHongBaoParams) (*sdk.HongBaoResponse, error) {
	return nil, sdkroot.NotImplemented("payment.OpenHongBao")
}

// GrabHongBao 抢红包
func (a ability) GrabHongBao(params sdk.GrabHongBaoParams) (*sdk.HongBaoResponse, error) {
	return nil, sdkroot.NotImplemented("payment.GrabHongBao")
}

// QueryHongBaoDetail 查询红包领取详情
func (a ability) QueryHongBaoDetail(params sdk.QueryHongBaoDetailParams) (*sdk.HongBaoResponse, error) {
	return nil, sdkroot.NotImplemented("payment.QueryHongBaoDetail")
}

// QueryHongBaoList 查询红包领取列表
func (a ability) QueryHongBaoList(params sdk.QueryHongBaoListParams) (*sdk.HongBaoResponse, error) {
	return nil, sdkroot.NotImplemented("payment.QueryHongBaoList")
}

// CreatePreTransfer 创建转账
func (a ability) CreatePreTransfer(params sdk.CreatePreTransferParams) (*sdk.TenPayResponse, error) {
	return nil, sdkroot.NotImplemented("payment.CreatePreTransfer")
}

// ConfirmPreTransfer 确认转账
func (a ability) ConfirmPreTransfer(params sdk.ConfirmPreTransferParams) (*sdk.TenPayResponse, error) {
	return nil, sdkroot.NotImplemented("payment.ConfirmPreTransfer")
}

// CollectMoney 确认收款
func (a ability) CollectMoney(params sdk.CollectMoneyParams) (*sdk.TenPayResponse, error) {
	return nil, sdkroot.NotImplemented("payment.CollectMoney")
}

func mapBaseResult(resp *baseapi.BaseResponse) *sdk.BaseResult {
	if resp == nil {
		return nil
	}
	return &sdk.BaseResult{
		Code:    resp.GetCode(),
		Message: resp.GetMessage().GetValue(),
	}
}

func mapBuffer(buffer *baseapi.Buffer) *sdk.Buffer {
	if buffer == nil {
		return nil
	}
	return &sdk.Buffer{
		Size: buffer.GetSize(),
		Data: buffer.GetData(),
	}
}

func mapF2FQrcodeResponse(resp *paymentapi.F2FQrcodeResponse) *sdk.F2FQrcodeResponse {
	if resp == nil {
		return nil
	}
	return &sdk.F2FQrcodeResponse{
		BaseResult: mapBaseResult(resp.GetBaseResponse()),
		QrcodeUrl:  resp.GetQrcodeUrl(),
		QrcodeData: resp.GetQrcodeData(),
		ExpireTime: resp.GetExpireTime(),
	}
}

func mapTenPayResponse(resp *paymentapi.TenPayResponse) *sdk.TenPayResponse {
	if resp == nil {
		return nil
	}
	return &sdk.TenPayResponse{
		BaseResult:   mapBaseResult(resp.GetBaseResponse()),
		Text:         resp.GetText().GetValue(),
		Code:         resp.GetCode(),
		Message:      resp.GetMessage(),
		CgiCmd:       sdk.CGICommand(resp.GetCgiCmd()),
		ErrorType:    resp.GetErrorType(),
		ErrorMessage: resp.GetErrorMessage(),
	}
}

func mapHongBaoResponse(resp *paymentapi.HongBaoResponse) *sdk.HongBaoResponse {
	if resp == nil {
		return nil
	}
	return &sdk.HongBaoResponse{
		BaseResult:   mapBaseResult(resp.GetBaseResponse()),
		Text:         mapBuffer(resp.GetText()),
		Code:         resp.GetCode(),
		Message:      resp.GetMessage(),
		CgiCommand:   resp.GetCgiCommand(),
		ErrorType:    resp.GetErrorType(),
		ErrorMessage: resp.GetErrorMessage(),
	}
}
