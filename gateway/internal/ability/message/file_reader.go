package messageability

import (
	"context"
	"fmt"
	"io"

	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	sdk "github.com/we-clay/we-clay/sdk/message"
)

// SendFileReader 接收进程内文件流，并保存真实原始回执以支持撤回。
func SendFileReader(ctx context.Context, receiver, filename string, reader io.Reader) (*sdk.Send_Response, error) {
	if err := ensureOutboundReady(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, e := messageapi.SendFileContext(ctx, receiver, filename, reader)
	if e != nil {
		return nil, e
	}
	if r == nil || r.GetNewId() == 0 {
		return nil, fmt.Errorf("文件发送回执缺少消息 ID，结果未确认，请勿自动重试")
	}
	if !instance.receipts.remember(receiver, r.GetNewId(), numericClientID(r.GetClientId()), uint64(r.GetCreateTime())) {
		instance.receipts.rememberString(receiver, r.GetNewId(), r.GetClientId(), uint64(r.GetCreateTime()))
	}
	return &sdk.Send_Response{NewId: r.GetNewId(), CreateTime: uint32(r.GetCreateTime())}, nil
}
