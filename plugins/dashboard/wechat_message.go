// 文件名：wechat_message.go
// 主要功能：把公众号图文和微信系统通知转换为可展示消息，隔离客户端控制指令。
// 作者：2guan
package dashboard

import (
	"encoding/xml"
	"net/url"
	"strings"
)

func parseRevokeNotice(raw string) (id, content string, ok bool) {
	var notice struct {
		Type string `xml:"type,attr"`
		Body struct {
			ID       string `xml:"newmsgid"`
			LegacyID string `xml:"msgid"`
			Replaced string `xml:"replacemsg"`
		} `xml:"revokemsg"`
	}
	if xml.Unmarshal([]byte(raw), &notice) != nil || notice.Type != "revokemsg" {
		return "", "", false
	}
	id = strings.TrimSpace(notice.Body.ID)
	if id == "" {
		id = strings.TrimSpace(notice.Body.LegacyID)
	}
	return id, strings.TrimSpace(notice.Body.Replaced), true
}

func applyRevokeNotice(v *State, id, content string) bool {
	content = resolveNotice(*v, Message{Kind: "system", Content: content}).Content
	for i := range v.Messages {
		m := &v.Messages[i]
		if m.ID != id {
			continue
		}
		if strings.Contains(content, "你撤回") || strings.Contains(content, "我撤回") {
			m.Status = "revoked"
		} else {
			m.Status = "revoked_by_other"
			m.Content = content
		}
		return true
	}
	return false
}

// MessageArticle 保存展示必要字段，不把原始 XML 或客户端配置传给浏览器。
type MessageArticle struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
	Image       string `json:"image,omitempty"`
	// CDN 票据仅在解析和加密落库之间传递，不输出到浏览器。
	ThumbID  string `json:"-"`
	ThumbKey string `json:"-"`
}

func messageWebURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	return u.String()
}

// ParseWechatMessage 共用于新消息和历史修复；false 表示无需出现在聊天中的控制事件。
func ParseWechatMessage(m Message, raw string) (Message, bool) {
	// 既有结构化卡片也做 URL 校验，避免历史数据或上游异常注入脚本链接。
	cards := make([]MessageArticle, 0, len(m.Articles))
	for _, card := range m.Articles {
		card.URL = messageWebURL(card.URL)
		if !strings.HasPrefix(card.Image, "/api/message-thumbnail?id=") {
			card.Image = messageWebURL(card.Image)
		}
		if card.URL != "" {
			cards = append(cards, card)
		}
	}
	m.Articles = cards
	if rich, ok := parseRichMessage(m, raw); ok {
		return rich, true
	}
	if isConversationStatus(raw) {
		return m, false
	}
	if len(raw) > 4<<20 {
		if m.Kind == "system" {
			m.Content = "[微信系统通知，请在微信中查看]"
		}
		return m, true
	}
	var root struct {
		XMLName xml.Name
		Type    string `xml:"type,attr"`
	}
	if xml.Unmarshal([]byte(raw), &root) != nil {
		return m, true
	}
	if root.XMLName.Local == "sysmsg" && m.Kind == "system" {
		if _, _, revoke := parseRevokeNotice(raw); revoke {
			return m, false
		}
		// 这些通知更新客户端配置、红点和内部任务，没有用户可阅读的聊天正文。
		switch root.Type {
		case "dynacfg", "functionmsg", "gamecenter", "newtips", "sns", "gog_wcs_plugin_config", "GetClientCheckForMinorDevice", "ClientCheckGetExtInfo":
			return m, false
		}
		var notice struct {
			Replaced string `xml:"revokemsg>replacemsg"`
			Content  string `xml:"content"`
			Plain    string `xml:"sysmsgtemplate>content_template>plain"`
		}
		_ = xml.Unmarshal([]byte(raw), &notice)
		switch {
		case notice.Replaced != "":
			m.Content = notice.Replaced
		case notice.Plain != "":
			m.Content = notice.Plain
		case notice.Content != "":
			m.Content = notice.Content
		default:
			m.Content = "[微信系统通知，请在微信中查看详情]"
		}
		return m, true
	}
	// 仅处理已标记为链接的应用消息，普通用户粘贴的 XML 仍然是普通文字。
	if root.XMLName.Local != "msg" || m.Kind != "link" {
		return m, true
	}
	var v struct {
		App struct {
			Title       string `xml:"title"`
			Description string `xml:"des"`
			URL         string `xml:"url"`
			Thumb       string `xml:"thumburl"`
			ThumbID     string `xml:"appattach>cdnthumburl"`
			ThumbKey    string `xml:"appattach>cdnthumbaeskey"`
			Categories  []struct {
				Name  string `xml:"name"`
				Items []struct {
					Title  string `xml:"title"`
					URL    string `xml:"url"`
					Digest string `xml:"digest"`
					Cover  string `xml:"cover"`
				} `xml:"item"`
			} `xml:"mmreader>category"`
		} `xml:"appmsg"`
	}
	if xml.Unmarshal([]byte(raw), &v) != nil {
		return m, true
	}
	m.Articles = nil
	for _, category := range v.App.Categories {
		if m.Publisher == "" {
			m.Publisher = category.Name
		}
		for _, item := range category.Items {
			if link := messageWebURL(item.URL); link != "" {
				m.Articles = append(m.Articles, MessageArticle{Title: item.Title, Description: item.Digest, URL: link, Image: messageWebURL(item.Cover)})
			}
		}
	}
	if len(m.Articles) == 0 {
		if link := messageWebURL(v.App.URL); link != "" {
			m.Articles = []MessageArticle{{Title: v.App.Title, Description: v.App.Description, URL: link, Image: messageWebURL(v.App.Thumb), ThumbID: v.App.ThumbID, ThumbKey: v.App.ThumbKey}}
		}
	}
	if len(m.Articles) > 0 {
		m.Content = m.Articles[0].Title
	}
	return m, true
}

// applyPublisher 仅用该公众号自己推送的名称补齐占位名，不覆盖已同步的昵称和备注。
func applyPublisher(v *State, m Message) {
	if m.Self || !strings.HasPrefix(m.Target, "gh_") || m.Publisher == "" {
		return
	}
	for i := range v.Contacts {
		c := &v.Contacts[i]
		if c.ID == m.Target && (c.Name == "" || c.Name == c.ID) {
			c.Name = m.Publisher
		}
	}
}

// normalizeWechatArchive 升级已归档的系统通知，并重建预览，避免列表继续显示配置 XML。
func normalizeWechatArchive(v *State) {
	for _, m := range v.Messages {
		if m.Kind == "system" {
			if id, content, ok := parseRevokeNotice(m.Content); ok && id != "" {
				applyRevokeNotice(v, id, content)
			}
		}
	}
	// 先更新原消息，再复制归档，避免原消息早于撤回通知时丢失更新。
	messages := make([]Message, 0, len(v.Messages))
	for _, m := range v.Messages {
		if next, keep := ParseWechatMessage(m, m.Content); keep {
			messages = append(messages, resolveNotice(*v, next))
		}
	}
	v.Messages = messages
	for i := range v.Contacts {
		c := &v.Contacts[i]
		if strings.HasPrefix(strings.TrimSpace(c.Preview), "<sysmsg") || isConversationStatus(c.Preview) {
			c.Preview = ""
		}
	}
	for _, m := range v.Messages {
		applyPublisher(v, m)
		for i := range v.Contacts {
			if v.Contacts[i].ID == m.Target && (v.Contacts[i].Preview == "" || m.Time >= v.Contacts[i].Time) {
				v.Contacts[i].Preview = m.Content
				v.Contacts[i].Time = m.Time
			}
		}
	}
}
