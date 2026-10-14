// 文件名：business_schema.go
// 主要功能：定义业务插件的默认注册、实际生效配置、触发说明与输入校验。
// 作者：2guan
package dashboard

import (
	"encoding/json"
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"github.com/we-clay/we-clay/plugins/toolbox"
	"github.com/we-clay/we-clay/sdk/control"
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
			choice("scope", "名单模式", "自动触发及发送均受此名单约束；白名单为空时不执行。", "whitelist", "whitelist", "blacklist"),
			entry("targets", "生效名单", "逗号分隔的会话 ID；好友验证使用申请人 ID。", ""),
			number("cooldown", "冷却时间（秒）", "限制同一插件、同一目标的自动执行频率。", 30, 0, 86400),
		}
		switch d.ID {
		case "cron":
			fields = append(fields, entry("timezone", "默认时区", "新建任务的缺省时区；每项任务可独立设置。", "Asia/Shanghai"))
		case "statistics":
			fields = append(fields, number("days", "默认统计天数", "按本地归档统计，不代表微信全量历史。", 7, 1, 365), number("limit", "排行人数", "按稳定成员 ID 排名；旧记录没有 ID 时按显示名。", 20, 1, 100))
		case "wordcloud":
			fields = append(fields, number("days", "默认统计天数", "只读取当前会话文字。", 7, 1, 365), number("limit", "词语数量", "按词频取前若干项。", 60, 10, 150), entry("stop", "停用词", "用逗号或换行分隔。", "这个,那个,我们,你们,他们,已经,收到,好的"), entry("dictionary", "自定义词典", "专业名词用逗号或换行分隔，提高中文分词效果。", ""), entry("font", "中文字体路径", "留空自动寻找系统中文字体；Linux 可安装 Noto CJK。", ""))
		case "welcome":
			fields = append(fields, entry("message", "消息内容", "支持 {name} 和 {group}；只处理真实系统入群通知。", "欢迎加入，一起轻松交流。"), number("delay_seconds", "发送延迟（秒）", "等待后重新检查开关、名单与账号。", 2, 0, 60))
		case "auto_accept":
			fields = append(fields, entry("keyword", "附言关键词", "非空时要求好友申请附言包含此文字。", ""), entry("scenes", "允许来源场景", "逗号分隔数字，留空允许所有合法场景。", ""), number("delay_seconds", "处理延迟（秒）", "票据仅在内存中使用，不保存到执行记录。", 10, 0, 60))
		case "digest":
			fields = append(fields, number("days", "默认范围（天）", "摘要读取指定会话的原始归档。", 1, 1, 365), number("max_messages", "消息数量", "超过上限取最近记录；模型容量不足会明确报错。", 200, 1, 20000), entry("prompt", "摘要要求", "复用默认对话流水线和提示词防护。", "请总结讨论重点、已作决定和待办，保留信息来源，不编造事实。"))
		case "news":
			fields = append(fields, entry("endpoint", "RSS / Atom 地址", "只支持公网 HTTP(S)，不填写凭据或带密钥的查询参数。", ""), entry("key_env", "凭据环境变量名", "可选，从服务端环境读取 Bearer Key，配置中不保存密钥。", ""), number("limit", "结果数量", "一次最多展示与发送的新闻数量。", 8, 1, 20))
		case "music", "video_parser", "meme":
			fields = append(fields, entry("endpoint", "服务地址", "GET 请求附带 q、url、limit；返回 items 数组，完整契约见运行说明。", ""), entry("key_env", "凭据环境变量名", "可选的 Bearer Key 环境变量名；不填写密钥本身。", ""), number("limit", "搜索结果数量", "搜索后可单独预览或发送某一项。", 5, 1, 20))
			if d.ID == "meme" {
				fields = append(fields, entry("font", "中文字体路径", "生成文字表情使用本机字体。", ""))
			}
		case "what_to_eat":
			fields = append(fields, entry("menu", "自定义菜单", "每行一道菜或一个餐厅。", "番茄鸡蛋面\n牛肉饭\n菌菇汤\n清蒸鱼\n饺子\n蔬菜沙拉"), entry("exclude", "排除项", "逗号分隔；匹配菜名中的文字。", ""))
		case "moyu_daily":
			fields = append(fields, entry("holidays", "节假日 JSON", "日期到名称的映射；自行维护，不假定法定调休。", "{}"), entry("font", "中文字体路径", "生成日报图片使用本机字体。", ""))
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
	case "endpoint":
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
func businessUsage(id string) *control.PluginUsage {
	d, ok := toolbox.Find(id)
	if !ok {
		return nil
	}
	u := &control.PluginUsage{Steps: []string{"在配置中设置生效名单、冷却时间和功能参数；真实模式默认关闭业务插件，启用后只在允许的目标执行。", "工作台支持预览、单独发送和查看执行记录。脱机模式不访问外部服务或真实微信。"}}
	if d.Trigger != "" {
		u.Commands = []control.PluginCommandHelp{{Name: d.Trigger, Description: d.Description, Syntax: d.Trigger + " [参数]", Examples: []string{d.Example}}}
	}
	switch id {
	case "cron":
		u.Steps = append(u.Steps, "工作台创建单次或五段 Cron 任务，可调用文字、摘要、统计、词云、新闻等；中断或未知发送结果不自动重发。")
	case "auto_accept":
		u.Steps = append(u.Steps, "仅系统类型 37 的合法好友申请参与处理；群邀请因当前 core 未接通接受接口，暂不执行。")
	case "welcome":
		u.Steps = append(u.Steps, "识别系统消息中的中文邀请入群、二维码入群通知；普通聊天文本不能触发。可在工作台模拟验证。")
	case "news":
		u.Steps = append(u.Steps, "配置 RSS 或 Atom 源；关键词仅过滤标题，不使用未经核验的硬编码新闻源。")
	case "music", "video_parser", "meme":
		u.Steps = append(u.Steps, "外部服务返回格式：{\"items\":[{\"title\":\"标题\",\"url\":\"公开链接\",\"artist\":\"可选作者\",\"media_url\":\"可选媒体链接\"}]}。", "音乐以文字及播放链接分享；视频可预览并作为文件发送，不伪造未验证的原生音乐卡片。")
	case "moyu_daily":
		u.Steps = append(u.Steps, "周末按周六、周日计算；节假日从配置读取，不推测调休安排。")
	}
	return u
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
