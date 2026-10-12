package sync

import (
	"fmt"
	"google.golang.org/protobuf/proto"
	"log/slog"

	messageability "github.com/we-clay/we-clay/gateway/internal/ability/message"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/gateway/internal/plugin"
	contactsdk "github.com/we-clay/we-clay/sdk/contact"
	messagesdk "github.com/we-clay/we-clay/sdk/message"
	pluginsdk "github.com/we-clay/we-clay/sdk/plugin"
)

func (p *processor) handleMessage(messages []*messageapi.NewMessage) error {
	if !messageability.OutboundReady() {
		slog.Warn("基础数据初始化中，跳过新消息处理", "count", len(messages))
		return nil
	}

	for _, msg := range messages {
		if data, err := messageability.Build(msg); err != nil {
			slog.Error("构建消息失败", "err", err)
		} else {
			log(data)
			if p.hooks.Message != nil {
				if err := p.hooks.Message(proto.Clone(data).(*messagesdk.Message)); err != nil {
					return &callbackError{err}
				}
			}
			if !p.hooks.WithoutPlugins {

				if msg, ok := plugin.HandleCommand(data.GetContent(), data.GetSender(), data.GetMember()); ok {
					if msg.Content != "" {
						if _, err := messagesdk.Instance.Send(msg); err != nil {
							slog.Warn("命令回复失败", "receiver", data.GetSender().GetUsername(), "err", err)
						}
					}
					continue
				}

			}
			if data.Content == "无法显示此消息，你目前使用的微信版本暂时不支持此类型的信息。" {
				continue
			}

			if err := p.publish(&pluginsdk.Event{
				Topic:  data.GetType().GetTopic(),
				Sender: data.GetSender().GetUsername(),
				Payload: &pluginsdk.Event_Message{
					Message: data,
				},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func log(message *messagesdk.Message) {
	if message.Sender.GetType() == contactsdk.ContactType_CONTACT_TYPE_CHATROOM {
		sender := "system"
		if message.Member != nil {
			sender = message.Member.Nickname
		}
		slog.Info(fmt.Sprintf("%s -> %s: [%s] %s", sender, message.Sender.GetNickname(), message.Type.Desc, message.Content))
	} else {
		slog.Info(fmt.Sprintf("%s -> %s: [%s] %s", message.Sender.GetNickname(), message.Receiver.GetNickname(), message.Type.Desc, message.Content))
	}
}
