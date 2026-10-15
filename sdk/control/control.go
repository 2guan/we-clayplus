// 文件名：control.go
// 主要功能：定义基础插件与 gateway 之间的控制台契约，避免插件导入内部实现。
// 作者：2guan
package control

import (
	"context"
	"encoding/json"
)

// Config 只包含启动配置；业务配置通过控制台保存，不包含明文凭据。
type Config struct {
	Capabilities map[string]Capability `toml:"-"`
	Address      string                `toml:"address"`
	DataDir      string                `toml:"data_dir"`
	Mock         bool                  `toml:"mock"`
	Disabled     bool                  `toml:"disabled"`
	DisableAI    bool                  `toml:"disable_ai"`
}

// Command 使用有限操作名，不允许浏览器提交任意 core 路径。
type Command struct {
	Operation string          `json:"operation"`
	Target    string          `json:"target"`
	Content   string          `json:"content"`
	ID        string          `json:"id"`
	Params    json.RawMessage `json:"params,omitempty"`
}

// Host 由 gateway 注入；Mock 模式根本不调用此接口。
// message-file 返回 io.ReadCloser，由调用方读取至 EOF 并关闭；仅用于进程内附件下载。
type Host interface {
	Execute(context.Context, Command) (any, error)
}

// Capability 来源于 gateway 能力清单，仅描述可用性，不授予执行权限。
type Capability struct {
	Label       string   `json:"label"`
	Enabled     []string `json:"enabled"`
	Placeholder []string `json:"placeholder"`
	Evidence    string   `json:"evidence"`
}

// AIOperations 只列出已具备有限参数适配的行为，禁止模型自行提交 core 路径。
var AIOperations = map[string]string{
	"message.Send": "send", "contact.SetRemark": "remark", "chatroom.SetName": "group-name", "chatroom.SetAnnouncement": "announcement", "moments.Like": "moment-like", "moments.Unlike": "moment-unlike", "moments.Comment": "moment-comment",
}

// RevokeReceipt 采用字符串保存原始大整数，避免浏览器或 JSON 数字精度损失。
type RevokeReceipt struct {
	Account  string `json:"account"`
	ClientID string `json:"client_id"`
	// OriginalClientID 保留媒体协议中的字符串 ID，与数值型 ClientID 互斥。
	OriginalClientID string `json:"original_client_id,omitempty"`
	Created          string `json:"create_time"`
}
