// 文件名：payment.go
// 主要功能：把人工领取请求适配到 SDK，检查支付回执，不将票据或错误原文回传页面。
// 作者：2guan
package console

import (
	"encoding/json"
	"fmt"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/payment"
	"strconv"
	"strings"
)

func claimPayment(c control.Command) (any, error) {
	var q struct {
		Account       string `json:"account"`
		Kind          string `json:"kind"`
		NativeURL     string `json:"native_url"`
		InvalidTime   string `json:"invalid_time"`
		TransferID    string `json:"transfer_id"`
		TransactionID string `json:"transaction_id"`
		InWay         int    `json:"in_way"`
	}
	if e := json.Unmarshal(c.Params, &q); e != nil {
		return nil, fmt.Errorf("支付参数无效")
	}
	if contact.Instance == nil {
		return nil, fmt.Errorf("微信账号尚未就绪")
	}
	self := contact.Instance.GetSelf()
	if self == nil || self.Username != q.Account {
		return nil, fmt.Errorf("微信账号已变化，未提交领取")
	}
	if payment.Instance == nil {
		return nil, fmt.Errorf("支付服务未就绪")
	}
	var text string
	switch q.Kind {
	case "redpacket":
		r, e := payment.Instance.GrabHongBao(payment.GrabHongBaoParams{NativeURL: q.NativeURL, InWay: q.InWay})
		if e != nil {
			return nil, fmt.Errorf("领取未确认，请在微信核对，勿重复提交")
		}
		if r == nil || r.Text == nil {
			return nil, fmt.Errorf("缺少领取回执")
		}
		text = string(r.Text.Data)
	case "transfer":
		r, e := payment.Instance.CollectMoney(payment.CollectMoneyParams{InvalidTime: q.InvalidTime, TransferID: q.TransferID, TransactionID: q.TransactionID, ToUserName: c.Target})
		if e != nil {
			return nil, fmt.Errorf("收款未确认，请在微信核对，勿重复提交")
		}
		if r == nil {
			return nil, fmt.Errorf("缺少收款回执")
		}
		text = r.Text
	default:
		return nil, fmt.Errorf("不支持的支付操作")
	}
	if e := paymentTextConfirmed(text); e != nil {
		return nil, e
	}
	return map[string]bool{"confirmed": true}, nil
}

// 传输成功不代表到账：只接受明确的嵌套业务成功码，未知格式不猜测成功。
func paymentTextConfirmed(text string) error {
	if len(text) > 1<<20 {
		return fmt.Errorf("支付回执异常，请在微信核对")
	}
	var v map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &v) != nil || v == nil {
		return fmt.Errorf("支付回执格式尚未识别，请在微信核对")
	}
	found := false
	for _, key := range []string{"retcode", "ret_code", "retCode", "err_code", "errcode"} {
		if raw, ok := v[key]; ok {
			found = true
			value := strings.Trim(string(raw), "\"")
			n, e := strconv.Atoi(value)
			if e != nil || n != 0 {
				return fmt.Errorf("支付业务未确认，请在微信核对")
			}
		}
	}
	if !found {
		return fmt.Errorf("缺少支付业务成功码，请在微信核对")
	}
	return nil
}
