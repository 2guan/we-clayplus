// 文件名：seed.go
// 主要功能：构造完全虚构的脱机演示数据；不读取真实账号、聊天或模型配置。
// 作者：2guan
package dashboard

import (
	"fmt"
	"time"
)

func initialState(mock bool) State {
	s := State{PluginScopeDefaultsVersion: 1, ConfigRevision: 1, Revision: 1, Mock: mock, Scenario: "normal", Account: Account{Name: "等待连接", Status: "offline"}, Contacts: []Contact{}, Messages: []Message{}, Moments: []Moment{}, Logs: []Record{}, Tasks: []Task{}, Models: []Model{}, Prompts: []Prompt{}, Settings: defaults(), Overrides: map[string]map[string]any{}, History: []Change{}, Trend: []Point{}, Users: []User{}}
	s.Plugins = []Plugin{
		{"dashboard", "控制台", "会话工作台、运行观测与配置管理。", "0.6.0", true, true, "running", []string{"消息", "联系人", "管理"}},
		{"ai", "AI 助手", "对话、图片与音视频理解，以及内容生成。", "0.6.0", true, true, "unconfigured", []string{"对话", "多模态", "任务"}},
	}
	s.Plugins = append(s.Plugins, safetyPlugins()...)
	s.Plugins = append(s.Plugins, businessPlugins(mock)...)
	s.Prompts = []Prompt{{"natural", "自然交流", "像一位细心的朋友，简洁、真诚，有边界。", "你是一位友好、可靠的交流伙伴。用自然简洁的中文回应。不编造事实，信息不足时明确说明。尊重会话隐私，不执行未授权的操作。"}, {"work", "工作助手", "整理讨论重点，把复杂问题讲清楚。", "用清晰的中文提供有依据的建议。区分事实与推测，必要时列出可执行的下一步。"}, {"community", "社区伙伴", "参与群聊，留意上下文，也懂得适时安静。", "适度参与群聊，优先回应明确的问题。避免重复消息，不打断正在进行的对话。"}}
	if !mock {
		return s
	}
	s.Account = Account{Name: "小禾", ID: "mock-account", Avatar: "禾", Owner: "演示管理员", Status: "online"}
	s.Plugins[1].Status = "ready"
	s.Vendors = []Vendor{{ID: "demo-vendor", Name: "脱机演示", Provider: "mock", Configured: true}}
	s.Pipeline = []string{"demo-chat"}
	s.Policies = []BehaviorPolicy{}
	s.Models = []Model{
		{ID: "demo-chat", VendorID: "demo-vendor", Name: "日常对话", Provider: "mock", Model: "离线对话模型", Capabilities: []string{"chat", "vision"}, Enabled: true, Configured: true},
		{ID: "demo-media", VendorID: "demo-vendor", Name: "多模态理解", Provider: "mock", Model: "离线音视频模型", Capabilities: []string{"audio", "video"}, Enabled: true, Configured: true},
		{ID: "demo-create", VendorID: "demo-vendor", Name: "内容创作", Provider: "mock", Model: "离线生成模型", Capabilities: []string{"tts", "image", "video_generation"}, Enabled: true, Configured: true},
	}

	names := []string{"设计日常", "林间", "周末漫游计划", "许清", "产品小组", "陈序", "山海", "阅读角", "小满", "拾光"}
	previews := []string{"把细节慢慢做好，就是最好的设计。", "这张照片的光线真好。", "周六早上九点，在老地方碰面。", "整理好了，发你看一下。", "收到，下一个版本见。", "语音消息", "给你分享一本最近在读的书。", "今天读到了很喜欢的一段。", "明天见！", "图片"}
	for i, name := range names {
		kind := "friend"
		members := 0
		if i == 0 || i == 2 || i == 4 || i == 7 {
			kind = "group"
			members = 8 + i*3
		}
		unread := 0
		if i < 3 {
			unread = 3 - i
		}
		mode := "auto"
		if i == 3 {
			mode = "manual"
		}
		s.Contacts = append(s.Contacts, Contact{ID: fmt.Sprintf("mock-contact-%d", i), Name: name, Avatar: string([]rune(name)[0]), Kind: kind, Region: "杭州", Tags: []string{"日常"}, Members: members, Unread: unread, Pinned: i < 2, Mode: mode, Preview: previews[i], Time: time.Now().Add(-time.Duration(i*37) * time.Minute).Format(time.RFC3339)})
	}
	lines := []struct {
		content, kind, sender string
		self                  bool
	}{{"周一的设计评审，我们试着把界面再做轻一点。", "text", "林间", false}, {"好呀，先把最常用的操作放在顺手的位置。", "text", "小禾", true}, {"参考了一些自然的颜色，感觉看久了也舒服。", "text", "许清", false}, {"一角山色，适合慢慢看。", "image", "林间", false}, {"喜欢这个方向。留白再多一点，层次就出来了。", "text", "小禾", true}, {"我把想法录下来了，晚点听听看。", "audio", "陈序", false}, {"收到，先把今天的讨论整理下来。", "text", "小禾", true}, {"山色片段（示例）", "video", "林间", false}, {"设计评审说明.txt", "file", "许清", false}, {"把细节慢慢做好，就是最好的设计。", "text", "林间", false}}
	for i, v := range lines {
		m := Message{ID: fmt.Sprintf("mock-message-%d", i), Target: s.Contacts[0].ID, Sender: v.sender, Content: v.content, Kind: v.kind, Self: v.self, Source: "wechat", Status: "sent", Time: time.Now().Add(time.Duration(i-12) * 4 * time.Minute).Format(time.RFC3339)}
		if v.self {
			m.Source = "ai"
		}
		if v.kind == "image" {
			m.Media = "/sample-landscape.svg"
		}
		if v.kind == "audio" {
			m.Media = "/sample-audio.wav"
			m.Duration = 5
		}
		if v.kind == "video" {
			m.Media = "/sample-video.mp4"
		}
		if v.kind == "file" {
			m.Media = "/sample-note.txt"
		}
		s.Messages = append(s.Messages, m)
	}
	for i := 1; i < len(s.Contacts); i++ {
		s.Messages = append(s.Messages, Message{ID: fmt.Sprintf("mock-other-%d", i), Target: s.Contacts[i].ID, Sender: s.Contacts[i].Name, Content: previews[i], Kind: "text", Source: "wechat", Status: "sent", Time: s.Contacts[i].Time})
	}
	s.Moments = []Moment{
		{AuthorID: "mock-contact-1", ID: "mock-moment-1", Author: "林间", Avatar: "林", Content: "周末留给山野，也留一点时间给自己。", Images: []string{"/sample-landscape.svg"}, Likes: 6, Time: time.Now().Add(-2 * time.Hour).Format(time.RFC3339), Comments: []Comment{{ID: "1", AuthorID: "mock-contact-2", Author: "许清", Content: "这样的天气，真适合出门。"}}},
		{AuthorID: "mock-contact-5", ID: "mock-moment-2", Author: "陈序", Avatar: "陈", Content: "读到一句话：生活的答案，常常藏在认真度过的日常里。", Images: []string{}, Liked: true, Likes: 3, Time: time.Now().Add(-5 * time.Hour).Format(time.RFC3339), Comments: []Comment{}},
	}
	for i := 13; i >= 0; i-- {
		d := time.Now().AddDate(0, 0, -i)
		s.Trend = append(s.Trend, Point{d.Format("2006-01-02"), 24000 + (i*7919)%60000, 8000 + (i*2741)%24000, 70 + (i*31)%120, 30 + (i*17)%80})
	}
	s.Logs = []Record{{"seed-1", time.Now().Format(time.RFC3339), "INFO", "dashboard", "脱机演示已启动，所有数据均为虚构内容。", ""}, {"seed-2", time.Now().Format(time.RFC3339), "INFO", "ai", "六类模拟能力已就绪，不会连接外部模型。", ""}, {"seed-3", time.Now().Format(time.RFC3339), "WARN", "media", "原生微信语音发送需要在真实环境验证编解码工具。", ""}}
	s.Tasks = []Task{{ID: "mock-task-1", Kind: "insight", Name: "设计日常 · 会话摘要", Status: "completed", Created: time.Now().Add(-time.Hour).Format(time.RFC3339), Result: "讨论主要围绕界面密度、颜色和操作路径展开。\n\n待办：简化高频操作入口，补充亮暗主题对比。\n\n依据：本页虚构示例消息。", Model: "离线对话模型"}}
	return s
}
