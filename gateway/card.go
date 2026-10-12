package gateway

import (
	"fmt"

	messageability "github.com/we-clay/we-clay/gateway/internal/ability/message"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/message"
)

// SendCard 发送联系人名片；receiver 为接收人/群 ID，username 为名片对应微信 ID。
// SDK 的通用 Message 尚无专用名片数据类型，因此提供直接入口，不改变现有 proto。
func (c *Client) SendCard(receiver, username, nickname, alias string) (*message.Send_Response, error) {
	return clientCall(c.state, "message.SendCard", func() (*message.Send_Response, error) {
		if receiver == "" || username == "" {
			return nil, fmt.Errorf("名片接收人和联系人 ID 不可为空")
		}
		resp, err := messageapi.Get().SendCard(receiver, username, nickname, alias)
		if err != nil {
			return nil, err
		}
		if len(resp.GetList()) == 0 || resp.GetList()[0].GetNewId() == 0 {
			return nil, fmt.Errorf("名片发送回执不完整，请核对发送结果")
		}
		item := resp.GetList()[0]
		if item.GetCode() != 0 {
			return nil, fmt.Errorf("名片发送未成功，返回码 %d", item.GetCode())
		}
		messageability.RememberReceipt(receiver, item.GetNewId(), item.GetClientId(), uint64(item.GetCreateTime()))
		return &message.Send_Response{NewId: item.GetNewId(), CreateTime: item.GetCreateTime()}, nil
	})
}
