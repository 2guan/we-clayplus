// 文件名: plugins/ai/pipeline.go
// 主要功能: 多模态微信事件分发处理流水线、私聊与群聊强弱触发判定、多模态解析与动作安全网关
// 作者: 2guan

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/ai/provider"
	"github.com/we-clay/we-clay/sdk/chatroom"
	contactsdk "github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/message"
	"github.com/we-clay/we-clay/sdk/plugin"
)

// Pipeline AI 处理流水线
type Pipeline struct {
	cfg      *Config
	router   *Router
	memory   *MemoryManager
	matrix   *MatrixExecutor
	auditor  *Auditor
	tts      *TTSService
	message  message.Ability
	contact  contactsdk.Ability
	chatroom chatroom.Ability
}

// NewPipeline 实例化流水线
func NewPipeline(
	cfg *Config,
	router *Router,
	memory *MemoryManager,
	matrix *MatrixExecutor,
	auditor *Auditor,
	tts *TTSService,
	msg message.Ability,
	cnt contactsdk.Ability,
	crm chatroom.Ability,
) *Pipeline {
	return &Pipeline{
		cfg:      cfg,
		router:   router,
		memory:   memory,
		matrix:   matrix,
		auditor:  auditor,
		tts:      tts,
		message:  msg,
		contact:  cnt,
		chatroom: crm,
	}
}

// HandleEvent 处理单次微信事件
func (p *Pipeline) HandleEvent(event *plugin.Event) (bool, error) {
	if event == nil {
		return true, nil
	}

	msgPayload, ok := event.Payload.(*plugin.Event_Message)
	if !ok || msgPayload == nil || msgPayload.Message == nil {
		return true, nil
	}

	rawMsg := msgPayload.Message

	// 动态热同步控制台保存的最新权限矩阵与延时配置
	if p.auditor != nil {
		if err := p.auditor.LoadMatrixSettingsFromDB(&p.cfg.Matrix); err == nil {
			p.matrix.UpdateConfig(p.cfg.Matrix)
		}
	}

	// 1. 获取微信自身登录账号信息
	var (
		selfUsername string
		selfNickname string
	)
	if p.contact != nil {
		if self := p.contact.GetSelf(); self != nil {
			selfUsername = self.Username
			selfNickname = self.Nickname
		}
	}

	// 2. 判断会话与发送者属性
	isGroup := false
	if rawMsg.Sender != nil && rawMsg.Sender.GetType() == contactsdk.ContactType_CONTACT_TYPE_CHATROOM {
		isGroup = true
	}

	var (
		sessionID  string
		senderID   string
		senderName string
		isSelf     = false
	)

	if isGroup {
		sessionID = rawMsg.Sender.GetUsername()
		if rawMsg.Member != nil {
			senderID = rawMsg.Member.GetUsername()
			senderName = rawMsg.Member.GetDisplayName()
			if senderName == "" {
				senderName = rawMsg.Member.GetNickname()
			}
		} else {
			senderID = rawMsg.Sender.GetUsername()
			senderName = rawMsg.Sender.GetNickname()
		}
		if selfUsername != "" && senderID == selfUsername {
			isSelf = true
		}
	} else {
		senderID = rawMsg.Sender.GetUsername()
		senderName = rawMsg.Sender.GetNickname()
		receiverID := rawMsg.Receiver.GetUsername()

		if selfUsername != "" && senderID == selfUsername {
			isSelf = true
			sessionID = receiverID
		} else {
			sessionID = senderID
		}
	}

	// 忽略自身发出的消息，防止死循环回声
	if isSelf {
		return true, nil
	}

	// 3. 读取目标专属策略覆盖 (Target Override)
	var override *TargetOverride
	if p.auditor != nil {
		ov, err := p.auditor.GetTargetOverride(sessionID)
		if err == nil && ov != nil {
			override = ov
		}
	}

	// 4. 特殊事件分支：好友申请事件 (message.TypeVerify)
	if rawMsg.Type != nil && rawMsg.Type.Code == message.TypeVerify.Code {
		return p.handleVerifyFriendEvent(rawMsg)
	}

	// 5. 特殊事件分支：拍一拍系统提示 (TypeSystemTip / TypeSystem)
	if rawMsg.Type != nil && (rawMsg.Type.Code == message.TypeSystemTip.Code || rawMsg.Type.Code == message.TypeSystem.Code) {
		return p.handlePatSystemTip(rawMsg, isGroup, sessionID, senderID, senderName, selfNickname)
	}

	// 6. 多模态内容解析与提取
	userParts, textSummary, isQuoteBot := p.extractMultiModalContent(rawMsg, selfUsername, selfNickname)
	if len(userParts) == 0 && textSummary == "" {
		return true, nil
	}

	// 7. 触发判定核心逻辑
	shouldTrigger := p.evaluateTrigger(isGroup, textSummary, isQuoteBot, selfNickname, selfUsername, override)
	if !shouldTrigger {
		slog.Debug("[pipeline] 消息未满足触发条件，跳过回复", "session", sessionID, "is_group", isGroup)
		return true, nil
	}

	slog.Info("[pipeline] 触发大模型回复", "session", sessionID, "sender", senderName, "is_group", isGroup)

	// 8. 异步执行大模型补全与动作派发
	go p.executeChatWorkflow(sessionID, senderID, senderName, isGroup, selfUsername, selfNickname, userParts, textSummary, override)

	return true, nil
}

// evaluateTrigger 评估是否满足回复触发条件
func (p *Pipeline) evaluateTrigger(
	isGroup bool,
	text string,
	isQuoteBot bool,
	selfNickname string,
	selfUsername string,
	override *TargetOverride,
) bool {
	// 场景 A: 私聊
	if !isGroup {
		if override != nil && override.SilentMode {
			slog.Debug("[pipeline] 私聊命中专属策略静音模式 silent_mode=true")
			return false
		}
		// 默认 100% 回复
		return p.cfg.PrivateChatReply
	}

	// 场景 B: 群聊触发判定
	// B1: 强触发判定 (命中任意强触发项 100% 触发)
	// 1. @机器人 (文本中包含 @机器人昵称)
	if p.cfg.GroupTriggerOnAt && selfNickname != "" && strings.Contains(text, "@"+selfNickname) {
		slog.Debug("[pipeline] 群聊命中强触发: @机器人")
		return true
	}
	// 2. 引用了机器人的上一条回复
	if p.cfg.GroupTriggerOnQuote && isQuoteBot {
		slog.Debug("[pipeline] 群聊命中强触发: 引用了机器人的发言")
		return true
	}
	// 3. 命中唤醒词前缀
	if p.cfg.GroupTriggerOnWake {
		for _, w := range p.cfg.WakeWords {
			if strings.HasPrefix(strings.TrimSpace(text), w) || strings.Contains(text, w) {
				slog.Debug("[pipeline] 群聊命中强触发: 唤醒词", "word", w)
				return true
			}
		}
	}

	// B2: 弱触发抽样判定 (按 reply_probability 概率抽样)
	probability := p.cfg.GroupReplyProbability
	if override != nil && override.ReplyRate >= 0 {
		probability = override.ReplyRate
	}

	if probability <= 0 {
		return false
	}
	if probability >= 1.0 {
		return true
	}

	// 随机抽样
	draw := rand.Float64()
	return draw <= probability
}

// extractMultiModalContent 解析多模态图片、音频、视频、表情包或引用结构
func (p *Pipeline) extractMultiModalContent(
	msg *message.Message,
	selfUsername, selfNickname string,
) (parts []provider.ContentPart, textSummary string, isQuoteBot bool) {
	if msg == nil {
		return nil, "", false
	}

	textSummary = strings.TrimSpace(msg.Content)

	// 检查各类多模态数据承载
	if msg.Data != nil {
		switch data := msg.Data.(type) {
		case *message.Message_Image:
			// 图片消息
			var imgURL string
			if data.Image != nil && data.Image.Media != nil {
				if len(data.Image.Media.Data) > 0 {
					b64 := base64.StdEncoding.EncodeToString(data.Image.Media.Data)
					imgURL = fmt.Sprintf("data:image/jpeg;base64,%s", b64)
				} else if data.Image.Media.Url != "" {
					imgURL = data.Image.Media.Url
				}
			}
			if imgURL != "" {
				parts = append(parts, provider.ContentPart{
					Type:     provider.PartTypeImage,
					ImageURL: imgURL,
				})
			}
			if textSummary == "" {
				textSummary = "[发送了一张图片]"
			}

		case *message.Message_Voice:
			// 语音消息
			durationSec := uint32(3)
			if data.Voice != nil && data.Voice.Duration > 0 {
				durationSec = data.Voice.Duration / 1000
				if durationSec == 0 {
					durationSec = 1
				}
			}
			textSummary = fmt.Sprintf("[语音消息: 时长 %d 秒]", durationSec)
			parts = append(parts, provider.ContentPart{
				Type: provider.PartTypeText,
				Text: textSummary,
			})

		case *message.Message_Video:
			// 视频消息
			durationSec := uint32(5)
			if data.Video != nil && data.Video.Duration > 0 {
				durationSec = data.Video.Duration
			}
			textSummary = fmt.Sprintf("[视频动态: 时长 %d 秒]", durationSec)
			parts = append(parts, provider.ContentPart{
				Type: provider.PartTypeText,
				Text: textSummary,
			})

		case *message.Message_Emoji:
			// 表情包消息
			desc := "表情"
			if data.Emoji != nil && data.Emoji.Desc != "" {
				desc = data.Emoji.Desc
			}
			textSummary = fmt.Sprintf("[表情包: %s]", desc)
			parts = append(parts, provider.ContentPart{
				Type: provider.PartTypeText,
				Text: textSummary,
			})

		case *message.Message_App:
			// 引用消息或其他 App 消息
			if data.App != nil {
				if data.App.SubType == 57 || strings.Contains(data.App.Xml, "refermsg") {
					// 引用消息
					if selfNickname != "" && strings.Contains(data.App.Xml, selfNickname) {
						isQuoteBot = true
					}
					if selfUsername != "" && strings.Contains(data.App.Xml, selfUsername) {
						isQuoteBot = true
					}
					if data.App.Title != "" {
						textSummary = data.App.Title
					}
				}
			}
		}
	}

	if textSummary != "" && len(parts) == 0 {
		parts = append(parts, provider.ContentPart{
			Type: provider.PartTypeText,
			Text: textSummary,
		})
	}

	return parts, textSummary, isQuoteBot
}

// handlePatSystemTip 处理拍一拍系统提示 (被拍强触发与互拍)
func (p *Pipeline) handlePatSystemTip(
	msg *message.Message,
	isGroup bool,
	sessionID, senderID, senderName, selfNickname string,
) (bool, error) {
	content := msg.Content
	if !strings.Contains(content, "拍了拍") {
		return true, nil
	}

	slog.Info("[pipeline] 收到微信拍一拍事件", "session", sessionID, "content", content)

	// 判断是否为机器人被拍一拍
	isBotPatted := false
	if selfNickname != "" && strings.Contains(content, "拍了拍我") {
		isBotPatted = true
	} else if selfNickname != "" && strings.Contains(content, "拍了拍 "+selfNickname) {
		isBotPatted = true
	}

	if !isBotPatted {
		return true, nil
	}

	// 1. 若配置开启被拍互拍，执行拍一拍反拍 (带有拟人生理反应延时)
	if p.cfg.Matrix.Pat.Enabled && p.cfg.Matrix.Pat.PatBack {
		if p.matrix.CheckPatAllowed(senderID) {
			targetSession := sessionID
			targetSenderName := senderName
			minPatDelay := p.cfg.Matrix.Pat.MinDelaySec
			if minPatDelay <= 0 {
				minPatDelay = 2
			}
			maxPatDelay := p.cfg.Matrix.Pat.MaxDelaySec
			if maxPatDelay < minPatDelay {
				maxPatDelay = minPatDelay + 4
			}
			_ = p.matrix.EnqueueHumanTask(fmt.Sprintf("反拍互动-%s", senderID), minPatDelay, maxPatDelay, func() error {
				slog.Info("[pipeline] 拟人延时触发反拍互动", "target", senderID)
				reply := fmt.Sprintf("拍了拍 @%s ，并对你眨了眨眼~", targetSenderName)
				_, sErr := p.message.Send(&message.Message{
					Receiver: &contactsdk.Contact{Username: targetSession},
					Content:  reply,
				})
				return sErr
			})
		}
	}

	// 2. 群聊中被拍，同样视为 100% 强触发进行大模型智能幽默互动
	if p.cfg.GroupTriggerOnPat {
		userParts := []provider.ContentPart{
			{Type: provider.PartTypeText, Text: fmt.Sprintf("[%s 拍了拍我]", senderName)},
		}
		go p.executeChatWorkflow(sessionID, senderID, senderName, isGroup, "", selfNickname, userParts, "对方拍了拍我", nil)
	}

	return true, nil
}

// handleVerifyFriendEvent 自动审核新好友申请 (严格执行两阶段拟人延时：先延时审核，再延时打招呼)
func (p *Pipeline) handleVerifyFriendEvent(msg *message.Message) (bool, error) {
	if !p.cfg.Matrix.VerifyFriend.Enabled {
		return true, nil
	}

	slog.Info("[pipeline] 收到新好友申请事件", "content", msg.Content)

	keyword := p.cfg.Matrix.VerifyFriend.KeywordFilter
	if keyword != "" && !strings.Contains(msg.Content, keyword) {
		slog.Info("[pipeline] 好友申请未包含指定暗号关键词，跳过自动通过", "required", keyword)
		return true, nil
	}

	// 提取好友申请验证参数
	v1 := ""
	v2 := ""
	if msg.Sender != nil {
		v1 = msg.Sender.GetUsername()
	}

	if p.contact == nil || p.matrix == nil {
		return true, nil
	}

	// 获取自身微信真实昵称，用于动态欢迎语
	selfNickname := ""
	if self := p.contact.GetSelf(); self != nil {
		selfNickname = self.Nickname
	}

	targetUser := v1
	targetV2 := v2

	minVerify := p.cfg.Matrix.VerifyFriend.MinVerifyDelaySec
	if minVerify <= 0 {
		minVerify = 30
	}
	maxVerify := p.cfg.Matrix.VerifyFriend.MaxVerifyDelaySec
	if maxVerify < minVerify {
		maxVerify = minVerify + 30
	}

	greetTpl := p.cfg.Matrix.VerifyFriend.AutoGreet
	if greetTpl == "" {
		greetTpl = "你好呀！我是{昵称}，很高兴认识你~"
	}
	greet := formatDynamicGreeting(greetTpl, selfNickname)

	minGreet := p.cfg.Matrix.VerifyFriend.MinDelaySec
	if minGreet <= 0 {
		minGreet = 20
	}
	maxGreet := p.cfg.Matrix.VerifyFriend.MaxDelaySec
	if maxGreet < minGreet {
		maxGreet = minGreet + 25
	}

	// 阶段一：收到好友申请后，先在拟人队列中等待延时（模拟真人查看微信并点击通过）
	err := p.matrix.EnqueueHumanTask(fmt.Sprintf("延时通过好友申请-%s", targetUser), minVerify, maxVerify, func() error {
		if p.contact == nil {
			return fmt.Errorf("联系人能力未就绪")
		}
		vErr := p.contact.VerifyFriend(targetUser, targetV2, 6)
		if vErr != nil {
			slog.Error("[pipeline] 自动通过好友验证失败", "username", targetUser, "err", vErr)
			return vErr
		}
		slog.Info("[pipeline] 拟人延时通过好友申请成功", "username", targetUser)

		// 阶段二：通过后，再延时发送个性化欢迎打招呼语
		if greet != "" && p.message != nil {
			_ = p.matrix.EnqueueHumanTask(fmt.Sprintf("新好友打招呼-%s", targetUser), minGreet, maxGreet, func() error {
				_, sErr := p.message.Send(&message.Message{
					Receiver: &contactsdk.Contact{Username: targetUser},
					Content:  greet,
				})
				return sErr
			})
		}
		return nil
	})

	if err != nil {
		slog.Warn("[pipeline] 排队延时通过好友申请失败", "err", err)
	}

	return true, nil
}

// executeChatWorkflow 完整的大模型编排流
func (p *Pipeline) executeChatWorkflow(
	sessionID, senderID, senderName string,
	isGroup bool,
	botUsername, botNickname string,
	userParts []provider.ContentPart,
	userText string,
	override *TargetOverride,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// 1. 组装 System Prompt
	promptCtx := PromptContext{
		SessionID:   sessionID,
		IsGroup:     isGroup,
		SenderID:    senderID,
		SenderName:  senderName,
		BotUsername: botUsername,
		BotNickname: botNickname,
		AllowVoice:  p.cfg.Matrix.VoiceReply.Enabled,
		AllowPat:    p.cfg.Matrix.Pat.Enabled,
		AllowAdd:    p.cfg.Matrix.AddFriend.Enabled,
		AllowRevoke: p.cfg.Matrix.Revoke.Enabled,
		AllowRemark: p.cfg.Matrix.SetRemark.Enabled,
		AllowAdmin:  p.cfg.Matrix.ChatroomAdmin.Enabled,
		AtMode:      p.cfg.Matrix.AtMode,
	}

	if override != nil {
		promptCtx.CustomPrompt = override.CustomPrompt
		promptCtx.TargetRole = override.PersonaSummary
	}

	systemPrompt := BuildSystemPrompt(promptCtx)

	// 2. 载入会话记忆并拼接当前用户输入
	history := p.memory.GetMessages(sessionID)
	var messages []provider.ChatMessage
	messages = append(messages, provider.ChatMessage{
		Role:    provider.RoleSystem,
		Content: systemPrompt,
	})
	messages = append(messages, history...)

	// 添加当前轮次用户提问
	currentUserMsg := provider.ChatMessage{
		Role:  provider.RoleUser,
		Parts: userParts,
	}
	messages = append(messages, currentUserMsg)

	// 3. 调用多模态路由网关
	targetModel := p.cfg.DefaultModel
	if override != nil && override.ModelName != "" {
		targetModel = override.ModelName
	}

	contextRounds := p.cfg.ContextRounds
	if override != nil && override.ContextRounds > 0 {
		contextRounds = override.ContextRounds
	}

	resp, err := p.router.Generate(ctx, targetModel, messages, p.cfg.Temperature, p.cfg.MaxTokens)
	if err != nil {
		slog.Error("[pipeline] 大模型生成回复失败", "session", sessionID, "err", err)
		return
	}

	// 4. 解析 Action Directives 标签并纯净化文本
	parsed := ParseActionDirectives(resp.Content)

	// 5. 决定消息发出方式并执行拟人态延时
	if parsed.IsVoice && p.cfg.Matrix.VoiceReply.Enabled && p.tts != nil {
		// 语音消息直发
		_ = p.tts.SendVoiceMessage(ctx, p.message, sessionID, parsed.CleanText)
	} else if parsed.CleanText != "" {
		// 自然拟人打字延时 (1.5s ~ 4.5s)
		p.matrix.SimulateTyping(len(parsed.CleanText))

		// 发送纯文本微信消息
		sendMsg := &message.Message{
			Receiver: &contactsdk.Contact{Username: sessionID},
			Content:  parsed.CleanText,
		}
		_, err = p.message.Send(sendMsg)
		if err != nil {
			slog.Error("[pipeline] 发送微信消息失败", "session", sessionID, "err", err)
		}
	}

	// 6. 执行被解析的各项底层 Action 指令
	p.dispatchParsedActions(sessionID, botNickname, parsed)

	// 7. 更新上下文记忆
	p.memory.Append(sessionID, currentUserMsg, contextRounds)
	p.memory.Append(sessionID, provider.ChatMessage{
		Role:    provider.RoleAssistant,
		Content: parsed.CleanText,
	}, contextRounds)

	// 8. 记录 Token 消耗审计
	if p.auditor != nil {
		p.auditor.RecordTokenUsage(
			sessionID,
			resp.Model,
			resp.PromptTokens,
			resp.CompletionTokens,
			resp.TotalTokens,
			resp.Duration.Milliseconds(),
		)
	}
}

// dispatchParsedActions 分发由大模型生成的各项微信底层操作指令
func (p *Pipeline) dispatchParsedActions(sessionID, botNickname string, parsed *ParsedActionResult) {
	// 1. 拍一拍操作 (拟人延迟)
	for _, target := range parsed.PatTargets {
		if p.matrix.CheckPatAllowed(target) {
			slog.Info("[pipeline] 执行大模型拍一拍指令", "target", target)
			minPatDelay := p.cfg.Matrix.Pat.MinDelaySec
			if minPatDelay <= 0 {
				minPatDelay = 2
			}
			maxPatDelay := p.cfg.Matrix.Pat.MaxDelaySec
			if maxPatDelay < minPatDelay {
				maxPatDelay = minPatDelay + 4
			}
			targetUser := target
			_ = p.matrix.EnqueueHumanTask(fmt.Sprintf("指令拍一拍-%s", targetUser), minPatDelay, maxPatDelay, func() error {
				if p.message != nil {
					reply := fmt.Sprintf("拍了拍 @%s", targetUser)
					_, sErr := p.message.Send(&message.Message{
						Receiver: &contactsdk.Contact{Username: sessionID},
						Content:  reply,
					})
					return sErr
				}
				return nil
			})
		}
	}

	// 2. 群内加好友指令 (进入拟人等待队列)
	for _, task := range parsed.AddFriendTasks {
		if p.matrix.CheckAddFriendAllowed() && p.contact != nil {
			uID := task.UserID
			greet := task.Greet
			if greet == "" {
				greet = p.cfg.Matrix.AddFriend.GreetTemplate
			}
			greet = formatDynamicGreeting(greet, botNickname)
			minWait := p.cfg.Matrix.AddFriend.MinWaitSec
			if minWait <= 0 {
				minWait = 30
			}
			maxWait := p.cfg.Matrix.AddFriend.MaxWaitSec
			if maxWait < minWait {
				maxWait = minWait + 30
			}

			_ = p.matrix.EnqueueHumanTask(fmt.Sprintf("加好友-%s", uID), minWait, maxWait, func() error {
				return p.contact.AddFriend(uID, "", greet, 2, 14)
			})
		}
	}

	// 3. 好友审核指令 (严格执行两阶段拟人延时：先延时审核，再延时打招呼)
	for _, task := range parsed.VerifyFriendTasks {
		if p.cfg.Matrix.VerifyFriend.Enabled && p.contact != nil {
			targetUser := task.V1
			targetV2 := task.V2
			greet := task.Greet
			if greet == "" {
				greet = p.cfg.Matrix.VerifyFriend.AutoGreet
			}
			if greet == "" {
				greet = "你好呀！我是{昵称}，很高兴认识你~"
			}
			greet = formatDynamicGreeting(greet, botNickname)

			minVerify := p.cfg.Matrix.VerifyFriend.MinVerifyDelaySec
			if minVerify <= 0 {
				minVerify = 30
			}
			maxVerify := p.cfg.Matrix.VerifyFriend.MaxVerifyDelaySec
			if maxVerify < minVerify {
				maxVerify = minVerify + 30
			}

			minGreet := p.cfg.Matrix.VerifyFriend.MinDelaySec
			if minGreet <= 0 {
				minGreet = 20
			}
			maxGreet := p.cfg.Matrix.VerifyFriend.MaxDelaySec
			if maxGreet < minGreet {
				maxGreet = minGreet + 25
			}

			_ = p.matrix.EnqueueHumanTask(fmt.Sprintf("指令延时通过好友申请-%s", targetUser), minVerify, maxVerify, func() error {
				if p.contact == nil {
					return fmt.Errorf("联系人能力未就绪")
				}
				vErr := p.contact.VerifyFriend(targetUser, targetV2, 6)
				if vErr != nil {
					slog.Error("[pipeline] 指令通过好友验证失败", "username", targetUser, "err", vErr)
					return vErr
				}
				slog.Info("[pipeline] 指令拟人延时通过好友申请成功", "username", targetUser)

				if greet != "" && p.message != nil {
					_ = p.matrix.EnqueueHumanTask(fmt.Sprintf("指令新好友打招呼-%s", targetUser), minGreet, maxGreet, func() error {
						_, sErr := p.message.Send(&message.Message{
							Receiver: &contactsdk.Contact{Username: targetUser},
							Content:  greet,
						})
						return sErr
					})
				}
				return nil
			})
		}
	}

	// 4. 消息自我纠错撤回
	for _, task := range parsed.RevokeTasks {
		if p.cfg.Matrix.Revoke.Enabled && p.message != nil {
			msgIDUint, err := strconv.ParseUint(task.MsgID, 10, 64)
			if err == nil {
				slog.Info("[pipeline] 执行消息撤回指令", "msg_id", task.MsgID)
				_, _ = p.message.Revoke(sessionID, msgIDUint)
				if p.cfg.Matrix.Revoke.AutoApologize && task.Explanation != "" {
					apologizeDelay := p.cfg.Matrix.Revoke.ApologizeDelaySec
					if apologizeDelay <= 0 {
						apologizeDelay = 2
					}
					_ = p.matrix.EnqueueHumanTask(fmt.Sprintf("撤回致歉-%s", sessionID), apologizeDelay, apologizeDelay+2, func() error {
						_, sErr := p.message.Send(&message.Message{
							Receiver: &contactsdk.Contact{Username: sessionID},
							Content:  formatDynamicGreeting(task.Explanation, botNickname),
						})
						return sErr
					})
				}
			}
		}
	}

	// 5. 修改好友备注
	for _, task := range parsed.SetRemarkTasks {
		if p.cfg.Matrix.SetRemark.Enabled && p.contact != nil {
			slog.Info("[pipeline] 执行修改备注指令", "user", task.UserID, "remark", task.Remark)
			_ = p.contact.SetRemark(task.UserID, task.Remark)
		}
	}

	// 6. 设置群公告
	for _, task := range parsed.Announcements {
		if p.cfg.Matrix.ChatroomAdmin.Enabled && p.chatroom != nil {
			slog.Info("[pipeline] 执行设置群公告指令", "room", task.ChatroomID)
			_, _ = p.chatroom.SetAnnouncement(task.ChatroomID, task.Content)
		}
	}

	// 7. 违规踢人
	for _, task := range parsed.KickTasks {
		if p.cfg.Matrix.ChatroomAdmin.Enabled && p.chatroom != nil {
			slog.Warn("[pipeline] 执行群管移出成员指令", "room", task.ChatroomID, "member", task.UserID, "reason", task.Reason)
			_, _ = p.chatroom.RemoveMember(task.ChatroomID, []string{task.UserID})
		}
	}
}

// formatDynamicGreeting 动态格式化欢迎语，将 {昵称} 及遗留的硬编码助理字眼替换为实际获取的微信真实昵称
func formatDynamicGreeting(tpl, selfNickname string) string {
	if strings.TrimSpace(tpl) == "" {
		return ""
	}
	nick := strings.TrimSpace(selfNickname)
	if nick == "" {
		nick = "我"
	}
	res := strings.ReplaceAll(tpl, "{昵称}", nick)
	res = strings.ReplaceAll(res, "智能微信助理", nick)
	res = strings.ReplaceAll(res, "微信助理", nick)
	res = strings.ReplaceAll(res, "智能助理", nick)
	res = strings.ReplaceAll(res, "智能管家", nick)
	return res
}
