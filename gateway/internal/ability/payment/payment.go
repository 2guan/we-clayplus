// Package paymentability 提供支付能力的实现（直连型）。
package paymentability

import (
	sdk "github.com/we-clay/we-clay/sdk/payment"

	baseapi "github.com/we-clay/we-clay/gateway/internal/api/base"
	paymentapi "github.com/we-clay/we-clay/gateway/internal/api/payment"
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
	resp, err := a.api.GeneratePayQCode()
	if resp == nil || err != nil {
		return nil, err
	}
	return mapF2FQrcodeResponse(resp), nil
}

// GetBandCardList 获取已绑定银行卡及余额
func (a ability) GetBandCardList() (*sdk.TenPayResponse, error) {
	resp, err := a.api.GetBandCardList()
	if resp == nil || err != nil {
		return nil, err
	}
	return mapTenPayResponse(resp), nil
}

// CreateHongBao 创建红包
func (a ability) CreateHongBao(params sdk.CreateHongBaoParams) (*sdk.HongBaoResponse, error) {
	resp, err := a.api.CreateHongBao(paymentapi.CreateHongBaoParams{
		HBType:   params.HBType,
		Username: params.Username,
		InWay:    params.InWay,
		Count:    params.Count,
		Amount:   params.Amount,
		Wishing:  params.Wishing,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return mapHongBaoResponse(resp), nil
}

// ReceiveHongBao 接收红包
func (a ability) ReceiveHongBao(params sdk.ReceiveHongBaoParams) (*sdk.HongBaoResponse, error) {
	resp, err := a.api.ReceiveHongBao(paymentapi.ReceiveHongBaoParams{
		NativeURL: params.NativeURL,
		InWay:     params.InWay,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return mapHongBaoResponse(resp), nil
}

// OpenHongBao 打开红包
func (a ability) OpenHongBao(params sdk.OpenHongBaoParams) (*sdk.HongBaoResponse, error) {
	resp, err := a.api.OpenHongBao(paymentapi.OpenHongBaoParams{
		NativeURL:        params.NativeURL,
		TimingIdentifier: params.TimingIdentifier,
		SendUserName:     params.SendUserName,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return mapHongBaoResponse(resp), nil
}

// GrabHongBao 抢红包
func (a ability) GrabHongBao(params sdk.GrabHongBaoParams) (*sdk.HongBaoResponse, error) {
	resp, err := a.api.GrabHongBao(paymentapi.GrabHongBaoParams{
		NativeURL: params.NativeURL,
		InWay:     params.InWay,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return mapHongBaoResponse(resp), nil
}

// QueryHongBaoDetail 查询红包领取详情
func (a ability) QueryHongBaoDetail(params sdk.QueryHongBaoDetailParams) (*sdk.HongBaoResponse, error) {
	resp, err := a.api.QueryHongBaoDetail(paymentapi.QueryHongBaoDetailParams{
		NativeURL:    params.NativeURL,
		SendUserName: params.SendUserName,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return mapHongBaoResponse(resp), nil
}

// QueryHongBaoList 查询红包领取列表
func (a ability) QueryHongBaoList(params sdk.QueryHongBaoListParams) (*sdk.HongBaoResponse, error) {
	resp, err := a.api.QueryHongBaoList(paymentapi.QueryHongBaoListParams{
		NativeURL:    params.NativeURL,
		SendUserName: params.SendUserName,
		Offset:       params.Offset,
		Limit:        params.Limit,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return mapHongBaoResponse(resp), nil
}

// CreatePreTransfer 创建转账
func (a ability) CreatePreTransfer(params sdk.CreatePreTransferParams) (*sdk.TenPayResponse, error) {
	resp, err := a.api.CreatePreTransfer(paymentapi.CreatePreTransferParams{
		ToUserName:  params.ToUserName,
		Fee:         params.Fee,
		Description: params.Description,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return mapTenPayResponse(resp), nil
}

// ConfirmPreTransfer 确认转账
func (a ability) ConfirmPreTransfer(params sdk.ConfirmPreTransferParams) (*sdk.TenPayResponse, error) {
	resp, err := a.api.ConfirmPreTransfer(paymentapi.ConfirmPreTransferParams{
		BankType:    params.BankType,
		BankSerial:  params.BankSerial,
		ReqKey:      params.ReqKey,
		PayPassword: params.PayPassword,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return mapTenPayResponse(resp), nil
}

// CollectMoney 确认收款
func (a ability) CollectMoney(params sdk.CollectMoneyParams) (*sdk.TenPayResponse, error) {
	resp, err := a.api.CollectMoney(paymentapi.CollectMoneyParams{
		InvalidTime:   params.InvalidTime,
		TransferID:    params.TransferID,
		TransactionID: params.TransactionID,
		ToUserName:    params.ToUserName,
	})
	if resp == nil || err != nil {
		return nil, err
	}
	return mapTenPayResponse(resp), nil
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
