package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/plugins/contextcleaner"
)

// 模型流水线只负责选定首个结果。进入工具循环后固定模型，绝不重跑已发生的副作用。
func (s *Server) runActionLoop(ctx context.Context, id string, q ai.Request, c ai.Config, result ai.Result, key string) (final ai.Result, sent bool, failure error) {
	session := newActionSession(s, id, q)
	input, output := result.InputTokens, result.OutputTokens
	singleConfirmation := ""
	writeRejected := false
	defer func() {
		final.InputTokens, final.OutputTokens = input, output
	}()
	// 只让无副作用的模型生成失败支持手动重试；安全、权限和证据错误不进入重试。
	recoveryFailure := func(err error) error {
		if writeRejected && !session.writeStarted && !session.sentCurrent && ctx.Err() == nil {
			return replyModelFailure{err}
		}
		return err
	}
	ctx = s.debugContext(ctx, q.AccountID, q.AccountEpoch, q.Target, id, c.Model)
	for step := 0; len(result.ToolCalls) > 0; step++ {
		if writeRejected {
			return result, session.sentCurrent, recoveryFailure(fmt.Errorf("模型纠正后仍请求工具，已停止回复"))
		}
		if step >= 12 {
			return result, session.sentCurrent, fmt.Errorf("已达到12步操作上限；已完成步骤已保留，未重复执行")
		}
		if q.Source != "wechat" || q.Conversation == nil {
			return result, false, fmt.Errorf("当前入口不允许模型执行微信操作")
		}
		if len(result.ToolCalls) != 1 {
			return result, false, fmt.Errorf("每次只允许一个工具调用，已停止执行")
		}
		call := result.ToolCalls[0]
		if !slices.ContainsFunc(q.Tools, func(t ai.Tool) bool { return t.Name == call.Name }) {
			return result, false, fmt.Errorf("模型请求了本轮未提供的操作")
		}
		if err := session.live(); err != nil {
			return result, false, err
		}
		var data any
		var err error
		if call.Name == "actions__Discover" {
			var offered ai.Tool
			for _, t := range q.Tools {
				if t.Name == call.Name {
					offered = t
				}
			}
			var args map[string]any
			args, err = validateActionArgs(call.Arguments, offered.Parameters)
			names := argRefs(args, "operations")
			if len(names) == 0 || len(names) > 8 {
				err = fmt.Errorf("每次需选择1至8个工具")
			}
			if err == nil {
				inventory := s.behaviorToolInventory(q)
				for _, name := range names {
					index := slices.IndexFunc(inventory, func(t ai.Tool) bool { return t.Name == name })
					if index < 0 {
						err = fmt.Errorf("请求接入的工具未授权")
						break
					}
					if !slices.ContainsFunc(q.Tools, func(t ai.Tool) bool { return t.Name == name }) {
						q.Tools = append(q.Tools, inventory[index])
					}
				}
			}
			data = map[string]any{"enabled_tools": names}
		} else {
			data, err = session.execute(ctx, call)
		}
		if err != nil {
			var missing writeIntentMissing
			var invalidRef actionReferenceError
			if errors.As(err, &missing) || errors.As(err, &invalidRef) && !session.writeStarted && !session.sentCurrent {
				// 拒绝写入后只给一次正常回复机会，不把内部错误当聊天正文发送。
				writeRejected = true
				singleConfirmation = ""
				data = map[string]any{"status": "denied", "code": "current_write_intent_missing", "executed": false, "message": "当前原始消息没有授权此次资料修改，此操作未执行。不要继续调用工具，不要把拒绝提示当正文发送。请重新理解当前发言，正常接续本轮话题；只有用户本轮确实提出修改请求但信息不足时，才针对缺少的信息简短澄清。"}
				if errors.As(err, &invalidRef) {
					data = map[string]any{"status": "invalid_reference", "executed": false, "message": "工具引用不存在或类型错误，此操作未执行。不要继续调用工具，不要编造已撤回、已发送或其他操作结果。请以本人身份正常接续本轮话题；如确实缺少执行条件，简短说明。"}
				}
				if logErr := s.store.change(func(st *State) error {
					addLog(st, "WARN", "ai", "操作未执行（"+err.Error()+"）："+call.Name+"；任务 "+id+"，反馈模型纠正回复，未执行该操作")
					st.Logs[len(st.Logs)-1].Target = q.Target
					return nil
				}); logErr != nil {
					return result, session.sentCurrent, logErr
				}
			} else {
				return result, session.sentCurrent, err
			}
		}
		if step == 0 && session.verifiedOperation {
			switch call.Name {
			case "chatroom__SetName":
				singleConfirmation = "已更新群名。"
			case "chatroom__SetAnnouncement":
				singleConfirmation = "已更新群公告。"
			case "contact__SetRemark":
				singleConfirmation = "已更新备注。"
			}
		} else {
			singleConfirmation = ""
		}
		if session.pending {
			return ai.Result{Text: "操作已提交后台确认，尚未执行。", InputTokens: input, OutputTokens: output}, session.sentCurrent, nil
		}
		raw, err := json.Marshal(data)
		if err != nil {
			return result, session.sentCurrent, err
		}
		if err = s.checkActionData(id, q, raw); err != nil {
			return result, session.sentCurrent, err
		}
		if len(raw) > 64<<10 {
			return result, session.sentCurrent, fmt.Errorf("操作结果超过上下文容量，请缩小查询范围；已执行步骤不会重放")
		}
		q.ToolHistory = append(q.ToolHistory, ai.ToolExchange{Result: result, Outputs: []ai.ToolOutput{{ID: call.ID, Name: call.Name, Content: string(raw)}}})
		// 纠正时已禁止继续操作，移除工具描述以节约容量；保留真实工具回执。
		if writeRejected {
			q.Tools = nil
			q.PlainReply = true
			q.System += "\n本轮操作未执行。仅用本人第一人称正常回复或 NO_REPLY，禁止继续调用工具和媒体标签。"
		}
		// 尚无写入时可以裁剪较早聊天；执行后不删除执行证据。
		history, _ := json.Marshal(q.ToolHistory)
		budget := settingNumber(s.store.snapshot(), "context_cleaner.max_input")
		if !session.writeStarted && !session.sentCurrent && c.ContextBudget > 0 {
			for len(q.Context) > 0 && ai.ConversationSize(q)+contextcleaner.Estimate(string(history)) > c.ContextBudget {
				q.Context = q.Context[1:]
			}
		}
		if c.ContextBudget > 0 && ai.ConversationSize(q)+contextcleaner.Estimate(string(history)) > c.ContextBudget {
			return result, session.sentCurrent, recoveryFailure(fmt.Errorf("连续操作超过该模型的上下文预算，已完成步骤保留，请提高模型上下文容量或缩小查询范围"))
		}
		size := contextcleaner.Estimate(q.System) + ai.ConversationSize(q) + contextcleaner.Estimate(string(history)) + c.MaxOutput
		for _, tool := range q.Tools {
			raw, _ := json.Marshal(tool.Parameters)
			size += contextcleaner.Estimate(tool.Name+tool.Description+string(raw)) + 32
		}
		if budget > 0 && size > budget {
			return result, session.sentCurrent, fmt.Errorf("连续操作超过请求容量，已完成步骤保留，请缩小查询范围")
		}
		if err := ctx.Err(); err != nil {
			return result, session.sentCurrent, err
		}
		result, err = ai.Execute(ctx, c, q, s.cfg.Mock)
		if err != nil {
			return result, session.sentCurrent, recoveryFailure(fmt.Errorf("操作后的模型回复失败；已执行步骤不会重试：%w", err))
		}
		input += result.InputTokens
		output += result.OutputTokens
		checked, report, err := filterSafetyOutput(s.store.snapshot(), q, result, key)
		if auditErr := s.recordSafety(id, q.Source, "output", report); auditErr != nil {
			return result, session.sentCurrent, auditErr
		}
		if err != nil {
			return result, session.sentCurrent, err
		}
		result = checked
		if len(result.ToolCalls) == 0 {
			if !session.verifiedOperation {
				if err = replyOperationError(q, result); err != nil {
					return result, session.sentCurrent, recoveryFailure(err)
				}
			}
			if err = replyQualityError(result.Text, q.Conversation); err != nil {
				return result, session.sentCurrent, recoveryFailure(err)
			}
			if !report.Silent {
				if err = replyPlanError(s.store.snapshot(), q, result); err != nil {
					return result, session.sentCurrent, recoveryFailure(err)
				}
			}
			if strings.TrimSpace(result.Text) == "" && !report.Silent {
				return result, session.sentCurrent, recoveryFailure(fmt.Errorf("操作后未返回有效正文"))
			}
		}
	}
	// 单项资料写入沿用已验收的固定回执，避免模型在成功后虚构额外操作。
	if singleConfirmation != "" {
		result.Text = singleConfirmation
	}
	result.InputTokens, result.OutputTokens = input, output
	return result, session.sentCurrent, nil
}
