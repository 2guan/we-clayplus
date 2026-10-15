// 文件名：payment.go
// 主要功能：接通 core 支付接口，保留原始回执，拒绝空回执且不自动重试。
// 作者：2guan
// Package paymentability 提供支付能力的实现（直连型）。
package paymentability

import (
	sdk "github.com/we-clay/we-clay/sdk/payment"

	"fmt"
	baseapi "github.com/we-clay/we-clay/gateway/internal/api/base"
	paymentapi "github.com/we-clay/we-clay/gateway/internal/api/payment"
	sdkroot "github.com/we-clay/we-clay/sdk"
	"net/url"
	"strings"
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
	if e := validatePacket(params.NativeURL); e != nil {
		return nil, e
	}
	if params.InWay < 0 {
		return nil, fmt.Errorf("红包来源无效")
	}
	resp, err := a.api.ReceiveHongBao(paymentapi.ReceiveHongBaoParams(params))
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("支付回执缺失，结果未确认，不应重试")
	}
	if resp.GetBaseResponse() == nil || resp.GetBaseResponse().Code == nil || resp.GetBaseResponse().GetCode() != 0 {
		return nil, fmt.Errorf("支付基础回执未确认")
	}
	if resp.GetCode() != 0 || resp.GetErrorType() != 0 {
		return nil, fmt.Errorf("支付业务未成功，返回码 %d / %d", resp.GetCode(), resp.GetErrorType())
	}
	if b := resp.GetText(); b != nil && b.Size != nil && int(b.GetSize()) != len(b.Data) {
		return nil, fmt.Errorf("红包回执长度不符")
	}
	return mapHongBaoResponse(resp), nil
}

// OpenHongBao 打开红包
func (a ability) OpenHongBao(params sdk.OpenHongBaoParams) (*sdk.HongBaoResponse, error) {
	if e := validatePacket(params.NativeURL); e != nil {
		return nil, e
	}
	if params.TimingIdentifier == "" || params.SendUserName == "" {
		return nil, fmt.Errorf("缺少拆红包凭据")
	}
	resp, err := a.api.OpenHongBao(paymentapi.OpenHongBaoParams(params))
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("支付回执缺失，结果未确认，不应重试")
	}
	if resp.GetBaseResponse() == nil || resp.GetBaseResponse().Code == nil || resp.GetBaseResponse().GetCode() != 0 {
		return nil, fmt.Errorf("支付基础回执未确认")
	}
	if resp.GetCode() != 0 || resp.GetErrorType() != 0 {
		return nil, fmt.Errorf("支付业务未成功，返回码 %d / %d", resp.GetCode(), resp.GetErrorType())
	}
	if b := resp.GetText(); b != nil && b.Size != nil && int(b.GetSize()) != len(b.Data) {
		return nil, fmt.Errorf("红包回执长度不符")
	}
	return mapHongBaoResponse(resp), nil
}

// GrabHongBao 抢红包
func (a ability) GrabHongBao(params sdk.GrabHongBaoParams) (*sdk.HongBaoResponse, error) {
	if e := validatePacket(params.NativeURL); e != nil {
		return nil, e
	}
	if params.InWay < 0 {
		return nil, fmt.Errorf("红包来源无效")
	}
	resp, err := a.api.GrabHongBao(paymentapi.GrabHongBaoParams(params))
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("支付回执缺失，结果未确认，不应重试")
	}
	if resp.GetBaseResponse() == nil || resp.GetBaseResponse().Code == nil || resp.GetBaseResponse().GetCode() != 0 {
		return nil, fmt.Errorf("支付基础回执未确认")
	}
	if resp.GetCode() != 0 || resp.GetErrorType() != 0 {
		return nil, fmt.Errorf("支付业务未成功，返回码 %d / %d", resp.GetCode(), resp.GetErrorType())
	}
	if b := resp.GetText(); b != nil && b.Size != nil && int(b.GetSize()) != len(b.Data) {
		return nil, fmt.Errorf("红包回执长度不符")
	}
	return mapHongBaoResponse(resp), nil
}

// QueryHongBaoDetail 查询红包领取详情
func (a ability) QueryHongBaoDetail(params sdk.QueryHongBaoDetailParams) (*sdk.HongBaoResponse, error) {
	if e := validatePacket(params.NativeURL); e != nil {
		return nil, e
	}
	if params.SendUserName == "" {
		return nil, fmt.Errorf("缺少发送人")
	}
	resp, err := a.api.QueryHongBaoDetail(paymentapi.QueryHongBaoDetailParams(params))
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("支付回执缺失，结果未确认，不应重试")
	}
	if resp.GetBaseResponse() == nil || resp.GetBaseResponse().Code == nil || resp.GetBaseResponse().GetCode() != 0 {
		return nil, fmt.Errorf("支付基础回执未确认")
	}
	if resp.GetCode() != 0 || resp.GetErrorType() != 0 {
		return nil, fmt.Errorf("支付业务未成功，返回码 %d / %d", resp.GetCode(), resp.GetErrorType())
	}
	if b := resp.GetText(); b != nil && b.Size != nil && int(b.GetSize()) != len(b.Data) {
		return nil, fmt.Errorf("红包回执长度不符")
	}
	return mapHongBaoResponse(resp), nil
}

// QueryHongBaoList 查询红包领取列表
func (a ability) QueryHongBaoList(params sdk.QueryHongBaoListParams) (*sdk.HongBaoResponse, error) {
	if e := validatePacket(params.NativeURL); e != nil {
		return nil, e
	}
	if params.Offset < 0 || params.Limit < 0 {
		return nil, fmt.Errorf("分页参数无效")
	}
	resp, err := a.api.QueryHongBaoList(paymentapi.QueryHongBaoListParams(params))
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("支付回执缺失，结果未确认，不应重试")
	}
	if resp.GetBaseResponse() == nil || resp.GetBaseResponse().Code == nil || resp.GetBaseResponse().GetCode() != 0 {
		return nil, fmt.Errorf("支付基础回执未确认")
	}
	if resp.GetCode() != 0 || resp.GetErrorType() != 0 {
		return nil, fmt.Errorf("支付业务未成功，返回码 %d / %d", resp.GetCode(), resp.GetErrorType())
	}
	if b := resp.GetText(); b != nil && b.Size != nil && int(b.GetSize()) != len(b.Data) {
		return nil, fmt.Errorf("红包回执长度不符")
	}
	return mapHongBaoResponse(resp), nil
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
	if params.InvalidTime == "" || params.TransferID == "" || params.TransactionID == "" || params.ToUserName == "" {
		return nil, fmt.Errorf("缺少原始转账票据")
	}
	resp, err := a.api.CollectMoney(paymentapi.CollectMoneyParams(params))
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("支付回执缺失，结果未确认，不应重试")
	}
	if resp.GetBaseResponse() == nil || resp.GetBaseResponse().Code == nil || resp.GetBaseResponse().GetCode() != 0 {
		return nil, fmt.Errorf("支付基础回执未确认")
	}
	if resp.GetCode() != 0 || resp.GetErrorType() != 0 {
		return nil, fmt.Errorf("支付业务未成功，返回码 %d / %d", resp.GetCode(), resp.GetErrorType())
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

// 接收票据必须来自微信红包协议；本层不读取支付凭据，也不猜测密码加密算法。
func validatePacket(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "wxpay" || !strings.EqualFold(u.Host, "c2cbizmessagehandler") {
		return fmt.Errorf("红包票据地址无效")
	}
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil || q.Get("msgtype") == "" || q.Get("channelid") == "" || q.Get("sendid") == "" {
		return fmt.Errorf("红包票据字段缺失")
	}
	return nil
}
