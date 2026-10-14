// 文件名：factory.go
// 主要功能：解析微信消息并在联系人尚未同步时保留原始收发标识。
// 作者：2guan
package messageability

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/duke-git/lancet/v2/formatter"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/chatroom"
	"github.com/we-clay/we-clay/sdk/contact"
	sdk "github.com/we-clay/we-clay/sdk/message"
)

// Build 从协议层 NewMessage 构建 SDK Message（由 gateway 同步调用）
func Build(msg *messageapi.NewMessage) (*sdk.Message, error) {
	marshal, _ := json.Marshal(msg)

	sender := contact.Instance.Get(msg.Sender.Value)
	receiver := contact.Instance.Get(msg.Receiver.Value)

	// 通讯录未命中时仍保留协议 ID，避免自己发送的消息丢失收件人。
	if sender == nil {
		sender = &contact.Contact{Username: msg.GetSender().GetValue()}
	}
	if receiver == nil {
		receiver = &contact.Contact{Username: msg.GetReceiver().GetValue()}
	}

	content := msg.GetContent().GetValue()
	msgType := sdk.TypeOf(msg.GetType())

	// 群消息解析 member
	var member *chatroom.Member
	if (strings.HasSuffix(msg.GetSender().GetValue(), "@chatroom") || strings.HasSuffix(msg.GetSender().GetValue(), "@im.chatroom")) && strings.Contains(content, ":\n") && msgType != sdk.TypeSystemTip {
		parts := strings.SplitN(content, ":\n", 2)
		memberName := parts[0]
		content = parts[1]
		member = chatroom.Instance.GetMember(msg.Sender.Value, memberName)
	}

	result := &sdk.Message{
		Id:        msg.GetNewId(),
		Type:      msgType,
		Sender:    sender,
		Receiver:  receiver,
		Member:    member,
		Content:   content,
		Raw:       string(marshal),
		Timestamp: msg.GetCreateTime(),
	}

	switch msgType {
	case sdk.TypeText:
		buildText(result, msg)
	case sdk.TypeImage:
		buildImage(result, msg)
	case sdk.TypeVoice:
		buildVoice(result, msg)
	case sdk.TypeVideo, sdk.TypeTinyVideo:
		buildVideo(result, msg)
	case sdk.TypeEmoji:
		buildEmoji(result, msg)
	case sdk.TypeLocation:
		buildLocation(result, msg)
	case sdk.TypeApplication:
		buildApp(result, msg)
	}

	return result, nil
}

func buildText(msg *sdk.Message, raw *messageapi.NewMessage) {
	var t struct {
		XmlName xml.Name `xml:"msgsource"`
		Reminds string   `xml:"atuserlist"`
	}
	if err := xml.Unmarshal([]byte(raw.GetSource()), &t); err != nil {
		return
	}
	var reminds []string
	if t.Reminds != "" {
		reminds = strings.Split(t.Reminds, ",")
	}
	msg.Data = &sdk.Message_Text{Text: &sdk.TextData{
		Content: msg.Content,
		Reminds: reminds,
	}}
}

func buildImage(msg *sdk.Message, raw *messageapi.NewMessage) {
	var temp struct {
		Msg   xml.Name `xml:"msg"`
		Image struct {
			Md5    string `xml:"md5,attr"`
			Key    string `xml:"aeskey,attr"`
			Url    string `xml:"url,attr"`
			CDNUrl string `xml:"cdnmidimgurl,attr"`
			Size   uint32 `xml:"length,attr"`
			Width  uint32 `xml:"cdnthumbwidth,attr"`
			Height uint32 `xml:"cdnthumbheight,attr"`
		} `xml:"img"`
	}
	if err := xml.Unmarshal([]byte(msg.GetContent()), &temp); err != nil {
		slog.Warn("parse image xml failed", "err", err)
		return
	}
	if temp.Image.CDNUrl != "" {
		temp.Image.Url = temp.Image.CDNUrl
	}
	msg.Content = fmt.Sprintf("[%d x %d] %s", temp.Image.Width, temp.Image.Height, formatter.DecimalBytes(float64(temp.Image.Size), 2))
	msg.Data = &sdk.Message_Image{Image: &sdk.ImageData{
		Media:  &sdk.Media{Md5: temp.Image.Md5, Key: temp.Image.Key, Url: temp.Image.Url, Size: temp.Image.Size},
		Width:  temp.Image.Width,
		Height: temp.Image.Height,
	}}
}

func buildVoice(msg *sdk.Message, raw *messageapi.NewMessage) {
	var temp struct {
		XmlName xml.Name `xml:"msg"`
		Voice   struct {
			Key      string `xml:"aeskey,attr"`
			Url      string `xml:"voiceurl,attr"`
			Size     uint32 `xml:"length,attr"`
			Duration uint32 `xml:"voicelength,attr"`
		} `xml:"voicemsg"`
	}
	if err := xml.Unmarshal([]byte(msg.GetContent()), &temp); err != nil {
		slog.Warn("parse voice xml failed", "err", err)
		return
	}
	msg.Content = fmt.Sprintf("[%ds] %s", (temp.Voice.Duration+999)/1000, formatter.DecimalBytes(float64(temp.Voice.Size), 2))
	msg.Data = &sdk.Message_Voice{Voice: &sdk.VoiceData{
		Media:    &sdk.Media{Key: temp.Voice.Key, Url: temp.Voice.Url, Size: temp.Voice.Size},
		Duration: temp.Voice.Duration,
	}}
}

func buildVideo(msg *sdk.Message, raw *messageapi.NewMessage) {
	var temp struct {
		XmlName xml.Name `xml:"msg"`
		Video   struct {
			Size     uint32 `xml:"length,attr"`
			Duration uint32 `xml:"playlength,attr"`
			Md5      string `xml:"md5,attr"`
			NewMd5   string `xml:"newmd5,attr"`
			Key      string `xml:"aeskey,attr"`
			Url      string `xml:"cdnvideourl,attr"`
			ThumbUrl string `xml:"cdnthumburl,attr"`
		} `xml:"videomsg"`
	}
	if err := xml.Unmarshal([]byte(msg.GetContent()), &temp); err != nil {
		slog.Warn("parse video xml failed", "err", err)
		return
	}
	msg.Content = fmt.Sprintf("[%ds] %s", temp.Video.Duration, formatter.DecimalBytes(float64(temp.Video.Size), 2))
	msg.Data = &sdk.Message_Video{Video: &sdk.VideoData{
		Media:    &sdk.Media{Md5: temp.Video.Md5, Key: temp.Video.Key, Url: temp.Video.Url, Size: temp.Video.Size},
		Duration: temp.Video.Duration,
		ThumbUrl: temp.Video.ThumbUrl,
		NewMd5:   temp.Video.NewMd5,
	}}
}

func buildEmoji(msg *sdk.Message, raw *messageapi.NewMessage) {
	var temp struct {
		Msg   xml.Name `xml:"msg"`
		Emoji struct {
			Md5  string `xml:"md5,attr"`
			Desc string `xml:"desc,attr"`
			Key  string `xml:"aeskey,attr"`
			Url  string `xml:"cdnurl,attr"`
		} `xml:"emoji"`
	}
	if err := xml.Unmarshal([]byte(msg.GetContent()), &temp); err != nil {
		slog.Warn("parse emoji xml failed", "err", err)
		return
	}
	msg.Content = temp.Emoji.Md5
	msg.Data = &sdk.Message_Emoji{Emoji: &sdk.EmojiData{
		Media: &sdk.Media{Md5: temp.Emoji.Md5, Key: temp.Emoji.Key, Url: temp.Emoji.Url},
		Desc:  temp.Emoji.Desc,
	}}
}

func buildLocation(msg *sdk.Message, raw *messageapi.NewMessage) {
	var temp struct {
		XmlName  xml.Name `xml:"msg"`
		Location struct {
			Latitude  float64 `xml:"x,attr"`
			Longitude float64 `xml:"y,attr"`
			Scale     int32   `xml:"scale,attr"`
			PoiName   string  `xml:"poiname,attr"`
			Label     string  `xml:"label,attr"`
		} `xml:"location"`
	}
	if err := xml.Unmarshal([]byte(msg.GetContent()), &temp); err != nil {
		slog.Warn("parse location xml failed", "err", err)
		return
	}
	msg.Content = temp.Location.PoiName
	msg.Data = &sdk.Message_Location{Location: &sdk.LocationData{
		Latitude:  temp.Location.Latitude,
		Longitude: temp.Location.Longitude,
		Scale:     temp.Location.Scale,
		PoiName:   temp.Location.PoiName,
		Label:     temp.Location.Label,
	}}
}

func buildApp(msg *sdk.Message, raw *messageapi.NewMessage) {
	var temp struct {
		XmlName xml.Name `xml:"msg"`
		Title   string   `xml:"appmsg>title"`
		Type    uint32   `xml:"appmsg>type"`
		Url     string   `xml:"appmsg>url"`
		Desc    string   `xml:"appmsg>des"`
	}
	if err := xml.Unmarshal([]byte(msg.GetContent()), &temp); err != nil {
		slog.Warn("parse app xml failed", "err", err)
		return
	}

	typ := fmt.Sprintf("49%02d", temp.Type)
	code, _ := strconv.Atoi(typ)
	msg.Type = sdk.TypeOf(int32(code))
	msg.Content = temp.Title
	msg.Data = &sdk.Message_App{App: &sdk.AppData{
		SubType: temp.Type,
		Title:   temp.Title,
		Desc:    temp.Desc,
		Url:     temp.Url,
		Xml:     raw.GetContent().GetValue(),
	}}
}
