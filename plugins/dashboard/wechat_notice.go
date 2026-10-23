// 主要功能：展开微信系统通知模板，展示通话状态，不向浏览器返回通话房间或票据。
package dashboard

import (
	"encoding/xml"
	"regexp"
	"strings"
)

var systemTemplateVariable = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)\$`)

func systemTemplateContent(raw string) string {
	var notice struct {
		Body struct {
			Template string `xml:"template"`
			Links    []struct {
				Name      string `xml:"name,attr"`
				Plain     string `xml:"plain"`
				Separator string `xml:"separator"`
				Members   []struct {
					Username string `xml:"username"`
					Nickname string `xml:"nickname"`
				} `xml:"memberlist>member"`
			} `xml:"link_list>link"`
		} `xml:"sysmsgtemplate>content_template"`
	}
	if xml.Unmarshal([]byte(raw), &notice) != nil || notice.Body.Template == "" {
		return ""
	}
	values := map[string]string{}
	for _, link := range notice.Body.Links {
		var names []string
		for _, member := range link.Members {
			name := strings.TrimSpace(member.Nickname)
			if name == "" && member.Username != "" {
				name = "${" + member.Username + "}"
			}
			if name != "" {
				names = append(names, name)
			}
		}
		separator := link.Separator
		if separator == "" || len(separator) > 32 {
			separator = "、"
		}
		value := link.Plain
		if len(names) > 0 {
			value = strings.Join(names, separator)
		}
		values[link.Name] = value
	}
	complete := true
	text := systemTemplateVariable.ReplaceAllStringFunc(notice.Body.Template, func(token string) string {
		value := values[strings.Trim(token, "$")]
		if value == "" {
			complete = false
		}
		return value
	})
	if !complete {
		return ""
	}
	return strings.TrimSpace(text)
}

func parseCallNotice(m Message, raw string) Message {
	m.Content, m.Rich = "[通话通知]", nil
	var call struct {
		XMLName xml.Name `xml:"voipmsg"`
		Type    string   `xml:"type,attr"`
		Body    struct {
			Text     string `xml:"msg"`
			RoomType string `xml:"room_type"`
		} `xml:"VoIPBubbleMsg"`
	}
	if xml.Unmarshal([]byte(raw), &call) != nil || call.Type != "VoIPBubbleMsg" {
		return m
	}
	title := "通话"
	if call.Body.RoomType == "1" {
		title = "语音通话"
	}
	status := strings.TrimSpace(call.Body.Text)
	if status == "" {
		status = "通话通知"
	}
	m.Rich = &RichMessage{Title: title, Status: status}
	m.Content = "[" + title + "] " + status
	return m
}
