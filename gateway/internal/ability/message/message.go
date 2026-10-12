// Package messageability 提供消息能力的实现。
package messageability

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/dustin/go-humanize"
	sdkroot "github.com/we-clay/we-clay/sdk"
	"google.golang.org/protobuf/proto"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	coreapi "github.com/we-clay/we-clay/gateway/internal/api"
	api "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/contact"
	sdk "github.com/we-clay/we-clay/sdk/message"
)

// ability 消息能力实现
type ability struct {
	api      api.MessageService
	receipts receiptCache
}

var (
	instance            = &ability{api: api.Get()}
	outboundReady       atomic.Bool
	errOutboundNotReady = errors.New("基础数据初始化未完成，已禁止发送消息")
)

func init() {
	outboundReady.Store(true)
	sdk.Instance = instance
}

// SetOutboundReady 设置消息出站开关。
func SetOutboundReady(ready bool) {
	outboundReady.Store(ready)
}

// OutboundReady 返回消息出站是否可用。
func OutboundReady() bool {
	return outboundReady.Load()
}

func ensureOutboundReady() error {
	if !OutboundReady() {
		return errOutboundNotReady
	}
	return nil
}

// Send 发送消息（根据类型分发到对应 API）
func (a *ability) Send(msg *sdk.Message) (*sdk.Send_Response, error) {
	if msg == nil {
		return nil, errors.New("消息不可为空")
	}
	if msg.GetReceiver() == nil || msg.GetReceiver().GetUsername() == "" {
		return nil, errors.New("消息接收人不可为空")
	}
	if err := ensureOutboundReady(); err != nil {
		return nil, err
	}

	msg = proto.Clone(msg).(*sdk.Message)
	var result sdk.Send_Response
	var clientID uint64
	receiver := msg.GetReceiver().GetUsername()

	switch msg.GetType().GetCode() {
	case sdk.TypeText.Code:
		data := msg.GetText()
		content := msg.GetContent()
		if content == "" {
			content = data.GetContent()
		}
		if content == "" {
			return nil, errors.New("消息正文不可为空")
		}
		remind := strings.Join(data.GetReminds(), ",")
		resp, err := a.api.SendText(receiver, content, remind)
		if err != nil {
			return nil, err
		}
		if len(resp.GetList()) > 0 {
			item := resp.GetList()[0]
			if item.GetCode() != 0 {
				return nil, fmt.Errorf("消息发送未成功，返回码 %d", item.GetCode())
			}
			result.NewId = item.GetNewId()
			result.CreateTime = item.GetCreateTime()
			clientID = item.GetClientId()
		}
	case sdk.TypeFile.Code:
		data := msg.GetFile()
		if data == nil || strings.TrimSpace(data.GetFilename()) == "" || data.GetMedia() == nil || len(data.GetMedia().GetData()) == 0 {
			return nil, errors.New("文件名和文件数据不可为空")
		}
		resp, err := a.api.SendFile(receiver, data.GetFilename(), bytes.NewReader(data.GetMedia().GetData()))
		if err != nil {
			return nil, err
		}
		if resp == nil || resp.GetNewId() == 0 {
			return nil, errors.New("文件发送回执缺少消息 ID，发送状态未确认，请勿自动重试")
		}
		result.NewId = resp.GetNewId()
		result.CreateTime = uint32(resp.GetCreateTime())
		clientID = numericClientID(resp.GetClientId())
		msg.Content = data.GetFilename()
	case sdk.TypeImage.Code:
		data := msg.GetImage()
		if data.GetMedia() == nil || len(data.GetMedia().GetData()) == 0 {
			return nil, errors.New("缺少待发送的媒体数据")
		}
		msg.Content = humanize.Bytes(uint64(len(data.GetMedia().Data)))
		resp, err := a.api.SendImage(receiver, bytes.NewReader(data.GetMedia().Data))
		if err != nil {
			return nil, err
		}
		result.NewId = resp.GetNewId()
		result.CreateTime = resp.GetCreateTime()
		clientID = numericClientID(resp.GetClientId().GetValue())
		result.Media = &sdk.Media{
			Key:  resp.GetAesKey(),
			Url:  resp.GetFileId(),
			Size: resp.GetSize(),
		}
	case sdk.TypeVoice.Code:
		format := int32(4)
		data := msg.GetVoice()
		if data.GetMedia() == nil || len(data.GetMedia().GetData()) == 0 {
			return nil, errors.New("缺少待发送的媒体数据")
		}
		if data != nil && data.Format != nil {
			format = data.GetFormat()
		}
		msg.Content = fmt.Sprintf("%d s", data.GetDuration())
		resp, err := a.api.SendVoice(receiver, bytes.NewReader(data.GetMedia().Data), int32(data.GetDuration()), format)
		if err != nil {
			return nil, err
		}
		result.NewId = resp.GetNewId()
		result.CreateTime = resp.GetCreateTime()
		clientID = numericClientID(resp.GetClientId())
		result.Media = &sdk.Media{
			Url:  resp.GetClientId(),
			Size: uint32(resp.GetSize()),
		}
	case sdk.TypeVideo.Code:
		data := msg.GetVideo()
		if data.GetMedia() == nil || len(data.GetMedia().GetData()) == 0 {
			return nil, errors.New("缺少待发送的媒体数据")
		}
		msg.Content = fmt.Sprintf("[%ds] %s", data.GetDuration(), humanize.Bytes(uint64(len(data.GetMedia().Data))))
		resp, err := a.api.SendVideo(receiver, bytes.NewReader(data.GetThumb()), bytes.NewReader(data.GetMedia().Data), data.GetDuration())
		if err != nil {
			return nil, err
		}
		result.NewId = resp.GetNewId()
		//result.CreateTime = resp.CreateTime
		result.Media = &sdk.Media{
			Key:  resp.GetAesKey(),
			Url:  resp.GetExtendXml(),
			Size: uint32(resp.GetVideoOffset()),
		}
	case sdk.TypeEmoji.Code:
		data := msg.GetEmoji()
		if data.GetMedia() == nil || len(data.GetMedia().GetData()) == 0 {
			return nil, errors.New("缺少待发送的媒体数据")
		}
		resp, err := a.api.SendEmoji(receiver, data.GetMedia().GetMd5(), data.GetMedia().Data)
		if err != nil {
			return nil, err
		}
		if len(resp.GetResult()) > 0 {
			item := resp.GetResult()[0]
			result.NewId = item.GetNewId()
			result.Media = &sdk.Media{Md5: item.GetMd5(), Size: uint32(item.GetSize())}
		}
	case sdk.TypeLocation.Code:
		data := msg.GetLocation()
		resp, err := a.api.SendPosition(receiver, data.GetLabel(), data.GetPoiName(), data.GetLongitude(), data.GetLatitude(), float64(data.GetScale()))
		if err != nil {
			return nil, err
		}
		if len(resp.GetList()) > 0 {
			item := resp.GetList()[0]
			if item.GetCode() != 0 {
				return nil, fmt.Errorf("消息发送未成功，返回码 %d", item.GetCode())
			}
			result.NewId = item.GetNewId()
			result.CreateTime = item.GetCreateTime()
			clientID = item.GetClientId()
		}
	case sdk.TypeApplication.Code, sdk.TypeAppChatRecord.Code, sdk.TypeAppMusic.Code:
		return nil, sdkroot.NotImplemented("message.Send: 通用应用/聊天记录/音乐")
	case sdk.TypeAppLink.Code:
		data := msg.GetApp()
		if data == nil {
			return nil, errors.New("缺少链接消息参数")
		}
		resp, err := a.api.SendLink(receiver, data.Title, data.Desc, data.Url, data.Xml)
		if err != nil {
			return nil, err
		}
		result.NewId = resp.GetNewId()
		result.CreateTime = uint32(resp.GetCreateTime())
		clientID = numericClientID(resp.GetClientId())

	default:
		return nil, sdkroot.NotImplemented(fmt.Sprintf("message.Send: type=%d", msg.GetType().GetCode()))
	}
	if result.NewId == 0 {
		return nil, errors.New("消息回执缺少消息 ID，结果未确认，请勿自动重试")
	}
	canRevoke := a.receipts.remember(receiver, result.NewId, clientID, uint64(result.CreateTime))
	slog.Info(fmt.Sprintf("[%s] -> %s : %s", msg.GetType().GetDesc(), msg.GetReceiver().GetNickname(), msg.Content))

	// 自动撤回绑定当前连接和 context，不能在下一次 Run 操作另一个账号。
	if msg.Timestamp != 0 && !canRevoke {
		slog.Warn("消息已发送，但回执缺少撤回字段，未安排自动撤回")
	}
	if msg.Timestamp != 0 && canRevoke {
		conn := coreapi.GetCore()
		scoped := api.New(conn)
		ctx := conn.Context()
		newID, created := result.NewId, result.CreateTime
		timer := time.NewTimer(time.Duration(msg.Timestamp) * time.Second)
		go func() {
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			if ctx.Err() != nil {
				return
			}
			if _, err := scoped.Revoke(receiver, newID, clientID, uint64(created)); err != nil {
				slog.Warn("消息撤回失败", "err", err)
			}
		}()
	}

	return &result, nil
}

// Forward 转发消息（根据类型调用对应转发 API）
func (a *ability) Forward(msg *sdk.Message, receiver string) (*sdk.Forward_Response, error) {
	if err := ensureOutboundReady(); err != nil {
		return nil, err
	}
	if msg == nil || receiver == "" {
		return nil, errors.New("消息和转发接收人不可为空")
	}
	switch msg.GetType().GetCode() {
	case sdk.TypeImage.Code:
		raw, err := messageXML(msg)
		if err != nil {
			return nil, err
		}
		resp, err := a.api.ForwardImage(receiver, strings.NewReader(raw))
		if err != nil {
			return nil, err
		}
		if resp.GetNewId() == 0 {
			return nil, errors.New("转发回执缺少消息 ID，结果未确认，请勿自动重试")
		}
		a.receipts.remember(receiver, resp.GetNewId(), numericClientID(resp.GetClientId().GetValue()), uint64(resp.GetCreateTime()))
		return &sdk.Forward_Response{NewId: resp.GetNewId(), CreateTime: resp.GetCreateTime()}, nil
	case sdk.TypeVideo.Code, sdk.TypeTinyVideo.Code:
		raw, err := messageXML(msg)
		if err != nil {
			return nil, err
		}
		resp, err := a.api.ForwardVideo(receiver, strings.NewReader(raw))
		if err != nil {
			return nil, err
		}
		if resp.GetNewId() == 0 {
			return nil, errors.New("转发回执缺少消息 ID，结果未确认，请勿自动重试")
		}
		return &sdk.Forward_Response{NewId: resp.GetNewId()}, nil
	case sdk.TypeAppFileAttach.Code:
		raw, err := messageXML(msg)
		if err != nil {
			return nil, err
		}
		resp, err := a.api.ForwardFile(receiver, raw)
		if err != nil {
			return nil, err
		}
		if resp.GetNewId() == 0 {
			return nil, errors.New("转发回执缺少消息 ID，结果未确认，请勿自动重试")
		}
		if resp.GetCreateTime() > 0 {
			a.receipts.remember(receiver, resp.GetNewId(), numericClientID(resp.GetClientId()), uint64(resp.GetCreateTime()))
		}
		return &sdk.Forward_Response{NewId: resp.GetNewId(), CreateTime: uint32(resp.GetCreateTime())}, nil
	default:
		copy := proto.Clone(msg).(*sdk.Message)
		copy.Receiver = &contact.Contact{Username: receiver}
		copy.Timestamp = 0 // 接收时间不是自动撤回延迟。
		resp, err := a.Send(copy)
		if err != nil {
			return nil, err
		}
		if resp.GetNewId() == 0 {
			return nil, errors.New("转发回执缺少消息 ID，结果未确认，请勿自动重试")
		}
		return &sdk.Forward_Response{NewId: resp.GetNewId(), CreateTime: resp.GetCreateTime()}, nil
	}
}

func messageXML(msg *sdk.Message) (string, error) {
	raw := msg.GetApp().GetXml()
	if raw == "" && msg.GetRaw() != "" {
		var source api.NewMessage
		if err := json.Unmarshal([]byte(msg.GetRaw()), &source); err != nil {
			return "", errors.New("原始消息无法解析")
		}
		raw = source.GetContent().GetValue()
	}
	if parts := strings.SplitN(raw, ":\n", 2); len(parts) == 2 {
		raw = parts[1]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("转发/下载缺少原始消息 XML")
	}
	return raw, nil
}

// Revoke 撤回消息
func (a *ability) Revoke(receiver string, newMsgId uint64) (*sdk.Revoke_Response, error) {
	if receiver == "" || newMsgId == 0 {
		return nil, errors.New("撤回需要接收人 ID 和原发送回执的消息 ID")
	}
	receipt, ok := a.receipts.get(receiver, newMsgId)
	if !ok {
		return nil, sdk.ErrRevokeReceiptUnavailable
	}
	resp, err := a.api.Revoke(receiver, newMsgId, receipt.clientID, receipt.created)
	if err != nil {
		return nil, err
	}
	if resp.GetBaseResponse() == nil || resp.GetBaseResponse().Code == nil {
		return nil, errors.New("撤回回执不完整，请核对消息状态")
	}
	if resp.GetBaseResponse().GetCode() != 0 {
		return nil, fmt.Errorf("消息撤回失败，返回码 %d", resp.GetBaseResponse().GetCode())
	}
	return &sdk.Revoke_Response{Code: 0}, nil
}

// Download 下载媒体资源
func (a *ability) Download(msg *sdk.Message) (io.ReadCloser, error) {
	if msg == nil {
		return nil, errors.New("消息不可为空")
	}
	receiver := msg.GetSender().GetUsername()
	switch msg.GetType().GetCode() {
	case sdk.TypeImage.Code:
		media := msg.GetImage().GetMedia()
		if media.GetUrl() == "" || media.GetKey() == "" {
			return nil, errors.New("缺少图片 CDN 地址或密钥")
		}
		resp, err := a.api.DownloadImg(receiver, media.GetUrl(), media.GetKey(), media.GetSize())
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(resp.GetChunk().GetData())), nil
	case sdk.TypeVideo.Code, sdk.TypeTinyVideo.Code:
		media := msg.GetVideo().GetMedia()
		if media.GetUrl() == "" || media.GetKey() == "" {
			return nil, errors.New("缺少视频 CDN 地址或密钥")
		}
		resp, err := a.api.DownloadVideo(receiver, media.GetUrl(), media.GetKey(), media.GetSize())
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(resp.GetChunk().GetData())), nil
	case sdk.TypeVoice.Code:
		data := msg.GetVoice()
		if msg.GetId() == 0 || data.GetMedia().GetSize() == 0 {
			return nil, errors.New("语音下载缺少消息 ID 或长度")
		}
		resp, err := a.api.DownloadVoice(receiver, strconv.FormatUint(uint64(msg.GetId()), 10), "", data.GetMedia().GetSize(), data.GetDuration())
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(resp.GetData().GetData())), nil
	case sdk.TypeAppFileAttach.Code:
		raw, err := messageXML(msg)
		if err != nil {
			return nil, err
		}
		var item struct {
			App struct {
				Attach struct {
					ID   string `xml:"attachid"`
					Size uint32 `xml:"totallen"`
				} `xml:"appattach"`
			} `xml:"appmsg"`
		}
		if err = xml.Unmarshal([]byte(raw), &item); err != nil {
			return nil, errors.New("文件消息 XML 无法解析")
		}
		if item.App.Attach.ID == "" || item.App.Attach.Size == 0 {
			return nil, errors.New("文件消息缺少附件 ID 或长度")
		}
		resp, err := a.api.DownloadFile(receiver, item.App.Attach.ID, "", item.App.Attach.Size)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(resp.GetChunk().GetData())), nil
	default:
		return nil, sdkroot.NotImplemented(fmt.Sprintf("message.Download: type=%d", msg.GetType().GetCode()))
	}
}

// SupportsFileSend advertises the optional local-file extension to the SDK.
func (a *ability) SupportsFileSend() bool { return true }
