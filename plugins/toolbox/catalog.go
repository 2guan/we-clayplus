// 文件名：catalog.go
// 主要功能：集中声明第二、三阶段插件及触发词，供控制台、调度器和消息分发复用。
// 作者：2guan
package toolbox

import "strings"
import "github.com/we-clay/we-clay/plugins/playroom"

type Definition struct{ ID, Name, Description, Trigger, Example string }

var Catalog = []Definition{
	{"turtlesoup", "海龟汤", "私聊或群聊情境推理，模型优先按主题出题，失败时使用固定题库。", "来碗海龟汤", "来碗海龟汤 职场，难一点"},
	{"cron", "定时任务", "定时发送消息，或调用摘要、统计和内容插件。", "", ""},
	{"statistics", "消息统计", "按会话、成员和时间查看发言排行与消息类型。", "消息统计", "消息统计 7"},
	{"wordcloud", "词云", "从会话文字生成词云，支持停用词和自定义词典。", "词云", "词云 7"},
	{"auto_accept", "自动通过", "按名单、附言与来源场景处理好友申请。", "", ""},
	{"welcome", "入群欢迎", "识别系统入群通知，按群配置欢迎语并防止重复发送。", "", ""},
	{"digest", "每日摘要", "复用 AI 洞察整理会话，可交给定时任务自动发送。", "会话摘要", "会话摘要"},
	{"news", "新闻", "今日新闻单图、昨日图卦双图及 RSS 新闻，支持预览和定时推送。", "新闻", "今日新闻"},
	{"video_parser", "视频解析", "通过配置的解析服务获取视频信息和下载链接。", "解析视频", "解析视频 https://example.com/video"},
	{"music", "音乐", "多种句式点歌，搜索歌曲并发送含封面、歌词的音乐卡片，支持试听和链接分享。", "点歌", "点歌 晴天"},
	{"meme", "表情包", "搜索表情、自定义素材，或制作文字表情图片。", "表情", "表情 制作 今天也要开心"},
	{"what_to_eat", "今天吃什么", "从自定义菜单中随机推荐，支持排除不想吃的菜。", "吃什么", "吃什么"},
	{"moyu_daily", "摸鱼日报", "生成每日信息卡片，展示周末和自定义节假日倒计时。", "摸鱼日报", "摸鱼日报"},
}

func init() {
	for _, d := range playroom.Definitions {
		Catalog = append(Catalog, Definition{d.ID, d.Name, "互动插件：" + d.Name + "，独立存档、范围控制和失败重试。", d.Trigger, d.Trigger + " [主题]"})
	}
}

func Find(id string) (Definition, bool) {
	for _, d := range Catalog {
		if d.ID == id {
			return d, true
		}
	}
	return Definition{}, false
}

// 只匹配完整触发词边界，不把普通聊天中的片段当成命令。
func Match(text string) (string, string) {
	text = CommandText(text)
	if query := MusicQuery(text); query != "" {
		return "music", query
	}
	if arg, ok := newsCommand(text); ok {
		return "news", arg
	}
	for _, d := range Catalog {
		if d.ID == "music" {
			continue
		}
		if d.Trigger != "" && (text == d.Trigger || strings.HasPrefix(text, d.Trigger+" ")) {
			return d.ID, strings.TrimSpace(strings.TrimPrefix(text, d.Trigger))
		}
	}
	return "", ""
}

// 仅去掉可见提醒前缀以识别口令；是否真的 @ 本人仍由宿主用协议元数据核验。
func CommandText(text string) string {
	text = strings.TrimSpace(text)
	for strings.HasPrefix(text, "@") {
		i := strings.IndexRune(text, '\u2005')
		if i < 0 {
			i = strings.IndexAny(text, " \t\r\n\u00a0")
		}
		if i < 0 {
			break
		}
		text = strings.TrimSpace(text[i:])
	}
	return text
}
