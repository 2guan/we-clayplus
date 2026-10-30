// 主要功能：把实际模型传输与系统操作写入 DEBUG 日志，不保存认证头或操作票据。
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/internal/debuglog"
	"github.com/we-clay/we-clay/sdk/control"
)

func (s *Server) debugRecord(account string, epoch uint64, target, title string, fields map[string]any) {
	var message string
	err := s.store.change(func(st *State) error {
		if settingText(*st, "logging.level") != "DEBUG" || st.Account.ID != account || s.store.accountEpoch != epoch {
			return nil
		}
		metadata := make(map[string]any, len(fields))
		for k, v := range fields {
			if k != "body" {
				metadata[k] = v
			}
		}
		raw, err := json.MarshalIndent(metadata, "", "  ")
		if err != nil {
			return err
		}
		message = title + "\n" + string(raw)
		if body, ok := fields["body"].(string); ok {
			message += "\n原文：\n" + body
		}
		addLog(st, "DEBUG", "debug", message)
		st.Logs[len(st.Logs)-1].Target = target
		return nil
	})
	if err != nil {
		slog.Warn("调试日志写入失败")
	}
	if message != "" {
		slog.Debug(title, "detail", message)
		s.notify()
	}
}

func (s *Server) debugContext(ctx context.Context, account string, epoch uint64, target, task, model string) context.Context {
	if settingText(s.store.snapshot(), "logging.level") != "DEBUG" {
		return ctx
	}
	return debuglog.WithSink(ctx, func(title string, fields map[string]any) {
		fields["task"], fields["model"] = task, model
		s.debugRecord(account, epoch, target, title, fields)
	})
}

type debugHost struct {
	control.Host
	s *Server
}

func (h debugHost) CoreUptime(ctx context.Context) (time.Duration, error) {
	if source, ok := h.Host.(control.CoreRuntimeHost); ok {
		return source.CoreUptime(ctx)
	}
	return 0, fmt.Errorf("宿主未提供 core 运行时间")
}

func (h debugHost) Execute(ctx context.Context, command control.Command) (any, error) {
	operations := map[string]string{"send": "发送消息", "revoke": "撤回消息", "forward": "转发消息", "remark": "修改备注", "group-name": "修改群名", "announcement": "修改群公告", "moment-like": "朋友圈点赞", "moment-unlike": "取消朋友圈点赞", "moment-comment": "朋友圈评论", "moment-post": "发布朋友圈", "moments-like": "朋友圈点赞", "moments-comment": "朋友圈评论", "moments-publish": "发布朋友圈", "moments-cover": "修改朋友圈封面", "friend-verify": "好友验证", "payment-claim": "收款"}
	if command.Operation == "ai-typing" {
		operations["ai-typing"] = "输入状态"
	}
	if command.Operation == "ai-action" {
		operations["ai-action"] = command.ID
	}
	st := h.s.store.snapshot()
	if settingText(st, "logging.level") != "DEBUG" || operations[command.Operation] == "" {
		return h.Host.Execute(ctx, command)
	}
	fields := map[string]any{"operation_id": randomID(), "operation": command.Operation, "name": operations[command.Operation], "target": command.Target, "id": command.ID, "content": command.Content}
	if command.Operation == "send" {
		var params struct {
			Mentions []string `json:"mentions"`
			QuoteXML string   `json:"quote_xml"`
		}
		if json.Unmarshal(command.Params, &params) == nil {
			fields["mentions"] = params.Mentions
			fields["native_quote"] = params.QuoteXML != ""
		}
	}
	h.s.debugRecord(st.Account.ID, st.AccountEpoch, command.Target, "系统操作开始", fields)
	result, err := h.Host.Execute(ctx, command)
	fields["status"] = "succeeded"
	if err != nil {
		publicError := err.Error()
		if command.Operation == "friend-verify" || command.Operation == "ai-action" && command.ID == "contact.VerifyFriend" {
			var q struct{ V1, V2 string }
			_ = json.Unmarshal(command.Params, &q)
			for _, ticket := range []string{q.V1, q.V2} {
				if ticket != "" {
					publicError = strings.ReplaceAll(publicError, ticket, "[验证凭据已隐藏]")
				}
			}
		}
		fields["status"], fields["error"] = "failed", publicError
	}
	// 不复制原始回执和 Params；实际返回值仍完整交给原有调用链。
	h.s.debugRecord(st.Account.ID, st.AccountEpoch, command.Target, "系统操作结果", fields)
	return result, err
}
