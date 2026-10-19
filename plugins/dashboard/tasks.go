// 文件名：tasks.go
// 主要功能：运行 AI 实验室任务、记录用量，并处理有白名单限制的自动回复。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
)

func (s *Server) createTask(w http.ResponseWriter, r *http.Request, u User) {
	var q struct {
		ai.Request
		Insight *InsightInfo `json:"insight,omitempty"`
	}
	if !decode(w, r, &q) {
		return
	}
	if strings.TrimSpace(q.Prompt) == "" {
		writeError(w, 400, "请输入测试内容或分析要求")
		return
	}
	q.Source = "dashboard"
	// HTTP 传入的人设补充是待参考文本，不能自称高优先级系统消息。
	if q.System != "" {
		q.Prompt += "\n本次人设补充：\n" + q.System
		q.System = ""
	}
	if q.Kind == "insight" {
		q.Source = "analysis"
	}
	// 旧入口只接受展示类型和原提示词；快照、父报告及重试关系必须由专用服务端入口建立。
	if q.Insight != nil {
		q.Insight = &InsightInfo{Type: q.Insight.Type, Prompt: q.Insight.Prompt}
	}
	// 后台测试与分析不因传入 target 而获得自动发送或工具执行权。
	id, err := s.queueTask(q.Request, q.Insight)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 202, map[string]string{"id": id})
}
func (s *Server) queue(q ai.Request) (string, error) {
	return s.queueTask(q, nil)
}

func (s *Server) queueTask(q ai.Request, insight *InsightInfo) (string, error) {
	return s.queueTaskRetry(q, insight, "")
}

func (s *Server) queueTaskRetry(q ai.Request, insight *InsightInfo, retryOf string) (string, error) {
	if s.cfg.DisableAI {
		return "", fmt.Errorf("AI 已在启动配置中停用")
	}
	if !slices.Contains([]string{"chat", "vision", "audio", "video", "tts", "image", "insight", "video_generation"}, q.Kind) {
		return "", fmt.Errorf("未知 AI 能力")
	}
	if q.SpeechMode != "" && !slices.Contains([]string{"tts", "design", "clone"}, q.SpeechMode) {
		return "", fmt.Errorf("语音模式无效")
	}
	state := s.store.snapshot()
	if q.AccountID != "" && q.AccountID != state.Account.ID {
		return "", fmt.Errorf("账号已切换，停止旧任务")
	}
	if (q.Conversation != nil || q.Source == "memory-scan") && q.AccountEpoch != state.AccountEpoch {
		return "", fmt.Errorf("账号会话已更新，停止旧任务")
	}
	q.AccountID = state.Account.ID
	q.AccountEpoch = state.AccountEpoch
	if insight != nil {
		if q.Kind != "insight" || !slices.Contains([]string{"conversation", "group", "moments"}, insight.Type) || strings.TrimSpace(insight.Prompt) == "" || len(insight.Prompt) > 64<<10 {
			return "", fmt.Errorf("分析报告类型或提示词无效")
		}
		// 名称从当前账号的通讯录获取，不信任客户端提供的展示名称。
		info := *insight
		for _, c := range state.Contacts {
			if c.ID == q.Target {
				info.TargetName = c.Name
				if c.Remark != "" {
					info.TargetName = c.Remark
				}
				break
			}
		}
		insight = &info
	}
	if !slices.ContainsFunc(state.Plugins, func(p Plugin) bool { return p.ID == "ai" && p.Enabled }) {
		return "", fmt.Errorf("AI 插件已停用")
	}
	if q.Kind == "tts" && len([]rune(q.Prompt)) > int(s.number("tts.max_chars", 500)) {
		return "", fmt.Errorf("合成文字超过配置上限")
	}
	if q.Media != "" {
		data, e := base64.StdEncoding.DecodeString(q.Media)
		if e != nil || len(data) > 16<<20 {
			return "", fmt.Errorf("媒体内容无效或超过 16 MB")
		}
		if q.Kind == "vision" && float64(len(data)) > s.number("vision.max_mb", 10)*1024*1024 {
			return "", fmt.Errorf("图片超过配置大小上限")
		}
	}
	totalImages := 0
	for _, im := range q.Images {
		data, e := base64.StdEncoding.DecodeString(im.Data)
		totalImages += len(data)
		if q.Kind != "insight" || e != nil || len(data) == 0 || totalImages > 12<<20 || len(im.Label) > 200 || !slices.Contains([]string{"image/jpeg", "image/png", "image/gif", "image/webp"}, im.MIME) || http.DetectContentType(data) != im.MIME {
			return "", fmt.Errorf("分析缩略图无效或总量超过 12 MB，请减少朋友圈条数")
		}
	}
	var safetyErr error
	q, safetyErr = s.prepareSafety(q)
	if safetyErr != nil {
		return "", safetyErr
	}
	s.taskMu.Lock()
	// 限额检查和入队共用锁，避免并发请求同时穿过每日上限。
	today := time.Now().Format("2006-01-02")
	count := 0
	for _, t := range s.store.snapshot().Tasks {
		if wechatReplySource(q.Source) && q.Target != "" && t.Target == q.Target && (t.Status == "queued" || t.Status == "running") {
			s.taskMu.Unlock()
			return "", fmt.Errorf("当前会话正在处理回复，请等待完成")
		}
		if strings.HasPrefix(t.Created, today) {
			count++
		}
	}
	if limit := int(s.number("routing.daily_calls", 200)); limit > 0 && count >= limit {
		s.taskMu.Unlock()
		return "", fmt.Errorf("已达到每日请求上限")
	}
	if len(s.taskCancel) >= int(s.number("routing.concurrency", 2)) {
		s.taskMu.Unlock()
		return "", fmt.Errorf("正在处理其他任务，请等待完成或取消后重试")
	}
	ctx, cancel := context.WithCancel(s.ctx)
	id := randomID()
	s.taskCancel[id] = cancel
	t := Task{Source: q.Source, ID: id, Kind: q.Kind, Name: map[string]string{"chat": "对话测试", "vision": "图片理解", "audio": "音频识别", "video": "视频理解", "tts": "语音合成", "image": "图片生成", "video_generation": "视频生成", "insight": "会话洞察"}[q.Kind], Status: "queued", Created: time.Now().Format(time.RFC3339), Input: q.Prompt, Target: q.Target}
	t.RetryOf = retryOf
	if q.Source == "wechat-proactive" {
		t.Name = "主动说一句"
	}
	if q.Conversation != nil {
		t.ReplyTo = q.Conversation.Current.ID
	}
	if q.Source == "memory-scan" {
		t.Name = "记忆候选抽取"
		t.Input = "扫描资料仅用于当前任务，不在任务列表公开"
	}
	if insight != nil && insight.Managed && insight.RootID == "" {
		insight.RootID = id
	}
	if insight != nil && insight.Question != "" && insight.TurnID == "" {
		insight.TurnID = id
	}
	t.Insight = insight
	if insight != nil {
		t.Name = map[string]string{"conversation": "会话总结", "group": "群聊分析", "moments": "朋友圈内容"}[insight.Type]
	}
	if err := s.store.change(func(v *State) error {
		if v.Account.ID != q.AccountID || s.store.accountEpoch != q.AccountEpoch {
			return fmt.Errorf("账号已切换，请刷新后重试")
		}
		if retryOf != "" {
			index := slices.IndexFunc(v.Tasks, func(old Task) bool {
				return old.ID == retryOf && old.Target == q.Target && old.Status == "failed" && old.ReplyRetryable
			})
			if index < 0 {
				return fmt.Errorf("该回复已重试或不允许重新生成，请刷新")
			}
			for j := index + 1; j < len(v.Tasks); j++ {
				if v.Tasks[j].Target == q.Target && wechatReplySource(v.Tasks[j].Source) && v.Tasks[j].Kind == "chat" {
					return fmt.Errorf("该会话已有更新的回复，请刷新")
				}
			}
			v.Tasks[index].ReplyRetryable = false
		}
		v.Tasks = append(v.Tasks, t)
		return nil
	}); err != nil {
		cancel()
		delete(s.taskCancel, id)
		s.taskMu.Unlock()
		return "", err
	}
	if q.Kind == "insight" {
		s.startTaskStream(id)
	}
	s.taskMu.Unlock()
	s.notify()
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		defer cancel()
		defer func() { s.taskMu.Lock(); delete(s.taskCancel, id); s.taskMu.Unlock() }()
		if q.Kind == "insight" {
			defer s.finishTaskStream(id)
		}
		if insight != nil && insight.Managed {
			workInfo := *insight
			s.updateTask(id, func(t *Task) { t.Status = "running" })
			if err := s.prepareReport(ctx, id, &q, &workInfo); err != nil {
				s.updateTask(id, func(t *Task) { t.Status = "failed"; t.Error = err.Error() })
				_ = s.store.change(func(v *State) error {
					addLog(v, "ERROR", "ai", "报告资料准备失败；任务 "+id+"："+err.Error())
					return nil
				})
				s.notify()
				return
			}
		}
		s.runTask(ctx, id, q)
	}()
	return id, nil
}
func (s *Server) runTask(ctx context.Context, id string, q ai.Request) {
	if active := s.store.snapshot(); q.AccountID != "" && (active.Account.ID != q.AccountID || active.AccountEpoch != q.AccountEpoch) {
		s.updateTask(id, func(t *Task) { t.Status = "failed"; t.Error = "账号已切换，停止旧任务" })
		return
	}
	s.updateTask(id, func(t *Task) {
		if t.Status == "queued" {
			t.Status = "running"
		}
	})
	if q.InputID != "" {
		var incomingErr error
		q, incomingErr = s.prepareIncoming(ctx, q)
		if incomingErr != nil {
			var modelErr replyModelFailure
			s.updateTask(id, func(t *Task) {
				t.Status = "failed"
				t.Error = incomingErr.Error()
				t.ReplyRetryable = q.Conversation != nil && errors.As(incomingErr, &modelErr)
			})
			_ = s.store.change(func(st *State) error { addLog(st, "ERROR", "ai", incomingErr.Error()); return nil })
			s.notify()
			return
		}
	}
	state := s.store.snapshot()
	capability := q.Kind
	if capability == "insight" {
		capability = "chat"
		if len(q.Images) > 0 {
			capability = "vision"
		}
	}
	modelID := q.Model
	if modelID == "" {
		modelID, _ = state.Settings["routing."+capability].(string)
	}
	candidates := []Model{}
	for _, m := range state.Models {
		if m.Enabled && slices.Contains(m.Capabilities, capability) {
			candidates = append(candidates, m)
		}
	}
	// 指定模型不切换供应商；微信回复的失败补试仍使用同一模型。
	if pipelineCapability(capability) && q.Model == "" {
		ordered := []Model{}
		for _, id := range modelPipeline(state, capability) {
			for _, m := range candidates {
				if m.ID == id {
					ordered = append(ordered, m)
				}
			}
		}
		candidates = ordered
	} else if modelID != "" {
		index := slices.IndexFunc(candidates, func(m Model) bool { return m.ID == modelID })
		if index < 0 {
			candidates = nil
		} else if q.Model != "" {
			candidates = []Model{candidates[index]}
		} else {
			preferred := candidates[index]
			candidates = append([]Model{preferred}, append(candidates[:index:index], candidates[index+1:]...)...)
		}
	}

	if capability == "tts" && len(candidates) > 0 {
		mode := q.SpeechMode
		if mode == "" {
			mode = speechMode(candidates[0].Model)
		}
		candidates = slices.DeleteFunc(candidates, func(m Model) bool { return speechMode(m.Model) != mode })
	}
	fallback := pipelineCapability(capability) && q.Model == "" && state.Settings["routing.fallback"] == true
	if !fallback && len(candidates) > 1 {
		candidates = candidates[:1]
	}
	if q.Kind == "video_generation" {
		if q.Size == "" {
			q.Size, _ = state.Settings["video_generation.size"].(string)
		}
		if q.Seconds == "" {
			q.Seconds, _ = state.Settings["video_generation.seconds"].(string)
		}
		if style, ok := state.Settings["video_generation.style"].(string); ok && style != "" {
			q.Prompt += "\n风格补充：" + style
		}
	}
	if q.Kind == "image" {
		if style, ok := state.Settings["image.style"].(string); ok && style != "" {
			q.Prompt += "\n风格补充：" + style
		}
	}
	var result ai.Result
	var actualRequest ai.Request
	var err error
	used := ""
	usedKey := ""
	silent := false
	outputChecked := false
	modelFailed := false
	autoRetries := 0
	if len(candidates) == 0 {
		err = fmt.Errorf("请先配置支持此能力的模型")
	}
	if q.System == "" && len(state.Prompts) > 0 {
		q.System = state.Prompts[0].Content
	}
	if wechatReplySource(q.Source) && q.Kind == "chat" {
		inputKind := q.InputKind
		if q.Source == "wechat-proactive" {
			inputKind = "proactive"
		}
		q.System += replyInstructions(state, q.Prompt, inputKind)
		if strings.Contains(q.Prompt, "表情") {
			q.System += s.stickerInstructions(q.Target, q.Prompt)
		}
		// 普通回复不用发送工具，但发送策略的提示词仍进入实际系统消息。
		for _, policy := range state.Policies {
			if policy.Operation == "message.Send" && policy.Prompt != defaultBehavior("message.Send").Prompt && behaviorAllowed(state, policy.Operation, q.Target) == nil {
				q.System += "\n当前消息发送要求：" + policy.Prompt
			}
		}
	}
	for candidateIndex := 0; candidateIndex < len(candidates); candidateIndex++ {
		m := candidates[candidateIndex]
		modelFailed = false
		outputChecked = false
		used = m.Name
		s.updateTask(id, func(t *Task) { t.Model = used })
		key := ""
		if !s.cfg.Mock {
			secretID := m.VendorID
			if secretID == "" {
				secretID = m.ID
			}
			key, err = s.store.getSecret(secretID)
			if err != nil {
				err = fmt.Errorf("模型凭据尚未配置")
				if fallback {
					continue
				}
				break
			}
		}
		voice, _ := state.Settings["tts.voice"].(string)
		size, _ := state.Settings["image.size"].(string)
		if s.cfg.Mock && state.Scenario == "error" {
			err = fmt.Errorf("模拟模型超时：可以切换正常场景后重新创建任务")
			break
		}
		request := q
		if (wechatReplySource(q.Source) && q.Conversation != nil || q.Source == "reply-media") && !s.requestReplyAllowed(q) {
			err = fmt.Errorf("会话已暂停自动回复")
			break
		}
		if active := s.store.snapshot(); q.AccountID != "" && (active.Account.ID != q.AccountID || active.AccountEpoch != q.AccountEpoch) {
			err = fmt.Errorf("账号已切换，停止旧任务")
			break
		}
		if err = s.validateReportTask(id, request); err != nil {
			break
		}
		request, err = s.refreshReplyContext(request)
		if err != nil {
			break
		}
		if q.Kind == "tts" {
			request.SpeechMode = speechMode(m.Model)
		}
		if q.Source == "wechat" && q.Kind == "chat" && q.Target != "" && (m.Provider == "openai" || m.Provider == "mimo") {
			request.Tools = s.replyBehaviorTools(q.Target, q.Prompt)
			if len(request.Tools) > 0 {
				request.System += behaviorSystemRules
			}
		}
		timeout := int(s.number("routing.timeout", 300))
		if m.Timeout > 0 {
			timeout = m.Timeout
		}
		maxOutput := int(s.number("routing.max_output", 2048))
		if q.Kind == "insight" {
			maxOutput = int(s.number("insight.max_output", 8192))
		}
		if m.MaxOutput > 0 {
			maxOutput = m.MaxOutput
		}
		design, _ := state.Settings["tts.design"].(string)
		quality, _ := state.Settings["image.quality"].(string)
		config := ai.Config{Endpoint: m.Endpoint, Key: key, Model: m.Model, Provider: m.Provider, Voice: voice, Design: design, Size: size, Quality: quality, Temperature: m.Temperature, Thinking: m.Thinking, Speed: s.number("tts.speed", 1), Timeout: time.Duration(timeout) * time.Second, MaxOutput: maxOutput}
		if !s.cfg.Mock {
			err = s.prepareReference(ctx, state, &request)
			if err != nil {
				break
			}
		}
		// 按候选模型整理上下文；处理失败必须停止整个流水线。
		var report safetyReport
		request, report, err = materializeSafety(state, request, m, maxOutput)
		if auditErr := s.recordSafety(id, q.Source, "context", report); auditErr != nil {
			err = auditErr
		}
		if err != nil {
			break
		}
		if q.Source == "wechat-proactive" && len(request.Context) == 0 {
			err = fmt.Errorf("当前会话没有可用的近期上下文")
			break
		}
		if err = s.saveReplyTrace(id, request, m.ID, report.Changes); err != nil {
			break
		}
		usedKey = key
		actualRequest = request
		modelCtx := s.debugContext(ctx, q.AccountID, q.AccountEpoch, q.Target, id, m.Name)
		if q.Kind == "insight" {
			s.resetTaskStream(id)
			result, err = ai.ExecuteStream(modelCtx, config, request, s.cfg.Mock, func(delta string) { s.appendTaskStream(id, delta) })
		} else {
			result, err = ai.Execute(modelCtx, config, request, s.cfg.Mock)
		}
		if err == nil && wechatReplySource(q.Source) && q.Kind == "chat" {
			// 先执行不可绕过的安全检查；安全失败不能靠切换模型重试。
			outputChecked = true
			result, report, err = filterSafetyOutput(s.store.snapshot(), q, result, usedKey)
			silent = report.Silent
			if auditErr := s.recordSafety(id, q.Source, "output", report); auditErr != nil {
				err = auditErr
			}
			if err != nil {
				result = ai.Result{}
				break
			}
			if !silent {
				err = replyQualityError(result.Text)
				if err != nil {
					result = ai.Result{}
					_ = s.store.change(func(v *State) error {
						addLog(v, "WARN", "ai", "模型回复异常，未发送；任务 "+id+"，模型 "+used+"："+err.Error())
						v.Logs[len(v.Logs)-1].Target = q.Target
						return nil
					})
				}
			}
		}
		modelFailed = err != nil && ctx.Err() == nil
		// 完整流水线都失败后，才从首个模型重跑一次；总共最多两轮。
		if modelFailed && !ai.Refused(err) && candidateIndex == len(candidates)-1 && autoRetries == 0 && (wechatReplySource(q.Source) && q.Kind == "chat" || q.Source == "reply-media" && (q.Kind == "vision" || q.Kind == "audio")) {
			autoRetries++
			s.updateTask(id, func(t *Task) { t.AutoRetries = autoRetries })
			_ = s.store.change(func(v *State) error {
				addLog(v, "WARN", "ai", "整条模型流水线失败，开始第 2 轮（自动重试 1/1）；任务 "+id+"："+err.Error())
				return nil
			})
			candidates = append(candidates, candidates...)
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
			continue
		}
		// 成功即停止；指定模型、取消和已受理但状态未知的视频任务不触发新生成。
		if err == nil || ctx.Err() != nil || !fallback || ai.VideoJobPending(err) || ai.Refused(err) {
			break
		}

	}
	// 输出和工具参数先检查，再进入任何持久化或外部副作用。
	if !outputChecked && (err == nil || q.Kind == "insight" && result.Text != "") {
		var report safetyReport
		var outputErr error
		result, report, outputErr = filterSafetyOutput(s.store.snapshot(), q, result, usedKey)
		silent = report.Silent
		if auditErr := s.recordSafety(id, q.Source, "output", report); auditErr != nil {
			outputErr = auditErr
		}
		if outputErr != nil {
			err = outputErr
			result = ai.Result{}
		}
	}
	if sourceErr := s.validateReportTask(id, q); sourceErr != nil {
		err = sourceErr
		result = ai.Result{}
	}
	generated := err == nil
	if active := s.store.snapshot(); err == nil && q.AccountID != "" && (active.Account.ID != q.AccountID || active.AccountEpoch != q.AccountEpoch) {
		err = fmt.Errorf("账号已切换，结果已丢弃")
	}
	if err == nil && q.Conversation != nil {
		err = s.validateReplyEvidence(actualRequest)
	}
	if err == nil && q.Source == "memory-scan" {
		if ctx.Err() != nil {
			err = ctx.Err()
		} else {
			err = s.store.putSecret("memory-scan-result:"+q.AccountID+":"+id, result.Text)
		}
		result.Text = "已生成候选资料"
	}
	// 模型选定后再执行动作；动作结果未知或失败时不重新生成、不切换供应商。
	actionTaken := false
	if err == nil && len(result.ToolCalls) > 0 {
		actionTaken = true
		if q.Source != "wechat" {
			err = fmt.Errorf("当前入口不允许模型执行微信操作")
		} else if len(result.ToolCalls) != 1 {
			err = fmt.Errorf("单次只允许一个 AI 行为")
		} else if !slices.ContainsFunc(actualRequest.Tools, func(v ai.Tool) bool { return v.Name == result.ToolCalls[0].Name }) {
			err = fmt.Errorf("模型请求了本轮未提供的操作，已停止执行")
		} else {
			err = s.executeTool(ctx, q.Target, result.ToolCalls[0])
		}
		if err == nil {
			result.Text = "已执行授权操作：" + strings.ReplaceAll(result.ToolCalls[0].Name, "__", ".")
		}
	}

	if err == nil && len(result.Data) > 0 {
		result.Text, err = s.saveMedia(result.Data, result.MIME)
	}
	if ctx.Err() != nil {
		s.updateTask(id, func(t *Task) { t.Status = "cancelled" })
		return
	}
	// 父任务在全部消息发送结束后才完成，防止同会话后续请求穿插发送。
	if err == nil && !silent && !actionTaken && q.Source == "wechat" && q.Kind == "chat" {
		err = s.deliverReplyEvidence(ctx, q.Target, result.Text, q.InputKind, func() error { return s.validateReplyEvidence(actualRequest) })
	}
	if err == nil && !silent && !actionTaken && q.Source == "wechat-proactive" && q.Kind == "chat" {
		err = s.deliverProactive(ctx, actualRequest, result.Text)
	}

	s.updateTask(id, func(t *Task) {
		t.Model = used
		if err != nil {
			t.Status = "failed"
			t.Error = err.Error()
			t.ReplyRetryable = modelFailed && (wechatReplySource(q.Source) && q.Conversation != nil || q.Source == "reply-media")
			if q.Kind == "insight" {
				t.Result = result.Text
			}
		} else {
			t.Status = "completed"
			t.Result = result.Text
			if silent {
				t.Result = "本次无需回复"
			}
		}
	})
	_ = s.store.change(func(v *State) error {
		level := "INFO"
		message := "AI 任务完成"
		if err != nil {
			level = "ERROR"
			message = err.Error()
		}
		addLog(v, level, "ai", message)
		if generated {
			date := time.Now().Format("2006-01-02")
			found := false
			for i := range v.Trend {
				if v.Trend[i].Date == date {
					v.Trend[i].Input += result.InputTokens
					v.Trend[i].Output += result.OutputTokens
					found = true
				}
			}
			if !found {
				v.Trend = append(v.Trend, Point{Date: date, Input: result.InputTokens, Output: result.OutputTokens})
			}
		}
		return nil
	})
	s.notify()
	if q.Source == "wechat" && err != nil {
		s.safetyNotice(q.Target, err)
	}
}
func (s *Server) deleteReport(id string) error {
	if handled, err := s.deleteManagedReport(id); handled {
		return err
	}
	err := s.store.change(func(v *State) error {
		for i, t := range v.Tasks {
			if t.ID != id || t.Kind != "insight" || t.Status == "deleted" {
				continue
			}
			if t.Status == "queued" || t.Status == "running" {
				return fmt.Errorf("请先取消正在生成的报告")
			}
			// 清除报告和输入，仅保留调用时间占位，避免删除报告重置每日调用限额。
			v.Tasks[i] = Task{ID: t.ID, Created: t.Created, Status: "deleted"}
			return nil
		}
		return fmt.Errorf("分析报告不存在")
	})
	if err == nil {
		s.streamMu.Lock()
		if stream := s.taskStreams[id]; stream != nil {
			stream.text = ""
			if !stream.done {
				stream.done = true
				close(stream.wake)
			}
			delete(s.taskStreams, id)
		}
		s.streamMu.Unlock()
	}
	return err
}

func (s *Server) updateTask(id string, fn func(*Task)) {
	_ = s.store.change(func(v *State) error {
		for i := range v.Tasks {
			if v.Tasks[i].ID == id {
				if v.Tasks[i].Status != "cancelled" && v.Tasks[i].Status != "deleted" {
					fn(&v.Tasks[i])
				}
				break
			}
		}
		return nil
	})
	s.notify()
}
func (s *Server) autoReply(m Message) {
	if m.Self || !slices.Contains([]string{"text", "image", "audio", "quote"}, m.Kind) {
		return
	}
	state := s.store.snapshot()
	if m.Kind == "image" && !settingFlag(state, "vision.auto") || m.Kind == "audio" && !settingFlag(state, "audio.auto") {
		return
	}
	if state.Settings["reply.enabled"] != true {
		return
	}
	if !replyTargetAllowed(state, m.Target) {
		return
	}
	var contact *Contact
	for _, c := range state.Contacts {
		if c.ID == m.Target {
			v := c
			contact = &v
			break
		}
	}
	if contact == nil || contact.Mode != "auto" {
		return
	}
	// 名单规则通过后，群聊继续按独立概率判断，不推断未验证的 @ 或引用。
	if contact.Kind == "group" {
		// 未经协议验证的 @ / 引用不做推断；普通群消息只按显式概率参与。
		if rand.Float64()*100 >= groupReplyProbability(state, m.Target) {
			return
		}
	} else if state.Settings["reply.private"] == "关闭" {
		return
	}
	if contact.Kind != "group" && state.Settings["reply.private"] == "关键词" {
		keywords, _ := state.Settings["reply.keywords"].(string)
		match := false
		for _, word := range strings.Split(keywords, ",") {
			if word = strings.TrimSpace(word); word != "" && strings.Contains(m.Content, word) {
				match = true
			}
		}
		if !match {
			return
		}
	}
	if quiet, _ := state.Settings["reply.quiet"].(string); quiet != "" {
		parts := strings.Split(quiet, "-")
		if len(parts) == 2 {
			now := time.Now().Format("15:04")
			if parts[0] < parts[1] && now >= parts[0] && now < parts[1] || parts[0] >= parts[1] && (now >= parts[0] || now < parts[1]) {
				return
			}
		}
	}
	for _, t := range state.Tasks {
		if t.Target == m.Target {
			stamp, _ := time.Parse(time.RFC3339, t.Created)
			if t.Status == "queued" || t.Status == "running" || time.Since(stamp) < time.Duration(s.number("reply.cooldown", 15))*time.Second {
				return
			}
		}
	}
	q := replyRequest(state, m)
	_, err := s.queue(q)
	if err != nil {
		s.safetyNotice(m.Target, err)
		_ = s.store.change(func(v *State) error { addLog(v, "WARN", "ai", err.Error()); return nil })
	}
}

// groupReplyProbability 与其他群触发限制共同生效；零概率是有效覆写，不能当成未设置。
func groupReplyProbability(st State, target string) float64 {
	if n, ok := st.Overrides[target]["probability"].(float64); ok && n >= 0 && n <= 100 {
		return n
	}
	n, _ := st.Settings["reply.probability"].(float64)
	if n < 0 || n > 100 {
		return 0
	}
	return n
}
