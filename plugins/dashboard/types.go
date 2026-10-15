// 文件名：types.go
// 主要功能：定义控制台响应、可持久化数据和独立于微信协议的页面模型。
// 作者：2guan
package dashboard

import "encoding/json"
import "github.com/we-clay/we-clay/sdk/control"

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
	Mentions      []string               `json:"mentions,omitempty"`       // 保存实际提醒对象，供失败重发复用。
	RevokeReceipt *control.RevokeReceipt `json:"revoke_receipt,omitempty"` // 与消息一起落盘，仅使用原始回执。
	SenderAvatar  string                 `json:"sender_avatar,omitempty"`  // 群成员不必是好友，直接保留消息来源头像。
	Rich          *RichMessage           `json:"rich,omitempty"`
	Articles      []MessageArticle       `json:"articles,omitempty"`
	Publisher     string                 `json:"publisher,omitempty"`
	SenderID      string                 `json:"sender_id,omitempty"` // 稳定成员 ID，避免同名成员统计合并。
	ID            string                 `json:"id"`
	Target        string                 `json:"target"`
	Sender        string                 `json:"sender"`
	Content       string                 `json:"content"`
	Kind          string                 `json:"kind"`
	Self          bool                   `json:"self"`
	Source        string                 `json:"source"`
	Status        string                 `json:"status"`
	Time          string                 `json:"time"`
	Media         string                 `json:"media,omitempty"`
	Duration      int                    `json:"duration,omitempty"`
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
	Author  string `json:"author"`
	Content string `json:"content"`
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
	Type       string `json:"type"`
	Prompt     string `json:"prompt"`
	TargetName string `json:"target_name,omitempty"`
}

type Task struct {
	Insight  *InsightInfo `json:"insight,omitempty"`
	Source   string       `json:"source,omitempty"`   // 区分微信自动回复与手动模型测试。
	Delivery string       `json:"delivery,omitempty"` // 语音发送独立于合成任务，阻止重复及未知结果重试。
	ID       string       `json:"id"`
	Kind     string       `json:"kind"`
	Name     string       `json:"name"`
	Status   string       `json:"status"`
	Created  string       `json:"created"`
	Result   string       `json:"result"`
	Error    string       `json:"error,omitempty"`
	Input    string       `json:"input"`
	Model    string       `json:"model"`
	Target   string       `json:"target,omitempty"`
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
	VendorID    string   `json:"vendor_id,omitempty"`
	Timeout     int      `json:"timeout"`
	Temperature *float64 `json:"temperature,omitempty"`
	Thinking    string   `json:"thinking"`
	Context     int      `json:"context"`
	MaxOutput   int      `json:"max_output"`

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
	ContactSync    string                    `json:"contact_sync,omitempty"`
	ConfigRevision int64                     `json:"config_revision"`
	Revision       int64                     `json:"revision"`
	Mock           bool                      `json:"mock"`
	Scenario       string                    `json:"scenario"`
	Account        Account                   `json:"account"`
	Contacts       []Contact                 `json:"contacts"`
	Messages       []Message                 `json:"messages"`
	Moments        []Moment                  `json:"moments"`
	Plugins        []Plugin                  `json:"plugins"`
	Logs           []Record                  `json:"logs"`
	Tasks          []Task                    `json:"tasks"`
	Vendors        []Vendor                  `json:"vendors"`
	Pipelines      map[string][]string       `json:"pipelines"` // 媒体能力分别保存顺序，旧对话字段继续兼容。
	Pipeline       []string                  `json:"pipeline"`
	Policies       []BehaviorPolicy          `json:"policies"`
	Models         []Model                   `json:"models"`
	Prompts        []Prompt                  `json:"prompts"`
	Settings       map[string]any            `json:"settings"`
	Overrides      map[string]map[string]any `json:"overrides"`
	History        []Change                  `json:"history"`
	Trend          []Point                   `json:"trend"`
	Users          []User                    `json:"users"`
}
type Change struct {
	ID      string          `json:"id"`
	Time    string          `json:"time"`
	Actor   string          `json:"actor"`
	Group   string          `json:"group"`
	Summary string          `json:"summary"`
	Before  json.RawMessage `json:"before,omitempty"`
}
