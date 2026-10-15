// 文件名：behaviors.go
// 主要功能：按 core 能力清单保存逐项行为授权，在 AI 操作入口强制检查范围与黑白名单。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/we-clay/we-clay/plugins/ai"
	"slices"
	"strings"

	"github.com/we-clay/we-clay/sdk/control"
)

// 启动时补齐默认策略；范围和名单按目标唯一 ID 精确匹配。
type BehaviorPolicy struct {
	Operation string   `json:"operation"`
	Enabled   bool     `json:"enabled"`
	Prompt    string   `json:"prompt"`
	Scope     string   `json:"scope"`
	ListMode  string   `json:"list_mode"`
	Targets   []string `json:"targets"`
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
	if len(p.Prompt) > 8000 || len(p.Targets) > 500 {
		return fmt.Errorf("提示词或名单过长")
	}
	value := *p
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

// AI 行为执行必须从此入口进入；手动后台操作仍沿用原先的独立权限检查。
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
	// 目标始终来自受校验命令；不信任模型自行填写底层操作名。
	command.Operation = mapped
	if operation == "message.Send" {
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

// 对话中只开放目标绑定且已有参数契约的文本操作，模型不能自行指定其他联系人。
// 其余 core 行为保留独立配置，等待对应 AI 参数适配接入后再开放执行。
var conversationOperations = []string{"message.Send", "contact.SetRemark", "chatroom.SetName", "chatroom.SetAnnouncement"}

func (s *Server) behaviorTools(target string) []ai.Tool {
	st := s.store.snapshot()
	tools := []ai.Tool{}
	for _, p := range st.Policies {
		// 普通回复直接走统一输出协议，不再重复提供文本发送工具。
		parts := strings.Split(p.Operation, ".")
		if len(parts) != 2 || !slices.Contains(s.cfg.Capabilities[parts[0]].Enabled, parts[1]) || behaviorAllowed(st, p.Operation, target) != nil || p.Operation == "message.Send" || !slices.Contains(conversationOperations, p.Operation) {
			continue
		}
		tools = append(tools, ai.Tool{Name: strings.ReplaceAll(p.Operation, ".", "__"), Description: p.Operation + "。仅对当前会话执行。" + p.Prompt})
	}
	return tools
}
func (s *Server) executeTool(ctx context.Context, target string, call ai.ToolCall) error {
	operation := strings.ReplaceAll(call.Name, "__", ".")
	if target == "" || !slices.Contains(conversationOperations, operation) {
		return fmt.Errorf("模型请求了未开放的操作")
	}
	var args struct {
		Content string `json:"content"`
	}
	if json.Unmarshal([]byte(call.Arguments), &args) != nil || strings.TrimSpace(args.Content) == "" || len(args.Content) > 8000 {
		return fmt.Errorf("AI 行为参数无效")
	}
	st := s.store.snapshot()
	if st.Settings["reply.enabled"] != true || !slices.ContainsFunc(st.Contacts, func(c Contact) bool { return c.ID == target && c.Mode == "auto" }) {
		return fmt.Errorf("会话已暂停自动处理")
	}
	if !replyTargetAllowed(st, target) {
		return fmt.Errorf("会话已移出自动回复名单")
	}
	_, err := s.ExecuteAI(ctx, operation, control.Command{Target: target, Content: args.Content})
	return err
}
