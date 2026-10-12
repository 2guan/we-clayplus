// 文件名: plugins/ai/config.go
// 主要功能: AI 智能体插件完整配置模型定义（模型中心、多模态驱动、触发规则、AI权限矩阵与防风控策略）
// 作者: 2guan

package main

import (
	"os"
	"strings"
	"time"
)

func getEnv(key string) string {
	return os.Getenv(key)
}

// Config AI 插件的主配置结构体
type Config struct {
	// 数据目录（SQLite dashboard.db 所在路径）
	DataDir string `toml:"data_dir" comment:"SQLite 数据库目录"`

	// 基础对话与模型路由配置
	DefaultProvider string   `toml:"default_provider" comment:"默认使用的大模型提供商 (mimo/deepseek/openai/claude/qwen/ollama)"`
	ModelChain      []string `toml:"model_chain" comment:"容灾故障转移模型链，按优先级排序"`
	DefaultModel    string   `toml:"default_model" comment:"默认对话模型名称"`
	Temperature     float32  `toml:"temperature" comment:"生成随机性 (0.0~2.0)"`
	MaxTokens       int      `toml:"max_tokens" comment:"单次回复最大 Token 限制"`
	ContextRounds   int      `toml:"context_rounds" comment:"上下文记忆携带最大对话轮数"`

	// 触发与分发规则
	WakeWords             []string `toml:"wake_words" comment:"群聊唤醒词前缀列表，匹配时 100% 触发"`
	PrivateChatReply      bool     `toml:"private_chat_reply" comment:"私聊是否默认必回 (除开启 silent_mode)"`
	GroupReplyProbability float64  `toml:"group_reply_probability" comment:"群聊普通发言弱触发概率 (0.0~1.0)"`
	GroupTriggerOnAt      bool     `toml:"group_trigger_on_at" comment:"群聊中被 @ 机器人时 100% 强触发"`
	GroupTriggerOnQuote   bool     `toml:"group_trigger_on_quote" comment:"群聊中被引用上一条回复时 100% 强触发"`
	GroupTriggerOnPat     bool     `toml:"group_trigger_on_pat" comment:"群聊中机器人被拍一拍时 100% 强触发"`
	GroupTriggerOnWake    bool     `toml:"group_trigger_on_wake" comment:"群聊中命中唤醒词时 100% 强触发"`

	// 各大模型提供商具体连接参数
	Mimo     ProviderConfig `toml:"mimo" comment:"小米 mimo 多模态与语音驱动配置"`
	DeepSeek ProviderConfig `toml:"deepseek" comment:"DeepSeek 对话模型配置"`
	OpenAI   ProviderConfig `toml:"openai" comment:"OpenAI 多模态模型配置"`
	Claude   ProviderConfig `toml:"claude" comment:"Anthropic Claude 模型配置"`
	Qwen     ProviderConfig `toml:"qwen" comment:"阿里通义千问驱动配置"`
	Ollama   ProviderConfig `toml:"ollama" comment:"本地私有化 Ollama 配置"`

	// AI 能力授权矩阵与防封安全配置
	Matrix MatrixConfig `toml:"matrix" comment:"AI 微信底层能力授权矩阵与防风控参数"`
}

// ProviderConfig 各大模型服务商的 API 连接参数
type ProviderConfig struct {
	Enabled    bool   `toml:"enabled" comment:"是否启用该服务商"`
	APIKey     string `toml:"api_key" comment:"API 鉴权密钥 (支持环境变量引用如 $MIMO_API_KEY)"`
	BaseURL    string `toml:"base_url" comment:"API 端点 Base URL"`
	Model      string `toml:"model" comment:"默认调用模型名"`
	VisionModel string `toml:"vision_model" comment:"多模态视觉模型名 (可留空沿用 Model)"`
	AudioModel string `toml:"audio_model" comment:"音频/语音理解或合成模型名"`
	TimeoutSec int    `toml:"timeout_sec" comment:"请求超时时间(秒)"`
}

// MatrixConfig AI 微信底层能力授权矩阵
type MatrixConfig struct {
	// 1. 朋友圈批量巡回
	Moments MomentsMatrixConfig `toml:"moments" comment:"朋友圈互动配置"`

	// 2. 群内加好友
	AddFriend AddFriendMatrixConfig `toml:"add_friend" comment:"群内主动加好友权限"`

	// 3. 自动通过好友验证
	VerifyFriend VerifyFriendMatrixConfig `toml:"verify_friend" comment:"自动通过新好友申请"`

	// 4. 拍一拍互动
	Pat PatMatrixConfig `toml:"pat" comment:"拍一拍互动权限"`

	// 5. 消息纠错撤回
	Revoke RevokeMatrixConfig `toml:"revoke" comment:"大模型消息撤回权限"`

	// 6. @群友互动
	AtMode string `toml:"at_mode" comment:"@群友模式: smart (智能识别) / always (强制每次@) / never (从不@)"`

	// 7. 修改好友备注
	SetRemark SetRemarkMatrixConfig `toml:"set_remark" comment:"自动修改好友备注"`

	// 8. 智能群聊治理
	ChatroomAdmin ChatroomAdminMatrixConfig `toml:"chatroom_admin" comment:"群聊管理操作权限"`

	// 9. 自然拟人态打字延时
	TypingSimulate TypingSimulateConfig `toml:"typing_simulate" comment:"拟人打字延时防秒回"`

	// 10. 语音回复开关
	VoiceReply VoiceReplyConfig `toml:"voice_reply" comment:"语音回复配置"`
}

// MomentsMatrixConfig 朋友圈互动配置
type MomentsMatrixConfig struct {
	Enabled            bool     `toml:"enabled" comment:"是否启用朋友圈定时巡回"`
	CronSchedule       string   `toml:"cron_schedule" comment:"每日执行时间点 (例如 18:00)"`
	MinWaitSec         int      `toml:"min_wait_sec" comment:"每个操作最小拟人等待秒数 (默认 30)"`
	MaxWaitSec         int      `toml:"max_wait_sec" comment:"每个操作最大拟人等待秒数 (默认 60)"`
	MaxOpsPerRun       int      `toml:"max_ops_per_run" comment:"单次任务最大点赞/评论总数"`
	SentimentThreshold float64  `toml:"sentiment_threshold" comment:"正向情感阈值 (0-100, 默认 60)"`
	Whitelist          []string `toml:"whitelist" comment:"互动白名单好友列表 (留空为全好友)"`
}

// AddFriendMatrixConfig 群内加好友配置
type AddFriendMatrixConfig struct {
	Enabled       bool     `toml:"enabled" comment:"是否允许大模型在群内主动发起加好友"`
	Keywords      []string `toml:"keywords" comment:"触发意图关键词列表 (加我/私聊我等)"`
	DailyLimit    int      `toml:"daily_limit" comment:"单日最大加好友上限 (防风控，默认 10)"`
	MinWaitSec    int      `toml:"min_wait_sec" comment:"发起加好友排队拟人等待最小秒数 (默认 30)"`
	MaxWaitSec    int      `toml:"max_wait_sec" comment:"发起加好友排队拟人等待最大秒数 (默认 60)"`
	GreetTemplate string   `toml:"greet_template" comment:"加好友申请附言模版"`
}

// VerifyFriendMatrixConfig 自动通过好友验证申请配置
type VerifyFriendMatrixConfig struct {
	Enabled           bool   `toml:"enabled" comment:"是否自动通过好友申请"`
	KeywordFilter     string `toml:"keyword_filter" comment:"验证申请附言关键词过滤 (留空为全通过)"`
	MinVerifyDelaySec int    `toml:"min_verify_delay_sec" comment:"收到申请后延时通过最小秒数 (默认 30)"`
	MaxVerifyDelaySec int    `toml:"max_verify_delay_sec" comment:"收到申请后延时通过最大秒数 (默认 90)"`
	AutoGreet         string `toml:"auto_greet" comment:"通过后自动发送的欢迎打招呼语"`
	MinDelaySec       int    `toml:"min_delay_sec" comment:"通过后延时发送欢迎语最小秒数 (默认 20)"`
	MaxDelaySec       int    `toml:"max_delay_sec" comment:"通过后延时发送欢迎语最大秒数 (默认 45)"`
}

// PatMatrixConfig 拍一拍互动配置
type PatMatrixConfig struct {
	Enabled     bool `toml:"enabled" comment:"是否开启拍一拍互动"`
	PatBack     bool `toml:"pat_back" comment:"被拍一拍后是否主动反拍对方"`
	Proactive   bool `toml:"proactive" comment:"允许大模型在幽默语境下主动拍对方"`
	CooldownSec int  `toml:"cooldown_sec" comment:"针对同一人的拍一拍冷却时间秒数 (默认 300)"`
	MinDelaySec int  `toml:"min_delay_sec" comment:"被拍后反拍反应延时最小秒数 (默认 2)"`
	MaxDelaySec int  `toml:"max_delay_sec" comment:"被拍后反拍反应延时最大秒数 (默认 6)"`
}

// RevokeMatrixConfig 大模型消息撤回配置
type RevokeMatrixConfig struct {
	Enabled           bool `toml:"enabled" comment:"是否允许大模型撤回消息"`
	WindowSec         int  `toml:"window_sec" comment:"撤回有效时限 (微信上限 120 秒)"`
	AutoApologize     bool `toml:"auto_apologize" comment:"撤回后是否自动发送更正说明"`
	ApologizeDelaySec int  `toml:"apologize_delay_sec" comment:"更正说明发送延时秒数 (默认 3)"`
}

// SetRemarkMatrixConfig 修改好友备注配置
type SetRemarkMatrixConfig struct {
	Enabled  bool   `toml:"enabled" comment:"是否允许大模型修改好友备注"`
	Template string `toml:"template" comment:"备注命名模版 (默认 {姓名}-{公司/职位})"`
}

// ChatroomAdminMatrixConfig 群治理配置
type ChatroomAdminMatrixConfig struct {
	Enabled      bool     `toml:"enabled" comment:"是否开启智能群管"`
	KickKeywords []string `toml:"kick_keywords" comment:"广告营销违规关键词黑名单"`
}

// TypingSimulateConfig 拟人打字延时配置
type TypingSimulateConfig struct {
	Enabled bool `toml:"enabled" comment:"是否开启自然打字延时"`
	MinMs   int  `toml:"min_ms" comment:"最小延时毫秒数 (默认 1500)"`
	MaxMs   int  `toml:"max_ms" comment:"最大延时毫秒数 (默认 4500)"`
}

// VoiceReplyConfig 语音回复配置
type VoiceReplyConfig struct {
	Enabled   bool   `toml:"enabled" comment:"是否允许以语音形式回复"`
	VoiceName string `toml:"voice_name" comment:"mimo 语音音色标识"`
}

// DefaultConfig 提供开箱即用的标准默认配置
func DefaultConfig() Config {
	return Config{
		DataDir:         "runtime/gateway/data",
		DefaultProvider: "mimo",
		ModelChain:      []string{"mimo", "deepseek", "openai"},
		DefaultModel:    "mimo-v1-chat",
		Temperature:     0.7,
		MaxTokens:       2000,
		ContextRounds:   10,

		WakeWords:             []string{"@小微", "小助手", "小微", "bot"},
		PrivateChatReply:      true,
		GroupReplyProbability: 0.3,
		GroupTriggerOnAt:      true,
		GroupTriggerOnQuote:   true,
		GroupTriggerOnPat:     true,
		GroupTriggerOnWake:    true,

		Mimo: ProviderConfig{
			Enabled:    true,
			APIKey:     "",
			BaseURL:    "https://api.mimo.mi.com/v1",
			Model:      "mimo-v1-chat",
			VisionModel: "mimo-v1-vision",
			AudioModel: "mimo-v1-speech",
			TimeoutSec: 60,
		},
		DeepSeek: ProviderConfig{
			Enabled:    true,
			APIKey:     "",
			BaseURL:    "https://api.deepseek.com/v1",
			Model:      "deepseek-chat",
			TimeoutSec: 60,
		},
		OpenAI: ProviderConfig{
			Enabled:    true,
			APIKey:     "",
			BaseURL:    "https://api.openai.com/v1",
			Model:      "gpt-4o-mini",
			VisionModel: "gpt-4o",
			TimeoutSec: 60,
		},
		Claude: ProviderConfig{
			Enabled:    false,
			APIKey:     "",
			BaseURL:    "https://api.anthropic.com/v1",
			Model:      "claude-3-5-sonnet-20241022",
			TimeoutSec: 60,
		},
		Qwen: ProviderConfig{
			Enabled:    false,
			APIKey:     "",
			BaseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
			Model:      "qwen-plus",
			TimeoutSec: 60,
		},
		Ollama: ProviderConfig{
			Enabled:    false,
			APIKey:     "ollama",
			BaseURL:    "http://localhost:11434/v1",
			Model:      "llama3:latest",
			TimeoutSec: 120,
		},

		Matrix: MatrixConfig{
			Moments: MomentsMatrixConfig{
				Enabled:            true,
				CronSchedule:       "18:00",
				MinWaitSec:         30,
				MaxWaitSec:         60,
				MaxOpsPerRun:       15,
				SentimentThreshold: 60.0,
				Whitelist:          nil,
			},
			AddFriend: AddFriendMatrixConfig{
				Enabled:       true,
				Keywords:      []string{"加我", "私聊我", "加个好友", "加微信"},
				DailyLimit:    10,
				MinWaitSec:    30,
				MaxWaitSec:    60,
				GreetTemplate: "你好，刚在群里看到你的消息，特意添加交流~",
			},
			VerifyFriend: VerifyFriendMatrixConfig{
				Enabled:           true,
				KeywordFilter:     "",
				MinVerifyDelaySec: 30,
				MaxVerifyDelaySec: 90,
				AutoGreet:         "你好呀！我是{昵称}，很高兴认识你~",
				MinDelaySec:       20,
				MaxDelaySec:       45,
			},
			Pat: PatMatrixConfig{
				Enabled:     true,
				PatBack:     true,
				Proactive:   true,
				CooldownSec: 300,
				MinDelaySec: 2,
				MaxDelaySec: 6,
			},
			Revoke: RevokeMatrixConfig{
				Enabled:           true,
				WindowSec:         120,
				AutoApologize:     true,
				ApologizeDelaySec: 3,
			},
			AtMode: "smart",
			SetRemark: SetRemarkMatrixConfig{
				Enabled:  false,
				Template: "{姓名}-{公司/职位}",
			},
			ChatroomAdmin: ChatroomAdminMatrixConfig{
				Enabled:      false,
				KickKeywords: []string{"刷单", "兼职日结", "股票内幕", "代开发票", "博彩平台"},
			},
			TypingSimulate: TypingSimulateConfig{
				Enabled: true,
				MinMs:   1500,
				MaxMs:   4500,
			},
			VoiceReply: VoiceReplyConfig{
				Enabled:   false,
				VoiceName: "xiaomi-sweet",
			},
		},
	}
}

// ResolveAPIKey 智能解析 API Key（若以 $ 开头则读取环境变量）
func (p *ProviderConfig) ResolveAPIKey() string {
	val := strings.TrimSpace(p.APIKey)
	if strings.HasPrefix(val, "$") {
		// 动态从环境变量提取
		envKey := strings.TrimPrefix(val, "$")
		return strings.TrimSpace(getEnv(envKey))
	}
	return val
}

// ParseCronHourMinute 解析每日固定时间如 "18:00"
func (m *MomentsMatrixConfig) ParseCronHourMinute() (hour int, min int, ok bool) {
	parts := strings.Split(strings.TrimSpace(m.CronSchedule), ":")
	if len(parts) != 2 {
		return 18, 0, false
	}
	t, err := time.Parse("15:04", strings.TrimSpace(m.CronSchedule))
	if err != nil {
		return 18, 0, false
	}
	return t.Hour(), t.Minute(), true
}
