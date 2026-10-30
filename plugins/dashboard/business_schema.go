// 文件名：business_schema.go
// 主要功能：定义业务插件的默认注册、实际生效配置、触发说明与输入校验。
// 作者：2guan
package dashboard

import (
	"encoding/json"
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"github.com/we-clay/we-clay/plugins/playroom"
	"github.com/we-clay/we-clay/plugins/toolbox"
	"math"
	"regexp"
	"strings"
	"time"
)

func isBusinessPlugin(id string) bool { _, ok := toolbox.Find(id); return ok }
func businessPlugins(mock bool) []Plugin {
	out := []Plugin{}
	for _, d := range toolbox.Catalog {
		enabled := mock
		out = append(out, Plugin{d.ID, d.Name, d.Description, "1.0.0", enabled, true, map[bool]string{true: "ready", false: "stopped"}[enabled], []string{}})
	}
	return out
}
func businessGroups() []Group {
	out := []Group{}
	for _, d := range toolbox.Catalog {
		fields := []Field{
			choice("scope", "名单模式", "默认黑名单；启用插件后，空黑名单全局生效。白名单仅允许名单内对象，留空不生效。", "blacklist", "blacklist", "whitelist"),
			entry("targets", "黑白名单", "逗号分隔的会话 ID。黑名单模式填写排除对象，留空全局生效；白名单模式填写允许对象。好友验证使用申请人 ID。", ""),
			number("cooldown", "冷却时间（秒）", "限制同一插件、同一目标的自动执行频率。", 30, 0, 86400),
		}
		switch d.ID {
		case "turtlesoup":
			fields = fields[:2]
			fields = append(fields, flag("group_start_mention", "群聊开局必须 @ 本人", "群聊须通过微信真实 @ 当前账号后发送来碗海龟汤，私聊和开局后的提问不受影响。", true))
			fields = append(fields, choice("group_mode", "群聊提问方式", "自然模式识别每条文字，非游戏消息交回普通对话；定向模式仅处理 @、引用游戏消息或汤问前缀。", "natural", "natural", "direct"), entry("model", "指定模型 ID", "留空沿用模型中心的对话流水线及故障切换配置。", ""), entry("self_name", "主持自称", "留空使用当前微信昵称；不会猜测身份、工作或状态。", ""), number("idle_minutes", "空闲暂停（分钟）", "到期暂停，发送继续恢复；不会自动揭底。", 30, 1, 1440), number("max_hints", "每局提示上限", "按题目预设提示依次公开。", 3, 1, 5))
		case "cron":
			fields = append(fields, entry("timezone", "默认时区", "新建任务的缺省时区；每项任务可独立设置。", "Asia/Shanghai"))
		case "statistics":
			fields = append(fields, number("days", "默认统计天数", "按本地归档统计，不代表微信全量历史。", 7, 1, 365), number("limit", "排行人数", "按稳定成员 ID 排名；旧记录没有 ID 时按显示名。", 20, 1, 100))
		case "wordcloud":
			fields = append(fields, number("days", "默认统计天数", "只读取当前会话文字。", 7, 1, 365), number("limit", "词语数量", "按词频取前若干项。", 60, 10, 150), entry("stop", "停用词", "用逗号或换行分隔。", "这个,那个,我们,你们,他们,已经,收到,好的"), entry("dictionary", "自定义词典", "专业名词用逗号或换行分隔，提高中文分词效果。", ""), entry("font", "中文字体路径", "留空自动寻找系统中文字体；Linux 可安装 Noto CJK。", ""))
		case "welcome":
			fields = append(fields, entry("message", "消息内容", "支持 {name} 和 {group}；只处理真实系统入群通知。", "欢迎加入，一起轻松交流。"), number("delay_seconds", "发送延迟（秒）", "等待后重新检查开关、名单与账号。", 2, 0, 60))
		case "auto_accept":
			fields = append(fields, entry("keyword", "附言关键词", "非空时要求好友申请附言包含此文字。", ""), entry("scenes", "允许来源场景", "逗号分隔数字，留空允许所有合法场景。", ""), number("delay_seconds", "处理延迟（秒）", "申请到达后等待此时长；凭据加密保存在本地，不返回网页或写入日志。", 10, 0, 86400))
		case "digest":
			fields = append(fields, number("days", "默认范围（天）", "摘要读取指定会话的原始归档。", 1, 1, 365), number("max_messages", "消息数量", "超过上限取最近记录；模型容量不足会明确报错。", 200, 1, 20000), entry("prompt", "摘要要求", "复用默认对话流水线和提示词防护。", "请总结讨论重点、已作决定和待办，保留信息来源，不编造事实。"))
		case "news":
			fields = append(fields, choice("default_mode", "默认新闻类型", "新闻口令及未指定类型的任务使用此项；今日新闻、今日图卦口令始终取对应图片。", "today", "today", "diagram", "rss"), entry("today_template", "今日新闻图片地址", "沿用 Golem 原数据源，{date} 为北京时间 YYYY-MM-DD。", toolbox.DefaultNewsTemplate), entry("diagram_template", "图卦图片地址", "沿用 Golem 原数据源，读取昨日两张；{month}=YYYYMM，{date}=YYYYMMDD，{index}=1 或 2。", toolbox.DefaultDiagramTemplate), entry("endpoint", "RSS / Atom 地址", "仅 RSS 模式使用；只支持公网 HTTP(S)，不填写凭据或带密钥的查询参数。", ""), entry("key_env", "凭据环境变量名", "可选，从服务端环境读取 Bearer Key，配置中不保存密钥。", ""), number("limit", "结果数量", "一次最多展示与发送的新闻数量。", 8, 1, 20))
		case "music":
			fields = append(fields, choice("source", "音乐音源", "自动模式优先酷我，失败后尝试抖音。内置酷我地址："+toolbox.KuwoMusicEndpoint+"；内置抖音地址："+toolbox.DouyinMusicEndpoint+"。两条地址由程序内置；QQ 接口仅在自定义服务模式下显示并生效。", "auto", "auto", "kuwo", "douyin", "custom"), entry("endpoint", "自定义服务地址", "仅自定义模式使用；保留 Golem Music 的原 QQ 音乐地址。", toolbox.DefaultMusicEndpoint), choice("api_format", "自定义接口格式", "仅自定义模式使用。Golem 接口使用 msg/code/data；兼容服务使用 q/url/limit/items。", "golem", "golem", "items"), entry("key_env", "凭据环境变量名", "仅自定义模式使用的可选 Bearer Key 环境变量名，不填写密钥本身。", ""), number("limit", "搜索结果数量", "抖音最多检查的候选数；内置音源返回首个可播放结果，自定义服务可选择其它结果。", 5, 1, 20), choice("send_mode", "默认分享形式", "音乐卡片需要播放地址；也可选择文字链接。", "card", "card", "link"), entry("app_id", "卡片来源 AppID", "random 沿用 Golem 的随机来源；可填固定 wx AppID，留空不标来源。", "random"))
		case "video_parser", "meme":
			fields = append(fields, entry("endpoint", "服务地址", "GET 请求附带 q、url、limit；返回 items 数组，完整契约见运行说明。", ""), entry("key_env", "凭据环境变量名", "可选的 Bearer Key 环境变量名；不填写密钥本身。", ""), number("limit", "搜索结果数量", "搜索后可单独预览或发送某一项。", 5, 1, 20))
			if d.ID == "meme" {
				fields = append(fields, entry("font", "中文字体路径", "生成文字表情使用本机字体。", ""))
			}
		case "what_to_eat":
			fields = append(fields, entry("menu", "自定义菜单", "每行一道菜或一个餐厅。", "番茄鸡蛋面\n牛肉饭\n菌菇汤\n清蒸鱼\n饺子\n蔬菜沙拉"), entry("exclude", "排除项", "逗号分隔；匹配菜名中的文字。", ""))
		case "moyu_daily":
			fields = append(fields, entry("holidays", "节假日 JSON", "日期到名称的映射；自行维护，不假定法定调休。", "{}"), entry("font", "中文字体路径", "生成日报图片使用本机字体。", ""))
		}
		if _, ok := playroom.Find(d.ID); ok {
			fields = fields[:2]
			fields = append(fields, entry("model", "指定模型 ID", "留空沿用模型中心流水线和故障切换；模型失败自动再试一轮。", ""))
			if d.ID == "romance" {
				fields = append(fields, choice("tone", "剧情尺度", "仅虚构互动；成人暧昧仅供成年用户主动选择，不包含露骨内容。", "清水", "清水", "暧昧", "成人暧昧"))
			}
			if d.ID == "group_digest" {
				fields = append(fields, number("max_messages", "报告消息上限", "超过上限明确失败，不静默删减；范围按北京时间。", 1000, 10, 20000))
			}
		}
		if d.Trigger != "" && d.ID != "turtlesoup" && d.ID != "romance" && d.ID != "radio" {
			fields = append(fields, flag("group_mention_required", "群聊触发必须 @ 本人", "默认开启，须在微信真实 @ 当前账号；手写昵称、@ 他人或 @所有人不能替代。私聊、工作台和定时任务不受影响；互动游戏仅限制开局或重新触发，局内正常指令不受影响。", true))
		}
		// 长文本使用多行编辑器，避免菜单、词典和 JSON 中的换行被 input 吞掉。
		for i := range fields {
			if fields[i].Key == "menu" || fields[i].Key == "stop" || fields[i].Key == "dictionary" || fields[i].Key == "holidays" || fields[i].Key == "prompt" || fields[i].Key == "message" {
				fields[i].Type = "textarea"
			}
		}
		out = append(out, Group{d.ID, d.Name, d.Description, "plugins", true, "保存后生效", fields})
	}
	return out
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

func validateBusinessSetting(group, key string, value any) error {
	if !isBusinessPlugin(group) {
		return nil
	}
	text, _ := value.(string)
	switch key {
	case "delay_seconds":
		if group == "auto_accept" {
			n, ok := value.(float64)
			if !ok || n != math.Trunc(n) {
				return fmt.Errorf("自动通过延迟必须是整数秒")
			}
		}
	case "app_id":
		if group == "music" {
			_, e := toolbox.MusicAppID(text)
			return e
		}
	case "endpoint", "today_template", "diagram_template":
		if text != "" {
			return toolbox.PublicURL(text)
		}
	case "key_env":
		if text != "" && !envName.MatchString(text) {
			return fmt.Errorf("请输入环境变量名，不要填写 API Key")
		}
	case "timezone":
		if _, e := time.LoadLocation(text); e != nil {
			return fmt.Errorf("时区无效")
		}
	case "holidays":
		_, e := toolbox.Daily(time.Now(), text)
		return e
	case "scenes":
		if text != "" {
			for _, v := range strings.Split(text, ",") {
				var n int
				if json.Unmarshal([]byte(strings.TrimSpace(v)), &n) != nil || n < 0 || n > 1000 {
					return fmt.Errorf("场景应为 0 到 1000 的整数")
				}
			}
		}
	case "menu":
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("菜单不能为空")
		}
	}
	return nil
}

// 旧版仅演示的欢迎/摘要配置升级到实际插件时，保留用户已经编辑的可兼容字段。
func migrateBusinessSettings(s *store, mock bool) {
	if !mock {
		return
	}
	for _, id := range []string{"welcome", "digest"} {
		var raw []byte
		if s.db.QueryRow("SELECT body FROM documents WHERE id=?", "plugin-config:"+id).Scan(&raw) != nil {
			continue
		}
		var old map[string]any
		if toml.Unmarshal(raw, &old) != nil {
			continue
		}
		values, e := jsonValues(old["config"])
		if e != nil {
			continue
		}
		allowed := map[string]bool{"message": true, "delay_seconds": true, "max_messages": true}
		for key, value := range values {
			if !allowed[key] {
				continue
			}
			full := id + "." + key
			if _, exists := s.state.Settings[full]; exists {
				continue
			}
			if validateSettings(id, map[string]any{key: value}) == nil {
				s.state.Settings[full] = value
			}
		}
	}
}

// 一次性升级旧的未配置空白名单；保留手动保存的配置及非空名单，重启不再次改变模式。
func migratePluginScopeDefaults(st *State) {
	if st.PluginScopeDefaultsVersion >= 1 {
		return
	}
	changed := false
	for _, d := range toolbox.Catalog {
		key := d.ID + ".scope"
		if settingText(*st, key) != "whitelist" || strings.TrimSpace(settingText(*st, d.ID+".targets")) != "" {
			continue
		}
		manual := false
		for _, h := range st.History {
			if h.Group == d.ID {
				manual = true
				break
			}
		}
		if !manual {
			st.Settings[key] = "blacklist"
			changed = true
		}
	}
	st.PluginScopeDefaultsVersion = 1
	if changed {
		st.ConfigRevision++
	}
}
