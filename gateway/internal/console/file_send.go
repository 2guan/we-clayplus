package console

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	messageability "github.com/we-clay/we-clay/gateway/internal/ability/message"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
)

func checkFileInput(f *control.FileInput) error {
	if f == nil || f.Size <= 0 {
		return fmt.Errorf("文件不能为空")
	}
	if f.Size > messageapi.MaxFileSendBytes {
		return fmt.Errorf("上层附件配置已生效，但当前 core 发送链路最多支持 %d MB；此文件尚未发送，需要先升级 core 的大文件能力", messageapi.MaxFileSendBytes>>20)
	}
	return nil
}

func (b *Bridge) sendFileInput(ctx context.Context, c control.Command) (any, error) {
	if e := checkFileInput(c.File); e != nil {
		return nil, e
	}
	var p struct {
		Kind     string   `json:"kind"`
		Media    string   `json:"media"`
		QuoteXML string   `json:"quote_xml"`
		Mentions []string `json:"mentions"`
	}
	if json.Unmarshal(c.Params, &p) != nil || p.Kind != "file" || p.Media != "" || p.QuoteXML != "" || len(p.Mentions) != 0 || c.File.Reader == nil || c.Target == "" || c.Content == "" || strings.ContainsAny(c.Content, "/\\\r\n\x00") {
		return nil, fmt.Errorf("文件输入无效或混用了其他消息类型")
	}
	end, e := c.File.Reader.Seek(0, 2)
	if e != nil || end != c.File.Size {
		return nil, fmt.Errorf("文件内容与声明大小不一致")
	}
	if _, e = c.File.Reader.Seek(0, 0); e != nil {
		return nil, e
	}
	r, e := messageability.SendFileReader(ctx, c.Target, c.Content, c.File.Reader)
	if e != nil {
		return nil, e
	}
	out := map[string]string{"id": strconv.FormatUint(r.GetNewId(), 10)}
	if client, original, created, ok := messageability.ReceiptFields(c.Target, r.GetNewId()); ok {
		if original != "" {
			out["original_client_id"] = original
		} else {
			out["client_id"] = strconv.FormatUint(client, 10)
		}
		out["create_time"] = strconv.FormatUint(created, 10)
		out["account"] = contact.Instance.GetSelf().GetUsername()
	}
	return out, nil
}
