// 文件名：schema.go
// 主要功能：以统一字段定义驱动设置表单、默认值和服务端参数校验。
// 作者：2guan
package dashboard

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// Field 中的范围和枚举同时用于前端展示与后端校验，不能只依赖浏览器限制。
type Field struct {
	Preview bool     `json:"preview"`
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Type    string   `json:"type"`
	Default any      `json:"default"`
	Help    string   `json:"help"`
	Options []string `json:"options,omitempty"`
	Min     float64  `json:"min,omitempty"`
	Max     float64  `json:"max,omitempty"`
}
type Group struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Area        string  `json:"area"`
	Admin       bool    `json:"admin"`
	Effect      string  `json:"effect"`
	Fields      []Field `json:"fields"`
}

func flag(key, label, help string, value bool) Field {
	return Field{Key: key, Label: label, Type: "boolean", Default: value, Help: help}
}
func number(key, label, help string, value, min, max float64) Field {
	return Field{Key: key, Label: label, Type: "number", Default: value, Min: min, Max: max, Help: help}
}
func choice(key, label, help, value string, options ...string) Field {
	return Field{Key: key, Label: label, Type: "select", Default: value, Options: options, Help: help}
}
func entry(key, label, help, value string) Field {
	return Field{Key: key, Label: label, Type: "text", Default: value, Help: help}
}

// Schema 明确区分运行参数和策略；不把“有一个开关”等同于 core 已支持该能力。
func Schema() []Group {
	groups := []Group{
		{"reply", "自动回复规则", "控制自动回复的全局边界。手动发送不会切换会话模式；单个群的回复概率可在会话设置中覆盖。", "models", false, "下次请求", []Field{
			choice("default_friend", "好友默认模式", "未单独设置的好友跟随此项；自动仍受总开关和生效目标限制。", "人工", "人工", "自动"),
			choice("default_group", "群聊默认模式", "未单独设置的群聊跟随此项；自动仍受总开关、目标及触发规则限制。", "人工", "人工", "自动"),
			flag("enabled", "自动回复", "默认开启；启用后还需会话处于自动模式、通过名单规则且有可用模型。", true),
			choice("list_mode", "名单模式", "好友和群聊共用；黑名单模式下，未被排除且开启自动的会话可回复。", "黑名单", "黑名单", "白名单"),
			entry("blacklist", "黑名单", "填写不允许自动回复的会话 ID，以逗号分隔；留空不排除任何会话。", ""),
			entry("targets", "白名单", "仅白名单模式使用；填写允许自动回复的会话 ID，以逗号分隔。", ""),
			choice("private", "私聊触发", "仅对通过名单规则的会话生效。", "全部消息", "全部消息", "关键词", "关闭"),
			entry("keywords", "触发关键词", "多个关键词使用逗号分隔。", ""),
			number("probability", "普通群消息回复概率 (%)", "真实 @ 本人始终触发；100% 时每批交给模型判断，不受冷却限制。群聊停顿 7 秒后合并，首条最多等待 15 秒；模型可判断不回复。仍需自动模式并通过名单规则。", 0, 0, 100),
			flag("skip_other_mentions", "群聊 @ 他人时不插话", "默认开启：真实 @ 其他成员且未 @ 本人时跳过自动回复，100% 概率也不例外；同时 @ 本人仍触发。", true),
			number("cooldown", "回复冷却 (秒)", "普通消息回复的最短间隔；真实 @ 本人及 100% 的群消息不受此项限制。", 15, 0, 3600),
			number("max_parts", "每轮最多消息数", "通常回复 1–3 条；双换行分条，语音和图片也计入上限。", 5, 1, 5),
			number("interval_ms", "分条发送间隔 (毫秒)", "逐条发送；期间人工接管立即停止。", 700, 0, 5000),
			flag("emoji_enabled", "允许自动发送表情", "只使用表情库中填写含义并授权 AI 的表情，每轮最多一张。", false),
			flag("voice_enabled", "允许自动发送语音", "根据 voice 标签调用语音流水线，每轮最多一条。", true),
			flag("image_enabled", "允许自动生成并发送图片", "根据 image 标签调用图片流水线，每轮最多一张；需配置生图模型。", true),
			entry("quiet", "静默时段", "例如 23:00-08:00；留空表示不启用。", ""),
		}},
		{"memory", "上下文与记忆", "聊天归档与 AI 记忆相互独立。模型单独配置时优先使用模型上下文参数。", "models", true, "下次请求", []Field{
			flag("enabled", "注入已确认记忆", "仅使用来源有效且允许在当前会话使用的记忆；会话还需单独开启。", false),
			flag("direct_enabled", "允许本人私聊明确记忆", "仅已开启记忆的私聊本人低敏偏好可以直接生效，其余内容后台审核。", false),
			flag("scan_enabled", "允许从聊天记录抽取", "管理员手动选择会话和范围后，将资料发送给配置的模型生成候选。", false),
			entry("timezone", "回复时区", "IANA 时区；时间由服务端生成，消息时间与请求时间分别提供。", "Asia/Shanghai"),
			number("messages", "上下文消息数", "最多 99999 条，按会话隔离；实际发送仍受 Token 预算限制。", 40, 2, 99999),
			number("tokens", "上下文 Token 预算", "0 表示无限制；最多可填 9999999。按字符保守估算，超限裁剪较早消息；仍受单独配置的请求容量和模型接口限制。", 8000, 0, 9999999),
			flag("summary", "旧版历史摘要开关（停用）", "兼容旧配置；不会自动生成摘要或长期记忆。请使用AI记忆页面手动扫描审核。", false),
			number("summary_after", "旧版摘要阈值（停用）", "仅兼容旧配置，不触发任务。", 80, 20, 1000),
			number("retention", "近期对话最长回看天数", "限制普通对话选取范围；仍受上下文条数和 Token 预算限制。长期记忆使用各条目自身的有效期。", 3000, 1, 3000),
		}},
		{"routing", "模型与预算", "各能力独立选择路由，未配置价格时不显示虚构费用。", "models", true, "下次请求", []Field{
			entry("chat", "对话默认模型 ID", "留空按模型列表中启用且支持对话的顺序选择。", ""),
			entry("vision", "识图默认模型 ID", "需要模型声明 vision 能力。", ""),
			entry("audio", "识音频默认模型 ID", "需要模型声明 audio 能力。", ""),
			entry("video_generation", "视频生成默认模型 ID", "需要模型声明 video_generation 能力。", ""),
			entry("video", "视频理解默认模型 ID", "需要模型声明 video 能力。", ""),
			entry("tts", "语音合成默认模型 ID", "需要模型声明 tts 能力。", ""),
			entry("image", "图片生成默认模型 ID", "需要模型声明 image 能力。", ""),
			number("timeout", "单次调用超时 (秒)", "默认 5 分钟；超时结果不自动重发微信消息。", 300, 5, 600),
			number("concurrency", "并发任务上限", "限制模型和媒体处理的资源消耗。", 2, 1, 8),
			number("daily_calls", "每日请求上限", "0 表示不设请求数量上限。", 200, 0, 100000),
			number("max_output", "单次最大输出 Token", "发送给支持该参数的模型。", 2048, 64, 32768),
			flag("fallback", "服务故障自动切换", "各能力按独立流水线切换；指定模型测活不切换，视频已受理但状态未知时停止。", true),
		}},
		{"insight", "智能洞察", "为较长报告单独设置生成预算，避免占用普通对话的输出上限。", "models", false, "下次任务", []Field{
			number("max_output", "报告最大输出 Token", "单个模型单独设置的上限优先；支持该参数的模型最多输出 32768 Token。", 8192, 1024, 32768),
		}},
		{"vision", "图片理解", "控制发送到识图模型的数据规模。", "lab", false, "下次任务", []Field{
			flag("auto", "自动识别入站图片", "仅对允许自动回复的会话生效。", true),
			number("max_mb", "图片大小上限 (MB)", "超出限制时拒绝处理并给出说明。", 10, 1, 20),
			number("max_images", "每次最多图片数", "避免一次请求占用过多上下文。", 4, 1, 10),
			choice("detail", "图像细节", "依据供应商能力适配。", "自动", "自动", "低", "高"),
			flag("cache", "复用相同图片分析", "按内容摘要与模型配置区分缓存。", true),
		}},
		{"audio", "音频理解", "转写与原生音频理解分别标记结果。", "lab", false, "下次任务", []Field{
			flag("auto", "自动识别入站语音", "需要可用的语音解码工具和模型。", true),
			choice("mode", "理解模式", "根据所选供应商的音频能力选择。", "转写", "转写", "原生理解"),
			entry("language", "语言提示", "留空由模型检测。", "zh"),
			number("duration", "最大音频时长 (秒)", "超过限制时要求缩短音频。", 120, 5, 1800),
			flag("transcript", "展示转写文本", "仅在后台显示，不冒充原消息内容。", true),
		}},
		{"video", "视频理解", "原生理解与采样分析有不同的精度边界。", "lab", false, "下次任务", []Field{
			flag("auto", "自动分析入站视频", "默认关闭，避免意外消耗。", false),
			choice("mode", "处理模式", "采样模式可能遗漏短暂画面。", "原生理解", "原生理解", "抽帧与音轨"),
			number("duration", "最长视频时长 (秒)", "限制长视频开销。", 120, 5, 1800),
			number("frames", "抽帧数量上限", "均匀抽帧并保留时间戳。", 12, 1, 60),
			flag("sound", "同时分析音轨", "输出区分画面观察和音频转写。", true),
		}},
		{"tts", "语音合成", "后台试听与微信原生语音发送分别处理。", "lab", false, "下次任务", []Field{
			choice("mode", "合成模式", "只有支持此模式的模型才能执行。", "标准音色", "标准音色", "声音设计", "声音克隆"),
			entry("voice", "发音人", "填写供应商支持的音色 ID。", "alloy"),
			entry("design", "声音描述", "自然、清晰、语气松弛。", "自然温和，清晰但不过分播音腔。"),
			number("speed", "语速", "供应商不支持时明确提示。", 1, 0.25, 4),
			choice("reply", "微信回复形式", "无标签的回复使用此偏好；显式 voice 标签优先。原生语音依赖 SILK 编码器。", "文字", "文字", "语音", "跟随来信"),
			number("max_chars", "合成文字上限", "长内容需拆分，不能静默截断。", 500, 10, 4000),
		}},
		{"video_generation", "视频生成", "适用于 OpenAI Videos 兼容接口，参数须在模型支持范围内。", "lab", false, "下次任务", []Field{
			choice("size", "默认视频尺寸", "包含横竖比例与分辨率。", "1280x720", "1280x720", "720x1280", "1792x1024", "1024x1792"),
			choice("seconds", "默认时长（秒）", "模型不支持的时长将返回错误。", "4", "4", "8", "12"),
			entry("style", "默认风格补充", "追加到本次提示词。", ""),
		}},
		{"image", "图片生成", "生成结果先保存，明确成功后再决定是否发送。", "lab", false, "下次任务", []Field{
			choice("aspect", "默认比例", "与分辨率共同决定请求尺寸。", "1:1", "1:1", "3:2", "2:3", "16:9", "9:16"),
			choice("resolution", "默认分辨率", "按长边像素设置，需模型支持。", "1024", "1024", "1536", "2048"),
			choice("size", "输出尺寸", "供应商支持范围内生效。", "1024x1024", "1024x1024", "1536x1024", "1024x1536"),
			choice("quality", "输出质量", "由模型支持情况决定。", "auto", "auto", "low", "medium", "high"),
			number("count", "生成张数", "每个任务最多生成的图片数量。", 1, 1, 4),
			flag("auto_send", "生成后自动发送", "默认先在实验室预览。", false),
			entry("style", "默认风格补充", "作为提示词补充，不覆盖用户要求。", ""),
		}},
		{"actions", "行为授权", "所有动作还要检查微信能力与目标权限。", "actions", true, "下次任务", []Field{
			flag("like", "自动点赞", "仅允许白名单及每日限额内操作。", false),
			flag("comment", "自动评论", "需要当前 core 评论能力通过验证。", false),
			flag("post", "自动发布朋友圈", "默认仅生成草稿。", false),
			flag("accept", "自动审核好友申请", "只处理可解析的真实申请事件。", false),
			flag("add", "允许发起好友申请", "只在对方明确表达意图后执行。", false),
			flag("remark", "自动修改备注", "必须先提取明确事实，不猜测身份。", false),
			flag("welcome", "群内欢迎语", "仅处理可确定的入群事件。", false),
			flag("moderation", "广告检测提醒", "只产生告警，不自动执行未接通的踢人。", false),
			flag("revoke", "允许自动纠正撤回", "必须有当前 Run 的完整发送回执。", false),
			flag("approval", "写操作需要人工确认", "确认绑定具体目标和参数。", true),
			entry("targets", "动作白名单", "使用逗号分隔的唯一 ID。", ""),
			number("daily", "每日动作上限", "0 表示不执行自动动作。", 15, 0, 100),
			number("cooldown", "动作间隔 (秒)", "节奏控制不保证规避平台限制。", 45, 1, 3600),
		}},
		{"schedule", "任务计划", "计划统一使用所选时区，停机恢复不补发未知写操作。", "actions", true, "下次任务", []Field{
			flag("enabled", "启用计划任务", "需要先配置任务目标与动作权限。", false),
			entry("times", "每日执行时间", "逗号分隔，例如 12:30,18:00。", "18:00"),
			choice("timezone", "业务时区", "统计和计划任务使用此时区。", "Asia/Shanghai", "Asia/Shanghai", "UTC", "Asia/Tokyo", "America/New_York"),
			number("batch", "每批任务上限", "到达上限后等待下个执行周期。", 10, 1, 50),
		}},
		{"brand", "基础与品牌", "控制台的名称与身份信息。", "system", true, "立即生效", []Field{
			entry("name", "控制台名称", "显示在浏览器标题、导航栏、登录页和初始化页。", "we-clay"),
			entry("subtitle", "工作空间名称", "用于区分不同部署实例。", "个人工作空间"),
			entry("footer", "页脚说明", "仅支持纯文本。", "we-clay · 让沟通自然发生"),
			entry("owner", "Owner 微信 ID", "保存唯一 ID，不使用昵称作为鉴权依据。", ""),
		}},
		{"attachments", "附件与传输", "控制微信聊天中普通文件附件的大小。图片、语音和模型输入仍按各自限制处理。", "system", true, "立即生效", []Field{
			number("max_file_mb", "单个文件大小上限 (MB)", "默认 500 MB，可设置 1–1024 MB；按 1 MB = 1024 × 1024 字节计算。当前 core 发送链路最多 32 MB，超出会在发送前明确拒绝；完整大文件发送需另行升级 core。", 500, 1, 1024),
		}},
		{"storage", "数据与保留", "清理只影响控制台数据，不删除微信账号会话。", "system", true, "下次清理", []Field{
			number("messages", "聊天记录保留 (天)", "归档从本系统接入时开始。", 90, 1, 3650),
			number("media", "媒体保留 (天)", "清理后历史消息仍保留附件说明。", 30, 1, 365),
			number("logs", "日志保留 (天)", "到期删除脱敏运行日志。", 14, 1, 365),
			number("reports", "报告保留 (天)", "到期删除分析报告正文。", 180, 1, 3650),
			number("quota", "媒体容量上限 (MB)", "达到限额后停止新增缓存，不覆盖旧文件。", 1024, 10, 102400),
		}},
		{"logging", "调试日志", "在实时日志中查看实际模型传输和系统操作；原文仅管理员可见。", "system", true, "立即生效", []Field{
			choice("level", "日志级别", "DEBUG 记录请求、响应原文和操作结果；INFO 关闭原文记录。密钥、票据和媒体二进制不记录。", "INFO", "INFO", "DEBUG"),
		}},
		{"security", "登录与安全", "会话权限由服务器执行，修改角色立即生效。", "system", true, "下次登录", []Field{
			number("session_hours", "登录有效期 (小时)", "到期后需要重新登录。", 12, 1, 168),
			number("login_attempts", "登录失败上限", "每个来源在 15 分钟窗口内的最大失败次数。", 10, 3, 30),
			flag("redact", "默认脱敏日志", "密钥始终脱敏，不能通过此项关闭。", true),
		}},
		{"notifications", "告警与提醒", "告警在控制台中展示，不自动向微信联系人发送通知。", "system", true, "立即生效", []Field{
			flag("offline", "微信离线提醒", "状态改变时提醒。", true),
			flag("model", "模型异常提醒", "持续失败时展示待处理事项。", true),
			flag("budget", "预算不足提醒", "用量接近配置上限时提醒。", true),
			flag("storage", "存储容量提醒", "接近容量上限时提醒。", true),
		}},
	}
	groups = append(groups, safetyGroups()...)
	groups = append(groups, businessGroups()...)
	// 尚未接通执行器的字段也提供完整表单，但明确标注仅预览；真实模式拒绝修改它们。
	live := strings.Fields("attachments.max_file_mb reply.skip_other_mentions reply.list_mode reply.blacklist reply.default_friend reply.default_group vision.auto audio.auto reply.max_parts reply.interval_ms reply.voice_enabled reply.image_enabled reply.emoji_enabled tts.reply video_generation.size video_generation.seconds video_generation.style routing.video_generation reply.probability tts.design image.quality image.aspect image.resolution reply.enabled reply.targets reply.private reply.keywords reply.cooldown reply.quiet memory.enabled memory.direct_enabled memory.scan_enabled memory.timezone memory.messages memory.tokens memory.retention routing.chat routing.vision routing.audio routing.video routing.tts routing.image routing.timeout routing.concurrency routing.daily_calls routing.max_output insight.max_output routing.fallback vision.max_mb tts.voice tts.speed tts.max_chars image.size image.style brand.name brand.subtitle brand.footer logging.level security.session_hours security.login_attempts")
	for i := range groups {
		for j := range groups[i].Fields {
			groups[i].Fields[j].Preview = groups[i].Area != "plugins" && !slices.Contains(live, groups[i].ID+"."+groups[i].Fields[j].Key)
		}
	}
	return groups

}
func defaults() map[string]any {
	result := map[string]any{}
	for _, g := range Schema() {
		for _, f := range g.Fields {
			result[g.ID+"."+f.Key] = f.Default
		}
	}
	return result
}

// validateSettings 拒绝未知字段，确保 TOML/HTTP 等入口不能绕过值域限制。
func validateSettings(group string, values map[string]any) error {
	for _, g := range Schema() {
		if g.ID != group {
			continue
		}
		for key, value := range values {
			if group == "attachments" && key == "max_file_mb" {
				if n, ok := value.(float64); !ok || n != math.Trunc(n) {
					return fmt.Errorf("文件大小上限必须是整数 MB")
				}
			}
			if group == "memory" && key == "timezone" {
				zone, ok := value.(string)
				if !ok || zone == "" {
					return fmt.Errorf("请输入有效IANA时区")
				}
				if _, err := time.LoadLocation(zone); err != nil {
					return fmt.Errorf("IANA时区无效")
				}
			}
			found := false
			for _, f := range g.Fields {
				if key != f.Key {
					continue
				}
				if err := validateBusinessSetting(group, key, value); err != nil {
					return err
				}
				if err := validateSafetySetting(group, key, value); err != nil {
					return err
				}
				found = true
				switch f.Type {
				case "boolean":
					if _, ok := value.(bool); !ok {
						return fmt.Errorf("%s 必须是开关值", f.Label)
					}
				case "number":
					n, ok := value.(float64)
					if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < f.Min || n > f.Max {
						return fmt.Errorf("%s 应在 %g 到 %g 之间", f.Label, f.Min, f.Max)
					}
				default:
					s, ok := value.(string)
					if !ok || len(s) > 8000 {
						return fmt.Errorf("%s 内容无效或过长", f.Label)
					}
					if f.Type == "select" && !slices.Contains(f.Options, s) {
						return fmt.Errorf("%s 不在可选范围内", f.Label)
					}
				}
			}
			if !found {
				return fmt.Errorf("未知设置项：%s", key)
			}
		}
		return nil
	}
	return fmt.Errorf("未知配置组：%s", strings.TrimSpace(group))
}
