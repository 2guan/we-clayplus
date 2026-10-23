// 文件名：behaviors.go
// 主要功能：按 core 能力清单保存逐项行为授权，在 AI 操作入口强制检查范围与黑白名单。
// 作者：2guan
package dashboard

import (
	"context"
	"fmt"
	"github.com/we-clay/we-clay/plugins/ai"
	"regexp"
	"slices"
	"strings"

	"github.com/we-clay/we-clay/sdk/control"
)

// 启动时补齐默认策略；范围和名单按目标唯一 ID 精确匹配。
type BehaviorPolicy struct {
	Actors     []string `json:"actors,omitempty"`
	AllowCross bool     `json:"allow_cross,omitempty"`
	Confirm    bool     `json:"confirm,omitempty"`
	Operation  string   `json:"operation"`
	Enabled    bool     `json:"enabled"`
	Prompt     string   `json:"prompt"`
	Scope      string   `json:"scope"`
	ListMode   string   `json:"list_mode"`
	Targets    []string `json:"targets"`
}

func (s *Server) saveBehavior(p *BehaviorPolicy) error {
	if p == nil {
		return fmt.Errorf("缺少行为配置")
	}
	parts := strings.Split(p.Operation, ".")
	if len(parts) != 2 || !slices.Contains(s.cfg.Capabilities[parts[0]].Enabled, parts[1]) {
		return fmt.Errorf("当前 core 未开放此操作")
	}
	if !slices.Contains([]string{"all", "private", "group", "self"}, p.Scope) || !slices.Contains([]string{"allow", "deny"}, p.ListMode) {
		return fmt.Errorf("影响范围或名单模式无效")
	}
	if len(p.Prompt) > 8000 || len(p.Targets) > 500 || len(p.Actors) > 500 {
		return fmt.Errorf("提示词或名单过长")
	}
	value := *p
	value.Actors = nil
	for _, id := range p.Actors {
		id = strings.TrimSpace(id)
		if id != "" && !slices.Contains(value.Actors, id) {
			value.Actors = append(value.Actors, id)
		}
	}
	if strings.TrimSpace(value.Prompt) == "" {
		value.Prompt = defaultBehavior(value.Operation).Prompt
	}
	value.Targets = []string{}
	for _, id := range p.Targets {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(value.Targets, id) {
			value.Targets = append(value.Targets, id)
		}
	}
	return s.store.change(func(st *State) error {
		for i := range st.Policies {
			if st.Policies[i].Operation == value.Operation {
				st.Policies[i] = value
				return nil
			}
		}
		st.Policies = append(st.Policies, value)
		return nil
	})
}

// ExecuteAI 保留旧的目标绑定调用；新增工具必须经过会话引用、发起人和操作记录校验。
// 核心支持不代表 AI 执行器已接通。未映射操作明确拒绝，不能降级为任意 core 路径调用。
func (s *Server) ExecuteAI(ctx context.Context, operation string, command control.Command) (any, error) {
	st := s.store.snapshot()
	parts := strings.Split(operation, ".")
	if len(parts) != 2 || !slices.Contains(s.cfg.Capabilities[parts[0]].Enabled, parts[1]) {
		return nil, fmt.Errorf("当前 core 未开放此操作")
	}
	if s.cfg.DisableAI || !slices.ContainsFunc(st.Plugins, func(p Plugin) bool { return p.ID == "ai" && p.Enabled }) {
		return nil, fmt.Errorf("AI 插件已停用")
	}
	if err := behaviorAllowed(st, operation, command.Target); err != nil {
		return nil, err
	}
	target := command.Target
	if target == "" {
		target = st.Account.ID
	}
	mapped := control.AIOperations[operation]
	if mapped == "" {
		return nil, fmt.Errorf("此 core 能力尚未接入 AI 执行器，授权配置已保留")
	}
	p := actionPolicy(st, operation)
	if len(p.Actors) > 0 || p.Confirm {
		return nil, fmt.Errorf("此行为必须经可信对话执行，不能绕过发起人或确认规则")
	}
	if mapped == "ai-action" && (operation != "message.Send" || len(command.Params) > 0) {
		return nil, fmt.Errorf("此行为必须经可信对话和引用校验执行")
	}
	// 目标始终来自受校验命令；不信任模型自行填写底层操作名。
	command.Operation = mapped
	if mapped == "ai-action" {
		command.ID = operation
	}
	if operation == "message.Send" && len(command.Params) == 0 {
		return nil, s.deliverReply(ctx, command.Target, command.Content)
	}
	if s.cfg.Mock {
		return map[string]any{"mock": true, "operation": operation, "target": target}, nil
	}
	if s.host == nil {
		return nil, fmt.Errorf("微信尚未就绪")
	}
	return s.host.Execute(ctx, command)
}

// 工具展示和执行共用同一授权判断，防止模型反复请求不可用操作。
func behaviorAllowed(st State, operation, target string) error {
	index := slices.IndexFunc(st.Policies, func(p BehaviorPolicy) bool { return p.Operation == operation })
	if index < 0 || !st.Policies[index].Enabled {
		return fmt.Errorf("此行为未获得 AI 授权")
	}
	p := st.Policies[index]
	if target == "" {
		target = st.Account.ID
	}
	group := strings.HasSuffix(target, "@chatroom") || strings.HasSuffix(target, "@im.chatroom") || slices.ContainsFunc(st.Contacts, func(c Contact) bool { return c.ID == target && c.Kind == "group" })
	if p.Scope == "group" && !group || p.Scope == "private" && (group || target == st.Account.ID) || p.Scope == "self" && target != st.Account.ID {
		return fmt.Errorf("目标不在行为影响范围内")
	}
	listed := slices.Contains(p.Targets, target)
	if p.ListMode == "allow" && !listed || p.ListMode == "deny" && listed {
		return fmt.Errorf("目标未通过行为名单规则")
	}
	return nil
}

// 兼容测试及原会话操作的工具查询；真实对话带发起人身份进入完整目录。
func (s *Server) behaviorTools(target string) []ai.Tool {
	return s.behaviorToolsFor(ai.Request{Target: target})
}

type writeIntentMissing struct{}

func (writeIntentMissing) Error() string {
	return "请在当前消息里明确写出要修改的群名、公告或备注，以及完整的新内容，我再执行。"
}

var directWritePatterns = map[string][]*regexp.Regexp{
	"chatroom.SetName": {
		regexp.MustCompile(`(?s)^(?:请|麻烦|帮我|给我)?\s*(?:把|将)?\s*(?:本群的|这个群的|当前群的)?群名\s*(?:改成|改为|修改为|设置为|设为|换成|修改|设置)\s*[:：]?\s*(.+)$`),
		regexp.MustCompile(`(?s)^(?:请|麻烦|帮我|给我)?\s*(?:改|修改|更改|设置|更新)\s*群名\s*(?:为|成|[:：])\s*(.+)$`),
		regexp.MustCompile(`^用[^\n]+建群并命名为\s*([^，。；;\n]+)(?:[，。；;]|$)`),
	},
	"chatroom.SetAnnouncement": {
		regexp.MustCompile(`(?s)^(?:请|麻烦|帮我|给我)?\s*(?:把|将)?\s*(?:群公告|公告)\s*(?:改成|改为|修改为|设置为|设为|发布|更新|修改|设置)\s*[:：]?\s*(.+)$`),
		regexp.MustCompile(`(?s)^(?:请|麻烦|帮我|给我)?\s*(?:发布|修改|更新|设置)\s*(?:群公告|公告)\s*[:：]?\s*(.+)$`),
	},
	"contact.SetRemark": {
		regexp.MustCompile(`(?s)^(?:请|麻烦|帮我|给我)?\s*(?:把|将)?\s*备注\s*(?:改成|改为|修改为|设置为|设为|换成|修改|设置)\s*[:：]?\s*(.+)$`),
		regexp.MustCompile(`(?s)^(?:请|麻烦|帮我|给我)?\s*(?:改|修改|更改|设置|更新)\s*备注\s*(?:为|成|[:：])\s*(.+)$`),
	},
}

// 仅当前收到的原始纯文字可以授权写操作；历史、引用、转述与模型生成的正文都不是授权。
func (s *Server) currentWriteIntent(q ai.Request, operation, content string) bool {
	if q.Conversation == nil || q.Conversation.Current.ID == "" {
		return false
	}
	st := s.store.snapshot()
	for _, m := range st.Messages {
		if m.ID != q.Conversation.Current.ID || m.Target != q.Target {
			continue
		}
		if m.Self || m.Kind != "text" || !contextAvailable(m) {
			return false
		}
		text := strings.TrimSpace(m.Content)
		for strings.HasPrefix(text, "@") {
			if !slices.Contains(m.Mentions, st.Account.ID) {
				return false
			}
			fields := strings.Fields(text)
			if len(fields) < 2 {
				return false
			}
			text = strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
		}
		for _, pattern := range directWritePatterns[operation] {
			if match := pattern.FindStringSubmatch(text); len(match) == 2 && strings.TrimSpace(match[1]) == strings.TrimSpace(content) {
				return true
			}
		}
		return false
	}
	return false
}

var operationTopic = regexp.MustCompile(`群名|群公告|备注`)
var operationClaim = regexp.MustCompile(`改好了|改好啦|改了|改完|已修改|已经修改|已更新|已设置|提交了一遍|刚点进去|执行成功|我这边显示|缓存没刷|缓存的事|缓存问题|缓存没更新|你自己刷新`)
var operationNegation = regexp.MustCompile(`没有(?:真正|实际)?(?:改|修改|执行|提交)|(?:没|未|尚未)(?:改|修改|执行|提交)|无法|不能|并未|不代表`)
var operationFollowup = regexp.MustCompile(`再改|重新改|继续改|再设置|重新设置|再提交|按.{0,8}(刚才|刚刚|之前|上次)`)
var otherEditTopic = regexp.MustCompile(`文案|标题|文章|稿子|代码|图片|照片|句子|段落|台词`)

// 此检查只拦截明显无回执的成功声明，不决定修改意图，也不要求普通回复调用工具。
func replyOperationError(q ai.Request, result ai.Result) error {
	if len(result.ToolCalls) > 0 {
		return nil
	}
	topic := operationTopic.MatchString(q.Prompt) || operationTopic.MatchString(result.Text)
	// 仅在当前发言明确承接先前操作、且未提出另一个编辑对象时引用历史话题。
	// 历史里提过群名不能让本轮普通文案编辑的“改好了”被误判。
	if !topic && operationFollowup.MatchString(q.Prompt) && !otherEditTopic.MatchString(q.Prompt) {
		for i := max(0, len(q.Context)-8); i < len(q.Context); i++ {
			topic = topic || operationTopic.MatchString(q.Context[i].Text)
		}
	}
	if topic && operationClaim.MatchString(result.Text) && !operationNegation.MatchString(result.Text) {
		return fmt.Errorf("回复声称已修改资料或归因于缓存，但本轮没有操作回执，已停止发送")
	}
	return nil
}
