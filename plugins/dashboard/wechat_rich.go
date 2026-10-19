// 文件名：wechat_rich.go
// 主要功能：解析拍一拍、支付通知、位置、引用和表情包的只读展示信息。
// 作者：2guan
package dashboard

import (
	"encoding/xml"
	"github.com/we-clay/we-clay/plugins/contextcleaner"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// RichMessage 只保存展示字段，不保存支付链接、交易票据或 CDN 解密凭据。
type RichMessage struct {
	QuoteUnavailable bool     `json:"quote_unavailable,omitempty"`
	FileNoticeID     string   `json:"file_notice_id,omitempty"`
	Title            string   `json:"title,omitempty"`
	Description      string   `json:"description,omitempty"`
	Amount           string   `json:"amount,omitempty"`
	Status           string   `json:"status,omitempty"`
	Author           string   `json:"author,omitempty"`
	QuoteID          string   `json:"quote_id,omitempty"`
	QuoteType        int      `json:"quote_type,omitempty"`
	QuoteURL         string   `json:"quote_url,omitempty"`
	Quote            string   `json:"quote,omitempty"`
	Latitude         *float64 `json:"latitude,omitempty"`
	Longitude        *float64 `json:"longitude,omitempty"`
	StickerID        string   `json:"sticker_id,omitempty"`
	Sticker          string   `json:"sticker,omitempty"`
	Image            string   `json:"image,omitempty"`
}

// 只处理由 SDK 标记的协议类型，用户粘贴的 XML 仍作为普通文字。
func parseRichMessage(m Message, raw string) (Message, bool) {
	var v struct {
		XMLName   xml.Name
		Type      string `xml:"type,attr"`
		Pat       string `xml:"pat>template"`
		PatActor  string `xml:"pat>fromusername"`
		PatTarget string `xml:"pat>pattedusername"`
		Voice     struct {
			Duration int `xml:"voicelength,attr"`
		} `xml:"voicemsg"`
		Location struct {
			X     string `xml:"x,attr"`
			Y     string `xml:"y,attr"`
			Name  string `xml:"poiname,attr"`
			Label string `xml:"label,attr"`
		} `xml:"location"`
		Emoji struct {
			MD5     string `xml:"md5,attr"`
			URL     string `xml:"cdnurl,attr"`
			Thumb   string `xml:"thumburl,attr"`
			Product string `xml:"productid,attr"`
			Text    string `xml:"attachedtext,attr"`
		} `xml:"emoji"`
		App struct {
			Attach struct {
				NoticeID string `xml:"overwrite_newmsgid"`
			} `xml:"appattach"`
			Type        int    `xml:"type"`
			Title       string `xml:"title"`
			Description string `xml:"des"`
			Pay         struct {
				Subtype      int    `xml:"paysubtype"`
				Amount       string `xml:"feedesc"`
				Memo         string `xml:"pay_memo"`
				ReceiveTitle string `xml:"receivertitle"`
				SendTitle    string `xml:"sendertitle"`
			} `xml:"wcpayinfo"`
			Ref struct {
				ID      string `xml:"svrid"`
				Type    int    `xml:"type"`
				Name    string `xml:"displayname"`
				Content string `xml:"content"`
			} `xml:"refermsg"`
		} `xml:"appmsg"`
	}
	if len(raw) > 4<<20 || xml.Unmarshal([]byte(raw), &v) != nil {
		return m, false
	}
	switch {
	case m.Kind == "system" && v.Type == "pat":
		m.Interaction = &contextcleaner.Interaction{Kind: "pat", ActorID: strings.TrimSpace(v.PatActor), TargetID: strings.TrimSpace(v.PatTarget), Verified: v.PatActor != "" && v.PatTarget != ""}
		m.Content = v.Pat
		if m.Content == "" {
			m.Content = "[拍一拍]"
		}
		return m, true
	case m.Kind == "image":
		m.Content = "[图片]"
		return m, true
	case m.Kind == "audio":
		if v.Voice.Duration > 0 {
			m.Duration = (v.Voice.Duration + 999) / 1000
		}
		m.Content = "[语音]"
		return m, true
	case m.Kind == "location":
		m.Rich = &RichMessage{Title: v.Location.Name, Description: v.Location.Label}
		x, ex := strconv.ParseFloat(v.Location.X, 64)
		y, ey := strconv.ParseFloat(v.Location.Y, 64)
		if ex == nil && ey == nil && !math.IsNaN(x) && !math.IsNaN(y) && math.Abs(x) <= 90 && math.Abs(y) <= 180 {
			m.Rich.Latitude = &x
			m.Rich.Longitude = &y
		}
		if m.Rich.Title == "" {
			m.Rich.Title = "位置"
		}
		m.Content = "[位置] " + m.Rich.Title
		return m, true
	case m.Kind == "emoji":
		m.Rich = &RichMessage{StickerID: strings.ToLower(v.Emoji.MD5), Title: "表情包", Description: v.Emoji.Text, Sticker: "custom"}
		if v.Emoji.Product != "" {
			m.Rich.Sticker = "pack"
			m.Rich.Title = "表情包"
		} else {
			m.Rich.Title = "自定义表情"
		}
		for _, image := range []string{v.Emoji.URL, v.Emoji.Thumb} {
			if _, ok := wechatImageURL(image); ok {
				m.Rich.Image = "/api/wechat-image?url=" + url.QueryEscape(image)
				break
			}
		}
		m.Content = "[" + m.Rich.Title + "]"
		return m, true
	case m.Kind == "file" || m.Kind == "link" || m.Kind == "app" || m.Kind == "transfer" || m.Kind == "redpacket" || m.Kind == "quote":
		switch v.App.Type {
		case 6, 74:
			m.Kind = "file"
			m.Articles = nil
			m.Content = v.App.Title
			if m.Content == "" {
				m.Content = "文件附件"
			}
			m.Rich = nil
			if id, err := strconv.ParseUint(v.App.Attach.NoticeID, 10, 64); v.App.Type == 6 && err == nil && id != 0 {
				m.Rich = &RichMessage{FileNoticeID: strconv.FormatUint(id, 10)}
			}
			if v.App.Type == 74 {
				m.Rich = &RichMessage{Status: "uploading", Description: "文件上传中…"}
			}
			return m, true
		case 2000:
			m.Kind = "transfer"
			m.Articles = nil
			m.Rich = &RichMessage{Title: "微信转账", Amount: v.App.Pay.Amount, Description: v.App.Pay.Memo, Status: "转账状态更新"}
			if v.App.Pay.Subtype == 1 {
				m.Rich.Status = "转账通知"
			}
			m.Content = "[转账] " + v.App.Pay.Amount
			return m, true
		case 2001:
			m.Kind = "redpacket"
			m.Articles = nil
			m.Rich = &RichMessage{Title: "微信红包", Description: v.App.Pay.ReceiveTitle, Status: "请在微信中查看领取状态"}
			if m.Self {
				m.Rich.Description = v.App.Pay.SendTitle
			}
			if m.Rich.Description == "" {
				m.Rich.Description = "恭喜发财，大吉大利"
			}
			m.Content = "[微信红包]"
			return m, true
		case 57:
			m.Kind = "quote"
			m.Articles = nil
			m.Rich = &RichMessage{Author: v.App.Ref.Name, Quote: quotePreview(v.App.Ref.Type, v.App.Ref.Content), QuoteID: v.App.Ref.ID, QuoteType: v.App.Ref.Type}
			if v.App.Ref.Type == 49 {
				var linked struct {
					URL string `xml:"appmsg>url"`
				}
				if xml.Unmarshal([]byte(quoteXMLRoot(v.App.Ref.Content)), &linked) == nil {
					if u, e := url.Parse(linked.URL); e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
						m.Rich.QuoteURL = messageWebURL(linked.URL)
					}
				}
			}
			m.Content = v.App.Title
			return m, true
		}
	}
	return m, false
}

// 引用的非文字消息只呈现类型摘要，不把嵌套 XML、支付链接或凭据展示出来。
func quotePreview(kind int, content string) string {
	if kind == 1 {
		return content
	}
	if label := map[int]string{3: "图片", 34: "语音", 47: "表情", 48: "位置", 43: "视频"}[kind]; label != "" {
		return "[" + label + "]"
	}
	var app struct {
		Title string `xml:"appmsg>title"`
		Type  int    `xml:"appmsg>type"`
	}
	if xml.Unmarshal([]byte(quoteXMLRoot(content)), &app) == nil && app.Title != "" {
		return app.Title
	}
	return "[引用消息]"
}

var noticeName = regexp.MustCompile(`\$\{([^}]+)\}`)

func resolveNotice(st State, m Message) Message {
	if m.Kind != "system" {
		return m
	}
	m.Content = noticeName.ReplaceAllStringFunc(m.Content, func(token string) string {
		id := strings.TrimSuffix(strings.TrimPrefix(token, "${"), "}")
		if id == st.Account.ID {
			return "我"
		}
		for _, c := range st.Contacts {
			if c.ID == id {
				if c.Remark != "" {
					return c.Remark
				}
				if c.Name != "" {
					return c.Name
				}
			}
		}
		return "对方"
	})
	return m
}

// 微信引用原文可能是完整 msg，也可能直接以 appmsg 开始。
func quoteXMLRoot(raw string) string {
	if strings.HasPrefix(strings.TrimSpace(raw), "<appmsg") {
		return "<msg>" + raw + "</msg>"
	}
	return raw
}
