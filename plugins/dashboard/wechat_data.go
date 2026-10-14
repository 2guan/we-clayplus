// 文件名：wechat_data.go
// 主要功能：从已归档消息补齐会话，并保存读取到的朋友圈动态。
// 作者：2guan
package dashboard

import (
	"encoding/xml"
	"sort"
	"strings"
)

// ensureConversation 不把陌生会话冒充好友；联系人未同步也能显示已收到的消息。
func ensureConversation(v *State, m Message) {
	if m.Target == "" {
		return
	}
	for _, c := range v.Contacts {
		if c.ID == m.Target {
			return
		}
	}
	kind, name := "contact", m.Target
	if strings.HasSuffix(m.Target, "@chatroom") {
		kind = "group"
	} else if strings.HasPrefix(m.Target, "gh_") {
		kind = "official"
	} else if m.Target == "weixin" || m.Target == "filehelper" || strings.HasSuffix(m.Target, "app") {
		kind = "system"
	} else if !m.Self && m.Sender != "" {
		name = m.Sender
	}
	c := Contact{ID: m.Target, Name: name, Kind: kind, Tags: []string{}, Mode: "paused", Time: m.Time, Preview: m.Content}
	classifyEnterpriseContact(&c)
	v.Contacts = append(v.Contacts, c)
}

// SetMoments 合并分页，沿用字符串 ID，避免 JavaScript 大整数精度丢失。
func (s *Server) SetMoments(items []Moment) error {
	err := s.store.change(func(v *State) error {
		byID := make(map[string]Moment)
		for _, m := range v.Moments {
			byID[m.ID] = m
		}
		for _, m := range items {
			byID[m.ID] = m
		}
		v.Moments = make([]Moment, 0, len(byID))
		for _, m := range byID {
			v.Moments = append(v.Moments, m)
		}
		sort.SliceStable(v.Moments, func(i, j int) bool { return v.Moments[i].Time > v.Moments[j].Time })
		return nil
	})
	if err == nil {
		s.notify()
	}
	return err
}

// isConversationStatus 识别历史版本误归档的同步游标；仅过滤明确的 op/name 结构。
func isConversationStatus(content string) bool {
	var v struct {
		XMLName xml.Name `xml:"msg"`
		Op      struct {
			Name string `xml:"name"`
		} `xml:"op"`
	}
	if !strings.HasPrefix(strings.TrimSpace(content), "<msg") || xml.Unmarshal([]byte(content), &v) != nil {
		return false
	}
	switch v.Op.Name {
	case "lastMessage", "HandOffMaster", "HandOff", "DownloadFile", "MomentsTimelineStatus", "MomentsUnreadMsgStatus":
		return true
	}
	return false
}

// SetMomentCover 仅保存微信返回的背景地址，不生成装饰文案。
func (s *Server) SetMomentCover(cover string) error {
	err := s.store.change(func(v *State) error { v.Account.MomentCover = cover; return nil })
	if err == nil {
		s.notify()
	}
	return err
}

// SetContactSync 让用户看见耗时同步的进度或失败原因，不把部分结果误报为全部完成。
func (s *Server) SetContactSync(status string) {
	_ = s.store.change(func(v *State) error { v.ContactSync = status; return nil })
	s.notify()
}

// ConversationIDs 返回已有会话的标识，用于补齐不在通讯录列表中的群和联系人。
func (s *Server) ConversationIDs() []string {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	ids := []string{}
	for _, c := range s.store.state.Contacts {
		if c.Kind == "group" || c.Kind == "contact" || c.Kind == "official" {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

// classifyEnterpriseContact 账号类型与好友关系分开处理，缺少资料不伪造昵称或企业名称。
func classifyEnterpriseContact(c *Contact) {
	if strings.HasSuffix(c.ID, "@openim") {
		c.Enterprise = true
		c.Kind = "enterprise"
	} else if strings.HasSuffix(c.ID, "@im.chatroom") {
		c.Enterprise = true
		c.Kind = "group"
	} else {
		return
	}
	if c.Name == "" || c.Name == c.ID {
		if c.Kind == "group" {
			c.Name = "企业微信群"
		} else {
			c.Name = "企业微信联系人"
			id := strings.TrimSuffix(c.ID, "@openim")
			if len(id) > 6 {
				id = id[len(id)-6:]
			}
			c.Name += " · " + id
		}
		c.ProfileNote = "昵称资料暂未获取，微信 ID 可用于区分联系人"
	}
}
