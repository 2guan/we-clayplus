// 文件名: plugins/dashboard/types.go
// 主要功能: Dashboard Web 控制台全站请求、响应及数据库持久化实体模型定义
// 作者: 2guan

package main

import "time"

// APIResponse 统一的 RESTful JSON 响应包裹结构
type APIResponse struct {
	Code    int    `json:"code"`              // 业务状态码: 0 或 200 为成功，其他为错误
	Message string `json:"message"`           // 状态说明文字
	Data    any    `json:"data,omitempty"`    // 响应荷载数据
}

// User 数据库中存储的用户账号实体
type User struct {
	ID           int64      `json:"id"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"-"` // 不向前端暴露密文
	Role         string     `json:"role"` // "admin" 或 "operator"
	DisplayName  string     `json:"display_name"`
	Enabled      bool       `json:"enabled"`
	CreatedAt    time.Time  `json:"created_at"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
}

// TargetOverride 单个好友或微信群的专属策略实体
type TargetOverride struct {
	TargetID       string    `json:"target_id"`       // wxid 或群聊 ID (chatroom_id)
	TargetType     string    `json:"target_type"`     // "contact" (好友) 或 "chatroom" (群聊)
	TargetName     string    `json:"target_name"`     // 好友备注名/昵称，或群聊名称
	CustomPrompt   string    `json:"custom_prompt"`   // 专属人设提示词
	PersonaSummary string    `json:"persona_summary"` // 智能画像分析结果回写摘要
	ModelName      string    `json:"model_name"`      // 指定绑定的专属模型标识
	ReplyRate      float64   `json:"reply_rate"`      // 专属回复概率: -1 表示沿用全局，0.0~1.0 为独立设置
	ContextRounds  int       `json:"context_rounds"`  // 专属记忆轮数: -1 表示沿用全局
	SilentMode     bool      `json:"silent_mode"`     // 是否静默监听只记录不回复
	UpdatedAt      time.Time `json:"updated_at"`
}

// ChatMessage 聊天控制台展示与持久化审计的消息实体
type ChatMessage struct {
	MsgID        string    `json:"msg_id"`
	SessionID    string    `json:"session_id"`             // 会话接收方或群ID
	SenderID     string    `json:"sender_id"`              // 发送方 wxid
	SenderName   string    `json:"sender_name"`            // 发送方展示名称/群昵称
	SenderAvatar string    `json:"sender_avatar,omitempty"`// 发送方头像
	MsgType      string    `json:"msg_type"`               // "text", "image", "voice", "video", "file", "emoji" 等
	Content      string    `json:"content"`                // 文本内容或媒体描述
	MediaPath    string    `json:"media_path,omitempty"`   // 本地媒体缓存相对路径或直链
	IsSelf       bool      `json:"is_self"`                // 是否为我方发出的消息
	IsOutgoing   bool      `json:"is_outgoing"`            // 前端对齐字段
	CreatedAt    time.Time `json:"created_at"`
}

// TokenLog 大模型每次对话调用的 Token 消耗审计记录
type TokenLog struct {
	ID               int64     `json:"id"`
	SessionID        string    `json:"session_id"`
	ModelName        string    `json:"model_name"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	DurationMs       int64     `json:"duration_ms"`
	CreatedAt        time.Time `json:"created_at"`
}

// OverviewData 运行总览一屏大盘的数据聚合模型
type OverviewData struct {
	// 机器人基本运行状态
	WeChatOnline    bool   `json:"wechat_online"`
	SelfUsername    string `json:"self_username"`
	SelfNickname    string `json:"self_nickname"`
	SelfAvatar      string `json:"self_avatar"`
	OwnerNickname   string `json:"owner_nickname"`
	OwnerUsername   string `json:"owner_username"`
	UptimeSeconds   int64  `json:"uptime_seconds"`
	
	// 资产规模统计
	FriendCount     int    `json:"friend_count"`
	ChatroomCount   int    `json:"chatroom_count"`
	MemberCoverage  int    `json:"member_coverage"`
	TotalMessages   int64  `json:"total_messages"`
	LoadedPlugins   int    `json:"loaded_plugins"`
	ActivePlugins   int    `json:"active_plugins"`

	// 大模型调用统计
	TodayTokens     int64  `json:"today_tokens"`
	TodayChats      int    `json:"today_chats"`
	WeekTokens      int64  `json:"week_tokens"`
	WeekChats       int    `json:"week_chats"`

	// 趋势图表数据
	TokenDates      []string `json:"token_dates"`
	PromptTokens    []int64  `json:"prompt_tokens"`
	CompletionTokens []int64 `json:"completion_tokens"`
	ModelPieLabels  []string `json:"model_pie_labels"`
	ModelPieValues  []int64  `json:"model_pie_values"`

	// 消息吞吐趋势图表数据
	TrafficDates    []string `json:"traffic_dates"`
	InboundMsgs     []int64  `json:"inbound_msgs"`
	OutboundMsgs    []int64  `json:"outbound_msgs"`
	MsgTypeLabels   []string `json:"msg_type_labels"`
	MsgTypeValues   []int64  `json:"msg_type_values"`
}

// SystemSettings 系统基础与平台品牌定制持久化模型
type SystemSettings struct {
	SiteTitle  string `json:"site_title"`
	FooterText string `json:"footer_text"`
	Owner      string `json:"owner"`
	Forbidden  string `json:"forbidden"`
}

// AIMatrixSettings AI 微信底层能力授权矩阵与全维度防风控拟人延时配置模型
type AIMatrixSettings struct {
	// 1. 朋友圈批量互动
	MomentsEnabled  bool   `json:"moments_enabled"`
	MomentsTimes    string `json:"moments_times"`
	MomentsMinWait  int    `json:"moments_min_wait"`
	MomentsMaxWait  int    `json:"moments_max_wait"`
	MomentsMaxOps   int    `json:"moments_max_ops"`
	LikeThreshold   int    `json:"like_threshold"`

	// 2. 群内加好友
	AddFriendEnabled bool   `json:"add_friend_enabled"`
	AddFriendMsg     string `json:"add_friend_msg"`
	AddFriendLimit   int    `json:"add_friend_limit"`
	AddFriendMinWait int    `json:"add_friend_min_wait"`
	AddFriendMaxWait int    `json:"add_friend_max_wait"`

	// 3. 通过好友申请 (含收到申请后延时通过、通过后延时打招呼)
	AutoVerify     bool   `json:"auto_verify"`
	VerifyKeyword  string `json:"verify_keyword"`
	VerifyMinDelay int    `json:"verify_min_delay"` // 收到申请后延时通过最小秒数
	VerifyMaxDelay int    `json:"verify_max_delay"` // 收到申请后延时通过最大秒数
	VerifyGreet    string `json:"verify_greet"`
	GreetMinDelay  int    `json:"greet_min_delay"`  // 通过后延时打招呼最小秒数
	GreetMaxDelay  int    `json:"greet_max_delay"`  // 通过后延时打招呼最大秒数

	// 4. 拍一拍互动
	PatBack      bool `json:"pat_back"`
	PatProactive bool `json:"pat_proactive"`
	PatCooldown  int  `json:"pat_cooldown"`
	PatMinDelay  int  `json:"pat_min_delay"`
	PatMaxDelay  int  `json:"pat_max_delay"`

	// 5. 消息纠错撤回
	RevokeEnabled        bool `json:"revoke_enabled"`
	RevokeWindow         int  `json:"revoke_window"`
	RevokeAutoApologize  bool `json:"revoke_auto_apologize"`
	RevokeApologizeDelay int  `json:"revoke_apologize_delay"`

	// 6. @群友互动
	AtMode string `json:"at_mode"`

	// 7. 自然打字拟态延时
	TypingSimulate bool `json:"typing_simulate"`
	TypingMinMs    int  `json:"typing_min_ms"`
	TypingMaxMs    int  `json:"typing_max_ms"`

	// 8. 修改好友备注
	SetRemarkEnabled bool   `json:"set_remark_enabled"`
	RemarkTemplate   string `json:"remark_template"`

	// 9. 智能群聊治理
	ChatroomAdminEnabled bool   `json:"chatroom_admin_enabled"`
	ChatroomKickWords    string `json:"chatroom_kick_words"`

	// 10. 语音回复
	VoiceEnabled bool   `json:"voice_enabled"`
	VoiceName    string `json:"voice_name"`
}

// DefaultAIMatrixSettings 提供默认矩阵参数
func DefaultAIMatrixSettings() AIMatrixSettings {
	return AIMatrixSettings{
		MomentsEnabled:  true,
		MomentsTimes:    "18:00",
		MomentsMinWait:  30,
		MomentsMaxWait:  60,
		MomentsMaxOps:   15,
		LikeThreshold:   60,

		AddFriendEnabled: false,
		AddFriendMsg:     "你好呀，看到你在群里找我，特来加你~",
		AddFriendLimit:   10,
		AddFriendMinWait: 30,
		AddFriendMaxWait: 60,

		AutoVerify:     true,
		VerifyKeyword:  "",
		VerifyMinDelay: 30,
		VerifyMaxDelay: 90,
		VerifyGreet:    "你好呀！我是{昵称}，很高兴认识你~",
		GreetMinDelay:  20,
		GreetMaxDelay:  45,

		PatBack:      true,
		PatProactive: true,
		PatCooldown:  300,
		PatMinDelay:  2,
		PatMaxDelay:  6,

		RevokeEnabled:        true,
		RevokeWindow:         120,
		RevokeAutoApologize:  true,
		RevokeApologizeDelay: 3,

		AtMode:         "smart",
		TypingSimulate: true,
		TypingMinMs:    1500,
		TypingMaxMs:    4500,

		SetRemarkEnabled:     false,
		RemarkTemplate:       "{姓名}-{公司/职位}",
		ChatroomAdminEnabled: false,
		ChatroomKickWords:    "刷单, 兼职日结, 股票内幕, 代开发票, 博彩平台",

		VoiceEnabled: false,
		VoiceName:    "xiaomi-sweet",
	}
}
