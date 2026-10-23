// 文件名：safety_schema.go
// 主要功能：定义防护及清洗插件的真实生效配置、默认注册与配置校验。
// 作者：2guan
package dashboard

import (
	"encoding/json"
	"fmt"
	"github.com/we-clay/we-clay/plugins/promptguard"
	"slices"
	"strings"
)

func safetyGroups() []Group {
	return []Group{
		{"prompt_guard", "提示词防护", "检查不可信内容，权限仍由后端独立校验。", "plugins", true, "下次请求", []Field{
			choice("mode", "工作模式", "observe 仅记录；standard 拦截明确攻击；strict 同时拦截可疑探询。", "standard", "observe", "standard", "strict"),
			entry("sources", "检查入口", "逗号分隔：wechat、dashboard、analysis、editor、plugin。", "wechat,dashboard,analysis,editor,plugin"),
			flag("output", "检查模型输出及工具参数", "处理后再保存、发送或执行，不通过备用模型绕过拦截。", true),
			choice("blocked_reply", "微信拦截处理", "silent 不回复；notice 发送固定说明。两种方式都不把攻击消息用于后续上下文。", "silent", "silent", "notice"),
			entry("notice", "固定说明", "只用于已允许自动回复的会话，不调用模型生成。", "这条请求无法自动处理，请换一种表述。"),
			entry("rules", "自定义规则 JSON", "可在验证窗口的规则页可视化编辑。", "[]"),
			entry("overrides", "会话模式覆盖 JSON", "键为会话唯一 ID；值为 observe、standard 或 strict。", "{}"),
			number("retention", "记录保留天数", "只保存阶段、规则、动作与数量，不保存原文；最多 1000 条。", 7, 1, 90),
		}},
		{"context_cleaner", "上下文清洗", "只整理模型可见副本，不更改归档消息。", "plugins", true, "下次请求", []Field{
			flag("controls", "移除异常控制字符", "保留换行、制表及正文。", true),
			flag("dedupe", "去除重复事件", "按消息 ID 去重，不按相同文本删除重复发言。", true),
			flag("media", "历史媒体瘦身", "用媒体说明代替重复的大块数据；不改变本轮上传。", true),
			flag("thinking", "清理推理标签", "移除明确推理标签，不把拒答或提及系统提示词视为泄露。", true),
			flag("silence", "识别不回复标记", "仅聊天回复中的 NO_REPLY / [SILENT] 生效；支持简短原因，仅记日志，不发到微信。", true),
			flag("stage", "清理聊天动作旁白", "仅清理少量明确舞台动作；报告、代码和编辑结果不应用。", false),
			number("max_input", "请求容量上限（估算 Token）", "保守估算，另预留最大输出与系统提示；分析资料超限报错，不静默删减。", 128000, 4096, 2000000),
		}},
	}
}
func isSafetyPlugin(id string) bool { return id == "prompt_guard" || id == "context_cleaner" }
func safetyPlugins() []Plugin {
	return []Plugin{
		{"prompt_guard", "提示词防护", "检查指令覆盖和越权尝试，隔离可疑资料并记录处理原因。", "1.0.0", true, true, "ready", []string{}},
		{"context_cleaner", "上下文清洗", "整理模型上下文、控制容量与清理推理标签，保留原始消息。", "1.0.0", true, true, "ready", []string{}},
	}
}
func validateSafetySetting(group, key string, value any) error {
	if group != "prompt_guard" {
		return nil
	}
	text, _ := value.(string)
	switch key {
	case "rules":
		var rules []promptguard.Rule
		if json.Unmarshal([]byte(text), &rules) != nil || rules == nil {
			return fmt.Errorf("规则必须是有效 JSON 数组")
		}
		return promptguard.Validate(rules)
	case "overrides":
		var overrides map[string]string
		if json.Unmarshal([]byte(text), &overrides) != nil || overrides == nil || len(overrides) > 500 {
			return fmt.Errorf("会话覆盖必须是 JSON 对象，最多 500 个会话")
		}
		for id, mode := range overrides {
			if strings.TrimSpace(id) == "" || !slices.Contains([]string{"observe", "standard", "strict"}, mode) {
				return fmt.Errorf("会话覆盖的 ID 或模式无效")
			}
		}
	case "sources":
		for _, source := range strings.Split(text, ",") {
			if !slices.Contains([]string{"wechat", "dashboard", "analysis", "editor", "plugin"}, strings.TrimSpace(source)) {
				return fmt.Errorf("检查入口无效")
			}
		}
	}
	return nil
}
