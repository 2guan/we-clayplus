// 文件名: plugins/ai/ai_test.go
// 主要功能: AI 智能体触发规则、动作解析与内存滑动窗口单元测试集
// 作者: 2guan

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/we-clay/we-clay/plugins/ai/provider"
	"github.com/we-clay/we-clay/sdk/message"
)

// TestActionDirectivesParser 测试 Action Directives 动作标签解析与文本剥离纯净化
func TestActionDirectivesParser(t *testing.T) {
	raw := `[VOICE]你好啊！很高兴认识你~
[ACTION:PAT:wxid_target123]
我也加你好友啦 [ACTION:ADD_FRIEND:wxid_friend456:我是智能管家]
刚才说错了，撤回一下 [ACTION:REVOKE:10086:不好意思刚才笔误]
[@张三] 你今天过得怎么样？
[ACTION:SET_REMARK:wxid_friend456:张总-腾讯]
[ACTION:SET_ANNOUNCEMENT:12345@chatroom:今晚8点全员开会]
[ACTION:KICK:12345@chatroom:bad_spammer:发送违规刷单广告]
祝你开心每一天！`

	res := ParseActionDirectives(raw)

	// 1. 验证语音标记
	if !res.IsVoice {
		t.Errorf("期望 IsVoice 为 true，实际为 false")
	}

	// 2. 验证拍一拍目标
	if len(res.PatTargets) != 1 || res.PatTargets[0] != "wxid_target123" {
		t.Errorf("PatTargets 解析错误: %v", res.PatTargets)
	}

	// 3. 验证加好友任务
	if len(res.AddFriendTasks) != 1 {
		t.Fatalf("AddFriendTasks 数量错误: %d", len(res.AddFriendTasks))
	}
	if res.AddFriendTasks[0].UserID != "wxid_friend456" || res.AddFriendTasks[0].Greet != "我是智能管家" {
		t.Errorf("AddFriendTask 字段解析错误: %+v", res.AddFriendTasks[0])
	}

	// 4. 验证撤回任务
	if len(res.RevokeTasks) != 1 || res.RevokeTasks[0].MsgID != "10086" || res.RevokeTasks[0].Explanation != "不好意思刚才笔误" {
		t.Errorf("RevokeTasks 字段解析错误: %+v", res.RevokeTasks)
	}

	// 5. 验证 @ 提取与替换
	if len(res.AtMentions) != 1 || res.AtMentions[0] != "张三" {
		t.Errorf("AtMentions 解析错误: %v", res.AtMentions)
	}

	// 6. 验证修改备注
	if len(res.SetRemarkTasks) != 1 || res.SetRemarkTasks[0].UserID != "wxid_friend456" || res.SetRemarkTasks[0].Remark != "张总-腾讯" {
		t.Errorf("SetRemarkTasks 解析错误: %+v", res.SetRemarkTasks)
	}

	// 7. 验证群公告
	if len(res.Announcements) != 1 || res.Announcements[0].ChatroomID != "12345@chatroom" || res.Announcements[0].Content != "今晚8点全员开会" {
		t.Errorf("Announcements 解析错误: %+v", res.Announcements)
	}

	// 8. 验证踢人任务
	if len(res.KickTasks) != 1 || res.KickTasks[0].UserID != "bad_spammer" || res.KickTasks[0].Reason != "发送违规刷单广告" {
		t.Errorf("KickTasks 解析错误: %+v", res.KickTasks)
	}

	// 9. 验证最终文本剥离，确认不再包含中括号动作指令
	if strings.Contains(res.CleanText, "[ACTION:") || strings.Contains(res.CleanText, "[VOICE]") {
		t.Errorf("CleanText 仍残留指令标签: %s", res.CleanText)
	}

	// 验证 @ 转换为了纯文本格式
	if !strings.Contains(res.CleanText, "@张三") {
		t.Errorf("CleanText 未包含转换后的 @张三: %s", res.CleanText)
	}
}

// TestPromptBuilder 测试系统提示词构建与专属人设注入
func TestPromptBuilder(t *testing.T) {
	ctx := PromptContext{
		SessionID:    "wxid_user999",
		IsGroup:      false,
		SenderID:     "wxid_user999",
		SenderName:   "李雷",
		BotUsername:  "wxid_bot001",
		BotNickname:  "小微助手",
		TargetRole:   "贴心英语私教",
		CustomPrompt: "尽量使用中英双语互动，并在每次回复末尾给出鼓励",
		AllowVoice:   true,
		AllowPat:     true,
		AllowAdd:     false,
		AllowRevoke:  true,
		AllowRemark:  true,
	}

	prompt := BuildSystemPrompt(ctx)

	if !strings.Contains(prompt, "贴心英语私教") {
		t.Errorf("Prompt 未包含专属角色设定: %s", prompt)
	}
	if !strings.Contains(prompt, "尽量使用中英双语互动") {
		t.Errorf("Prompt 未包含专属自定义提示词: %s", prompt)
	}
	if !strings.Contains(prompt, "[VOICE]") {
		t.Errorf("Prompt 未包含语音回复指令说明")
	}
	if !strings.Contains(prompt, "[ACTION:PAT:wxid_user999]") {
		t.Errorf("Prompt 未包含拍一拍指令说明")
	}
}

// TestMemorySlidingWindow 测试滑动窗口记忆管理器截断机制
func TestMemorySlidingWindow(t *testing.T) {
	mgr := NewMemoryManager(2) // 设定为 2 轮上下文（即最多 4 条消息）

	sessionID := "test_session_1"
	for i := 1; i <= 6; i++ {
		role := provider.RoleUser
		if i%2 == 0 {
			role = provider.RoleAssistant
		}
		mgr.Append(sessionID, provider.ChatMessage{
			Role:    role,
			Content: strings.Repeat("A", i),
		}, 2)
	}

	msgs := mgr.GetMessages(sessionID)
	// 期望只保留最后 4 条消息 (2 轮对话)
	if len(msgs) != 4 {
		t.Fatalf("期望保留 4 条消息，实际保留 %d 条", len(msgs))
	}

	if msgs[0].Content != "AAA" {
		t.Errorf("截断窗口起始消息内容不符合预期: %s", msgs[0].Content)
	}
	if msgs[3].Content != "AAAAAA" {
		t.Errorf("截断窗口最新消息内容不符合预期: %s", msgs[3].Content)
	}
}

// TestMatrixExecutorSafetyGates 测试 AI 权限矩阵频控与安全熔断拦截网
func TestMatrixExecutorSafetyGates(t *testing.T) {
	cfg := MatrixConfig{
		Pat: PatMatrixConfig{
			Enabled:     true,
			CooldownSec: 2, // 2 秒冷却用于测试
		},
		AddFriend: AddFriendMatrixConfig{
			Enabled:    true,
			DailyLimit: 2, // 单日限额 2 次
		},
	}

	exec := NewMatrixExecutor(cfg)
	defer exec.Close()

	// 1. 测试拍一拍频控
	target := "user_a"
	if !exec.CheckPatAllowed(target) {
		t.Errorf("首次拍一拍应该被允许")
	}
	if exec.CheckPatAllowed(target) {
		t.Errorf("未到冷却时间，第二次拍一拍应该被拦截")
	}

	// 休眠等待冷却
	time.Sleep(2100 * time.Millisecond)
	if !exec.CheckPatAllowed(target) {
		t.Errorf("冷却时间过后，拍一拍应该被允许")
	}

	// 2. 测试加好友每日上限
	if !exec.CheckAddFriendAllowed() {
		t.Errorf("第 1 次加好友应该被允许")
	}
	if !exec.CheckAddFriendAllowed() {
		t.Errorf("第 2 次加好友应该被允许")
	}
	if exec.CheckAddFriendAllowed() {
		t.Errorf("超过单日限额 2 次后，第 3 次应该被熔断拦截")
	}
}

// TestPipelineTriggers 测试私聊与群聊强弱触发判定
func TestPipelineTriggers(t *testing.T) {
	cfg := DefaultConfig()
	cfg.WakeWords = []string{"@小微", "小助手"}
	cfg.PrivateChatReply = true
	cfg.GroupReplyProbability = 0.0 // 群聊弱触发概率为 0，仅靠强触发

	pipe := &Pipeline{cfg: &cfg}

	// 1. 私聊场景：正常应该必回 (100%)
	if !pipe.evaluateTrigger(false, "你好呀", false, "小微", "wxid_bot", nil) {
		t.Errorf("私聊默认应 100%% 触发回复")
	}

	// 私聊场景：开启 silent_mode=true 时应被静音
	overrideSilent := &TargetOverride{SilentMode: true}
	if pipe.evaluateTrigger(false, "你好呀", false, "小微", "wxid_bot", overrideSilent) {
		t.Errorf("私聊开启 silent_mode 时应静音不回复")
	}

	// 2. 群聊场景：普通发言且概率为 0 时不触发
	if pipe.evaluateTrigger(true, "今天天气真好", false, "小微", "wxid_bot", nil) {
		t.Errorf("群聊普通闲聊且弱触发为0时不应触发")
	}

	// 3. 群聊场景：@机器人强触发
	if !pipe.evaluateTrigger(true, "大家好，@小微 请问在吗？", false, "小微", "wxid_bot", nil) {
		t.Errorf("群聊中 @机器人 应当 100%% 强触发回复")
	}

	// 4. 群聊场景：引用了机器人的发言强触发
	if !pipe.evaluateTrigger(true, "赞同你的观点", true, "小微", "wxid_bot", nil) {
		t.Errorf("群聊中引用机器人发言应当 100%% 强触发回复")
	}

	// 5. 群聊场景：前缀唤醒词强触发
	if !pipe.evaluateTrigger(true, "小助手 帮我查一下汇率", false, "小微", "wxid_bot", nil) {
		t.Errorf("群聊中命中唤醒词小助手应当 100%% 强触发回复")
	}
}

// mockProvider 离线单元测试模拟大模型驱动
type mockProvider struct {
	name        string
	reply       string
	returnErr   error
	callCount   int
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) Chat(ctx context.Context, req *provider.ChatRequest) (*provider.ChatResponse, error) {
	m.callCount++
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	return &provider.ChatResponse{
		Content:          m.reply,
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
		Model:            req.Model,
		Duration:         50 * time.Millisecond,
	}, nil
}

// TestRouterFallbackChain 测试多模型容灾降级故障转移链
func TestRouterFallbackChain(t *testing.T) {
	mockP1 := &mockProvider{name: "mimo", returnErr: context.DeadlineExceeded}
	mockP2 := &mockProvider{name: "deepseek", reply: "来自备用模型的安全回复"}

	r := &Router{
		providers: map[string]provider.Provider{
			"mimo":     mockP1,
			"deepseek": mockP2,
		},
		modelChain:   []string{"mimo", "deepseek"},
		defaultProv:  "mimo",
		defaultModel: "test-model",
	}

	ctx := context.Background()
	resp, err := r.Generate(ctx, "", []provider.ChatMessage{
		{Role: provider.RoleUser, Content: "Hello"},
	}, 0.7, 500)

	if err != nil {
		t.Fatalf("容灾降级调用失败: %v", err)
	}

	if resp.Content != "来自备用模型的安全回复" {
		t.Errorf("未成功降级转移至 deepseek: %s", resp.Content)
	}

	if mockP1.callCount != 1 || mockP2.callCount != 1 {
		t.Errorf("调用次数不符合预期: p1=%d, p2=%d", mockP1.callCount, mockP2.callCount)
	}
}

// TestMultiModalContentExtraction 测试多模态消息提取
func TestMultiModalContentExtraction(t *testing.T) {
	pipe := &Pipeline{}

	// 1. 测试图片消息提取
	imgBytes := []byte("fake-image-bytes")
	msgImg := &message.Message{
		Content: "",
		Data: &message.Message_Image{
			Image: &message.ImageData{
				Media: &message.Media{
					Data: imgBytes,
				},
			},
		},
	}
	parts, summary, _ := pipe.extractMultiModalContent(msgImg, "bot_user", "小助手")
	if len(parts) != 1 || parts[0].Type != provider.PartTypeImage {
		t.Errorf("图片提取失败: %+v", parts)
	}
	if !strings.HasPrefix(parts[0].ImageURL, "data:image/jpeg;base64,") {
		t.Errorf("图片 base64 格式不符合预期: %s", parts[0].ImageURL)
	}
	if summary != "[发送了一张图片]" {
		t.Errorf("图片文本摘要不符合预期: %s", summary)
	}

	// 2. 测试表情包提取
	msgEmoji := &message.Message{
		Data: &message.Message_Emoji{
			Emoji: &message.EmojiData{
				Desc: "龇牙笑",
			},
		},
	}
	parts, summary, _ = pipe.extractMultiModalContent(msgEmoji, "bot_user", "小助手")
	if len(parts) != 1 || !strings.Contains(parts[0].Text, "龇牙笑") {
		t.Errorf("表情包提取失败: %+v", parts)
	}
}

// TestFormatDynamicGreeting 测试动态微信昵称插值与遗留助理硬编码消除
func TestFormatDynamicGreeting(t *testing.T) {
	tpl := "你好呀！我是{昵称}，很高兴认识你~"
	formatted := formatDynamicGreeting(tpl, "小美")
	if formatted != "你好呀！我是小美，很高兴认识你~" {
		t.Errorf("动态昵称替换错误: %s", formatted)
	}

	// 测试空昵称回退为 "我"
	formattedEmpty := formatDynamicGreeting(tpl, "")
	if formattedEmpty != "你好呀！我是我，很高兴认识你~" {
		t.Errorf("空昵称回退错误: %s", formattedEmpty)
	}

	// 测试遗留硬编码词替换
	legacyTpl := "你好，我是微信助理，也是你的智能管家！欢迎添加智能微信助理与智能助理。"
	formattedLegacy := formatDynamicGreeting(legacyTpl, "小美")
	expected := "你好，我是小美，也是你的小美！欢迎添加小美与小美。"
	if formattedLegacy != expected {
		t.Errorf("遗留助理词替换错误: 实际=%s, 期望=%s", formattedLegacy, expected)
	}
}

// TestVerifyFriendMatrixConfigDelays 测试好友申请两阶段延时配置与默认值
func TestVerifyFriendMatrixConfigDelays(t *testing.T) {
	cfg := DefaultConfig()

	// 验证默认好友验证配置包含两阶段延时
	if cfg.Matrix.VerifyFriend.MinVerifyDelaySec <= 0 || cfg.Matrix.VerifyFriend.MaxVerifyDelaySec <= 0 {
		t.Errorf("VerifyFriend 审核等待延时配置未正确设置默认值: min=%d, max=%d",
			cfg.Matrix.VerifyFriend.MinVerifyDelaySec, cfg.Matrix.VerifyFriend.MaxVerifyDelaySec)
	}

	if cfg.Matrix.VerifyFriend.MinDelaySec <= 0 || cfg.Matrix.VerifyFriend.MaxDelaySec <= 0 {
		t.Errorf("VerifyFriend 打招呼等待延时配置未正确设置默认值: min=%d, max=%d",
			cfg.Matrix.VerifyFriend.MinDelaySec, cfg.Matrix.VerifyFriend.MaxDelaySec)
	}

	if strings.Contains(cfg.Matrix.VerifyFriend.AutoGreet, "智能微信助理") || strings.Contains(cfg.Matrix.VerifyFriend.AutoGreet, "微信助理") {
		t.Errorf("默认 AutoGreet 包含硬编码助理词: %s", cfg.Matrix.VerifyFriend.AutoGreet)
	}
}

