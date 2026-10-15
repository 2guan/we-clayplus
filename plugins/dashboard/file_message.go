// 文件名：file_message.go
// 主要功能：从加密原文恢复历史文件卡片，并复用 SDK 下载描述，不向浏览器暴露票据。
// 作者：2guan
package dashboard

import (
	"encoding/xml"
	"fmt"
	"strconv"

	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *Server) restoreFileArchive() error {
	st := s.store.snapshot()
	updates := make(map[string]Message)
	for _, m := range st.Messages {
		if m.Kind != "app" && m.Kind != "file" {
			continue
		}
		raw, err := s.store.getSecret("quote:" + accountMessageMediaKey(st.Account.ID, m))
		if err != nil {
			continue
		}
		next, keep := ParseWechatMessage(m, raw)
		if keep && next.Kind == "file" {
			if m.Status == "revoked" {
				next.Content = m.Content
			}
			updates[m.Target+"\x00"+m.ID] = next
		}
	}
	if len(updates) == 0 {
		return nil
	}
	return s.store.change(func(v *State) error {
		if v.Account.ID != st.Account.ID {
			return fmt.Errorf("恢复文件时账号已切换")
		}
		for i, m := range v.Messages {
			if next, ok := updates[m.Target+"\x00"+m.ID]; ok {
				// 只修正展示字段，不覆盖下载缓存、撤回状态或其他并发更新。
				v.Messages[i].Kind, v.Messages[i].Content, v.Messages[i].Rich = next.Kind, next.Content, next.Rich
				v.Messages[i].Articles = nil
			}
		}
		return nil
	})
}

func (s *Server) fileDescriptor(m Message) (string, error) {
	raw, err := s.store.getSecret("quote:" + s.messageMediaKey(m))
	if err != nil {
		return "", err
	}
	var v struct {
		App struct {
			Type   int `xml:"type"`
			Attach struct {
				ID   string `xml:"attachid"`
				Size uint32 `xml:"totallen"`
			} `xml:"appattach"`
		} `xml:"appmsg"`
	}
	if len(raw) > 128<<10 || xml.Unmarshal([]byte(raw), &v) != nil || v.App.Type != 6 || v.App.Attach.ID == "" || v.App.Attach.Size == 0 || v.App.Attach.Size > 16<<20 {
		return "", fmt.Errorf("附件信息不完整或文件超过 16 MiB")
	}
	id, err := strconv.ParseInt(m.ID, 10, 64)
	if err != nil || id <= 0 {
		return "", fmt.Errorf("附件消息 ID 无效")
	}
	account := s.store.snapshot().Account.ID
	sender, receiver := m.Target, account
	if m.Self {
		sender, receiver = account, m.Target
	}
	descriptor, err := protojson.Marshal(&message.Message{
		Id: id, Type: message.TypeAppFileAttach,
		Sender: &contact.Contact{Username: sender}, Receiver: &contact.Contact{Username: receiver},
		Data: &message.Message_App{App: &message.AppData{SubType: 6, Xml: raw}},
	})
	return string(descriptor), err
}
