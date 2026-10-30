// 文件名：types.go
// 主要功能：定义控制台响应、可持久化数据和独立于微信协议的页面模型。
// 作者：2guan
package dashboard

import "encoding/json"
import "github.com/we-clay/we-clay/sdk/control"
import "github.com/we-clay/we-clay/plugins/contextcleaner"
import sdkmessage "github.com/we-clay/we-clay/sdk/message"

// ID 始终用字符串传给浏览器，防止 64 位微信 ID 丢失精度。
type Contact struct {
	Enterprise    bool     `json:"enterprise,omitempty"`
	ProfileNote   string   `json:"profile_note,omitempty"`
	WechatPinned  *bool    `json:"wechat_pinned,omitempty"`
	Muted         *bool    `json:"muted,omitempty"`
	Alias         string   `json:"alias,omitempty"`
	Signature     string   `json:"signature,omitempty"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Remark        string   `json:"remark"`
	Avatar        string   `json:"avatar"`
	Kind          string   `json:"kind"`
	Region        string   `json:"region"`
	Tags          []string `json:"tags"`
	Members       int      `json:"members"`
	Unread        int      `json:"unread"`
	Pinned        bool     `json:"pinned"`
	ModeInherited bool     `json:"mode_inherited"`
	Mode          string   `json:"mode"`
	Preview       string   `json:"preview"`
	Time          string   `json:"time"`
}
type Message struct {
	ImageAnalysis    *ImageAnalysis              `json:"image_analysis,omitempty"`
	ImageGenerated   bool                        `json:"image_generated,omitempty"`
	GenerationPrompt string                      `json:"generation_prompt,omitempty"`
	Music            *sdkmessage.Music           `json:"music,omitempty"`
	Game             string                      `json:"game,omitempty"` // 公开游戏过程在上下文中显式标记，不作为长期记忆来源。
	MentionAll       bool                        `json:"mention_all,omitempty"`
	Interaction      *contextcleaner.Interaction `json:"interaction,omitempty"`
	Mentions         []string                    `json:"mentions,omitempty"`       // 保存实际提醒对象，供失败重发复用。
	RevokeReceipt    *control.RevokeReceipt      `json:"revoke_receipt,omitempty"` // 与消息一起落盘，仅使用原始回执。
	SenderAvatar     string                      `json:"sender_avatar,omitempty"`  // 群成员不必是好友，直接保留消息来源头像。
	Rich             *RichMessage                `json:"rich,omitempty"`
	Articles         []MessageArticle            `json:"articles,omitempty"`
	Publisher        string                      `json:"publisher,omitempty"`
	SenderID         string                      `json:"sender_id,omitempty"` // 稳定成员 ID，避免同名成员统计合并。
	ID               string                      `json:"id"`
	Target           string                      `json:"target"`
	Sender           string                      `json:"sender"`
	Content          string                      `json:"content"`
	Kind             string                      `json:"kind"`
	Self             bool                        `json:"self"`
	Source           string                      `json:"source"`
	Status           string                      `json:"status"`
	Time             string                      `json:"time"`
	Media            string                      `json:"media,omitempty"`
	Duration         int                         `json:"duration,omitempty"`
}

// 识图是与原消息关联的模型描述，不覆盖原文，也不是长期记忆或操作授权。
type ImageAnalysis struct {
	Status      string `json:"status"`
	Description string `json:"description,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	ConfigHash  string `json:"config_hash,omitempty"`
	Model       string `json:"model,omitempty"`
	TaskID      string `json:"task_id,omitempty"`
	Updated     string `json:"updated"`
	Error       string `json:"error,omitempty"`
	CacheHit    bool   `json:"cache_hit,omitempty"`
}
type MomentLike struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Moment struct {
	LikeUsers      []MomentLike `json:"like_users,omitempty"`
	OriginalImages []string     `json:"original_images,omitempty"`
	Thumbnail      string       `json:"thumbnail,omitempty"`
	Link           string       `json:"link,omitempty"`
	Title          string       `json:"title,omitempty"`
	Kind           string       `json:"kind,omitempty"`
	AuthorID       string       `json:"author_id,omitempty"` // 稳定作者 ID，避免同名好友内容串入洞察。
	ID             string       `json:"id"`
	Author         string       `json:"author"`
	Avatar         string       `json:"avatar"`
	Content        string       `json:"content"`
	Images         []string     `json:"images"`
	Liked          bool         `json:"liked"`
	Likes          int          `json:"likes"`
	Time           string       `json:"time"`
	Comments       []Comment    `json:"comments"`
}
type Comment struct {
	ID        string `json:"id,omitempty"` // core 当前支持的 32 位评论编号；64 位编号不截断冒充。
	AuthorID  string `json:"author_id,omitempty"`
	Author    string `json:"author"`
	ReplyToID string `json:"reply_to_id,omitempty"`
	ReplyTo   string `json:"reply_to,omitempty"`
	Content   string `json:"content"`
}
type Plugin struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Enabled     bool     `json:"enabled"`
	Builtin     bool     `json:"builtin"`
	Status      string   `json:"status"`
	Abilities   []string `json:"abilities"`
}
type Record struct {
	ID        string `json:"id"`
	Time      string `json:"time"`
	Level     string `json:"level"`
	Component string `json:"component"`
	Message   string `json:"message"`
	Target    string `json:"target,omitempty"`
}

// InsightInfo 保存报告生成时的展示信息，后续联系人改名不改变历史报告。
type InsightInfo struct {
	TurnID         string       `json:"turn_id,omitempty"`
	Managed        bool         `json:"managed,omitempty"`
	RootID         string       `json:"root_id,omitempty"`
	ParentID       string       `json:"parent_id,omitempty"`
	Question       string       `json:"question,omitempty"`
	SnapshotID     string       `json:"snapshot_id,omitempty"`
	Range          *ReportRange `json:"range,omitempty"`
	Refresh        bool         `json:"refresh,omitempty"`
	Captured       string       `json:"captured,omitempty"`
	CacheHit       bool         `json:"cache_hit,omitempty"`
	Count          int          `json:"count,omitempty"`
	ImageCount     int          `json:"image_count,omitempty"`
	SkippedImages  int          `json:"skipped_images,omitempty"`
	Progress       string       `json:"progress,omitempty"`
	RequestedModel string       `json:"requested_model,omitempty"`
	RetryOf        string       `json:"retry_of,omitempty"`

	Type       string `json:"type"`
	Prompt     string `json:"prompt"`
	TargetName string `json:"target_name,omitempty"`
}

type Task struct {
	ReplyFollowup   bool         `json:"reply_followup,omitempty"`
	ReplyMergedInto string       `json:"reply_merged_into,omitempty"` // 尚未生成的同群批次合并目标，不作为新的回复阻止父任务重试。
	ReplyBatch      []string     `json:"reply_batch,omitempty"`       // 合并来源；手动重试重新读取归档。
	ReplyTo         string       `json:"reply_to,omitempty"`          // 自动回复对应的归档消息，重试时重新验证。
	ReplyRetryable  bool         `json:"reply_retryable,omitempty"`   // 仅尚未执行微信操作的模型失败可重新生成。
	RetryOf         string       `json:"retry_of,omitempty"`
	AutoRetries     int          `json:"auto_retries,omitempty"`
	Insight         *InsightInfo `json:"insight,omitempty"`
	Source          string       `json:"source,omitempty"`   // 区分微信自动回复与手动模型测试。
	Delivery        string       `json:"delivery,omitempty"` // 语音发送独立于合成任务，阻止重复及未知结果重试。
	ID              string       `json:"id"`
	Kind            string       `json:"kind"`
	Name            string       `json:"name"`
	Status          string       `json:"status"`
	Created         string       `json:"created"`
	Result          string       `json:"result"`
	Error           string       `json:"error,omitempty"`
	Input           string       `json:"input"`
	Model           string       `json:"model"`
	Target          string       `json:"target,omitempty"`
}

// Vendor 将厂商凭据与具体模型分离；Key 只用于写入，读取状态不包含明文。
type Vendor struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	Endpoint   string `json:"endpoint"`
	Key        string `json:"key,omitempty"`
	Configured bool   `json:"configured"`
}
type Model struct {
	VendorID      string   `json:"vendor_id,omitempty"`
	Timeout       int      `json:"timeout"`
	Temperature   *float64 `json:"temperature,omitempty"`
	Thinking      string   `json:"thinking"`
	Context       int      `json:"context"`
	ContextTokens int      `json:"context_tokens,omitempty"`
	MaxOutput     int      `json:"max_output"`

	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	Endpoint     string   `json:"endpoint"`
	Capabilities []string `json:"capabilities"`
	Enabled      bool     `json:"enabled"`
	Configured   bool     `json:"configured"`
	Key          string   `json:"key,omitempty"`
}
type Prompt struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}
type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Nickname string `json:"nickname"`
	Role     string `json:"role"`
	Enabled  bool   `json:"enabled"`
	Password string `json:"-"`
}
type Account struct {
	MomentCover string `json:"moment_cover,omitempty"`
	Photo       string `json:"photo,omitempty"`
	Name        string `json:"name"`
	ID          string `json:"id"`
	Avatar      string `json:"avatar"`
	Owner       string `json:"owner"`
	Status      string `json:"status"`
}
type Point struct {
	Date     string `json:"date"`
	Input    int    `json:"input"`
	Output   int    `json:"output"`
	Received int    `json:"received"`
	Sent     int    `json:"sent"`
}

// State 是控制台读模型；不包含密码、解密后的 API Key 或微信认证票据。
type State struct {
	PluginScopeDefaultsVersion int                       `json:"plugin_scope_defaults_version,omitempty"`
	AccountEpoch               uint64                    `json:"-"`
	ContactSync                string                    `json:"contact_sync,omitempty"`
	ConfigRevision             int64                     `json:"config_revision"`
	Revision                   int64                     `json:"revision"`
	Mock                       bool                      `json:"mock"`
	Scenario                   string                    `json:"scenario"`
	Account                    Account                   `json:"account"`
	Contacts                   []Contact                 `json:"contacts"`
	Messages                   []Message                 `json:"messages"`
	Moments                    []Moment                  `json:"moments"`
	Plugins                    []Plugin                  `json:"plugins"`
	Logs                       []Record                  `json:"logs"`
	Tasks                      []Task                    `json:"tasks"`
	Vendors                    []Vendor                  `json:"vendors"`
	Pipelines                  map[string][]string       `json:"pipelines"` // 媒体能力分别保存顺序，旧对话字段继续兼容。
	Pipeline                   []string                  `json:"pipeline"`
	Policies                   []BehaviorPolicy          `json:"policies"`
	Models                     []Model                   `json:"models"`
	Prompts                    []Prompt                  `json:"prompts"`
	Settings                   map[string]any            `json:"settings"`
	Overrides                  map[string]map[string]any `json:"overrides"`
	History                    []Change                  `json:"history"`
	Trend                      []Point                   `json:"trend"`
	Users                      []User                    `json:"users"`
}
type Change struct {
	ID      string          `json:"id"`
	Time    string          `json:"time"`
	Actor   string          `json:"actor"`
	Group   string          `json:"group"`
	Summary string          `json:"summary"`
	Before  json.RawMessage `json:"before,omitempty"`
}
