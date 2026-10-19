// 文件名：business.go
// 主要功能：执行日常工具插件，复用归档、AI、防护和发送流程，严格区分预览与微信副作用。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/plugins/contextcleaner"
	"github.com/we-clay/we-clay/plugins/toolbox"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

type businessRequest struct {
	jobID       string // 调度器内部关联，浏览器不能伪造。
	jobRevision int
	Index       int    `json:"index"`
	Plugin      string `json:"plugin"`
	Target      string `json:"target"`
	Query       string `json:"query"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Days        int    `json:"days"`
	Member      string `json:"member"`
	Mode        string `json:"mode"`
	Media       string `json:"media"`
	Asset       string `json:"asset"`
	Send        bool   `json:"send"`
}
type businessResult struct {
	Preview string          `json:"preview,omitempty"`
	Text    string          `json:"text"`
	Items   []toolbox.Item  `json:"items,omitempty"`
	Words   []toolbox.Count `json:"words,omitempty"`
	Ranking []toolbox.Count `json:"ranking,omitempty"`
	Types   []toolbox.Count `json:"types,omitempty"`
	Count   int             `json:"count,omitempty"`
	Media   string          `json:"media,omitempty"`
	MIME    string          `json:"mime,omitempty"`
	Task    string          `json:"task,omitempty"`
	Mock    bool            `json:"mock"`
	data    []byte
}

func businessText(st State, id, key string) string {
	v, _ := st.Settings[id+"."+key].(string)
	return v
}
func businessNumber(st State, id, key string) int {
	v, _ := st.Settings[id+"."+key].(float64)
	return int(v)
}
func businessEnabled(st State, id string) bool {
	return slices.ContainsFunc(st.Plugins, func(p Plugin) bool { return p.ID == id && p.Enabled })
}
func businessAllowed(st State, id, target string) bool {
	if !businessEnabled(st, id) || target == "" {
		return false
	}
	found := false
	for _, v := range strings.Split(businessText(st, id, "targets"), ",") {
		if strings.TrimSpace(v) == target {
			found = true
		}
	}
	if businessText(st, id, "scope") == "blacklist" {
		return !found
	}
	return found
}
func (s *Server) businessMessages(st State, q businessRequest) ([]Message, error) {
	if !slices.ContainsFunc(st.Contacts, func(c Contact) bool { return c.ID == q.Target }) {
		return nil, fmt.Errorf("请选择有效会话")
	}
	days := q.Days
	if days == 0 {
		days = businessNumber(st, q.Plugin, "days")
	}
	if days < 1 || days > 365 {
		return nil, fmt.Errorf("范围应为 1 到 365 天")
	}
	end := time.Now()
	start := end.AddDate(0, 0, -days)
	if q.Start != "" || q.End != "" {
		var e error
		start, e = time.ParseInLocation("2006-01-02", q.Start, time.Local)
		if e != nil {
			return nil, fmt.Errorf("开始日期无效")
		}
		end, e = time.ParseInLocation("2006-01-02", q.End, time.Local)
		if e != nil {
			return nil, fmt.Errorf("结束日期无效")
		}
		end = end.AddDate(0, 0, 1)
		if !start.Before(end) || end.Sub(start) > 366*24*time.Hour {
			return nil, fmt.Errorf("日期范围最多 366 天")
		}
	}
	out := []Message{}
	for _, m := range st.Messages {
		t, e := time.Parse(time.RFC3339, m.Time)
		if e != nil || m.Target != q.Target || t.Before(start) || !t.Before(end) || m.Kind == "system" || m.Source == "guard" || m.Status == "unknown" || m.Status == "sending" {
			continue
		}
		if q.Member != "" && m.SenderID != q.Member && m.Sender != q.Member {
			continue
		}
		out = append(out, m)
	}
	slices.SortStableFunc(out, func(a, b Message) int { return strings.Compare(a.Time, b.Time) })
	return out, nil
}
func (s *Server) buildBusiness(ctx context.Context, q businessRequest) (businessResult, error) {
	st := s.store.snapshot()
	r := businessResult{Mock: s.cfg.Mock}
	if !isBusinessPlugin(q.Plugin) || len(q.Query) > 8000 || len(q.Member) > 300 {
		return r, fmt.Errorf("插件或输入无效")
	}
	switch q.Plugin {
	case "statistics", "wordcloud", "digest":
		messages, e := s.businessMessages(st, q)
		if e != nil {
			return r, e
		}
		r.Count = len(messages)
		if len(messages) == 0 {
			return r, fmt.Errorf("所选范围没有已归档消息")
		}
		if q.Plugin == "statistics" {
			counts, kinds, names := map[string]int{}, map[string]int{}, map[string]string{}
			for _, m := range messages {
				id := m.SenderID
				if id == "" {
					id = m.Sender
				}
				counts[id]++
				names[id] = m.Sender
				kinds[m.Kind]++
			}
			r.Ranking = toolbox.Ranking(counts, businessNumber(st, q.Plugin, "limit"))
			r.Types = toolbox.Ranking(kinds, 20)
			r.Text = fmt.Sprintf("消息统计 · %d 条消息 · %d 位成员", len(messages), len(counts))
			for i := range r.Ranking {
				v := &r.Ranking[i]
				v.Name = names[v.Name]
				r.Text += fmt.Sprintf("\n%d. %s：%d 条", i+1, v.Name, v.Count)
			}
			typeNames := map[string]string{"text": "文字", "image": "图片", "audio": "语音", "video": "视频", "file": "文件", "emoji": "表情"}
			r.Text += "\n\n消息类型"
			for _, v := range r.Types {
				name := typeNames[v.Name]
				if name == "" {
					name = v.Name
				}
				r.Text += fmt.Sprintf("\n%s：%d 条", name, v.Count)
			}
			return r, nil
		}
		if q.Plugin == "wordcloud" {
			texts := []string{}
			for _, m := range messages {
				if m.Kind == "text" {
					texts = append(texts, m.Content)
				}
			}
			r.Words, e = toolbox.Words(texts, businessText(st, q.Plugin, "stop"), businessText(st, q.Plugin, "dictionary"), businessNumber(st, q.Plugin, "limit"))
			if e != nil {
				return r, e
			}
			r.Text = fmt.Sprintf("会话词云 · %d 条消息", len(messages))
			r.data, e = toolbox.RenderCloud(r.Words, businessText(st, q.Plugin, "font"))
			r.MIME = "image/png"
			return r, e
		}
		max := businessNumber(st, "digest", "max_messages")
		if max > 0 && len(messages) > max {
			messages = messages[len(messages)-max:]
		}
		docs := []contextcleaner.Message{}
		for _, m := range messages {
			docs = append(docs, contextcleaner.Message{ID: m.ID, Session: m.Target, Role: "user", Sender: m.Sender, Source: "archive", Kind: m.Kind, Text: m.Content, Time: m.Time})
		}
		prompt := businessText(st, "digest", "prompt")
		if q.Query != "" {
			prompt += "\n补充要求：" + q.Query
		}
		task, e := s.Submit(ai.Request{Kind: "insight", Prompt: prompt, Documents: docs})
		if e != nil {
			return r, e
		}
		r.Task = task
		// 摘要必须等到现有任务执行器完成后才能发送，失败不能把任务 ID 当成结果。
		tick := time.NewTicker(150 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				s.taskMu.Lock()
				if cancel := s.taskCancel[task]; cancel != nil {
					cancel()
				}
				s.taskMu.Unlock()
				return r, ctx.Err()
			case <-tick.C:
				t, e := s.Task(task)
				if e != nil {
					return r, e
				}
				switch t.Status {
				case "completed":
					r.Text = t.Result
					return r, nil
				case "failed", "cancelled":
					return r, fmt.Errorf("摘要任务未完成，请在 AI 任务记录中查看原因")
				}
			}
		}
	case "what_to_eat":
		var e error
		r.Text, e = toolbox.Meal(businessText(st, q.Plugin, "menu"), businessText(st, q.Plugin, "exclude")+","+q.Query)
		return r, e
	case "moyu_daily":
		var e error
		r.Text, e = toolbox.Daily(time.Now(), businessText(st, q.Plugin, "holidays"))
		if e == nil {
			r.data, e = toolbox.RenderCard("摸鱼日报", r.Text, businessText(st, q.Plugin, "font"), nil)
			r.MIME = "image/png"
		}
		return r, e
	case "welcome":
		name := strings.TrimSpace(q.Query)
		if name == "" {
			name = "新朋友"
		}
		group := "本群"
		for _, c := range st.Contacts {
			if c.ID == q.Target {
				group = c.Name
			}
		}
		r.Text = strings.NewReplacer("{name}", name, "{group}", group).Replace(businessText(st, q.Plugin, "message"))
		return r, nil
	case "auto_accept":
		r.Text = "好友申请规则预览："
		if reason := friendMatchReason(st, FriendRequest{Applicant: q.Target, Comment: q.Query, Scene: q.Index}); reason != "" {
			r.Text += reason
		} else {
			r.Text += "名单、附言与来源场景均匹配；实际处理仍要求合法的系统申请票据"
		}
		return r, nil
	case "cron":
		return r, fmt.Errorf("请在计划任务页创建或执行任务")
	case "meme":
		if q.Mode == "asset" && q.Asset == "" {
			return r, fmt.Errorf("请选择素材")
		}
		if q.Asset != "" {
			asset, e := s.readBusinessAsset(st.Account.ID, q.Asset)
			if e != nil {
				return r, e
			}
			r.Text = asset.Name
			r.Media = asset.Media
			r.MIME = "image/png"
			r.data, e = os.ReadFile(s.businessMediaPath(asset.Media))
			return r, e
		}
		if q.Mode == "create" || strings.HasPrefix(q.Query, "制作 ") {
			text := strings.TrimSpace(strings.TrimPrefix(q.Query, "制作 "))
			if text == "" {
				return r, fmt.Errorf("请输入表情文字")
			}
			var bg []byte
			if q.Media != "" {
				var e error
				bg, e = base64.StdEncoding.DecodeString(q.Media)
				if e != nil || len(bg) > 8<<20 {
					return r, fmt.Errorf("背景图无效或超过 8 MB")
				}
			}
			var e error
			r.Text = text
			r.data, e = toolbox.RenderCard("", text, businessText(st, q.Plugin, "font"), bg)
			r.MIME = "image/png"
			return r, e
		}
	}
	// Mock 在网络分支之前返回内置媒体，绝不访问配置的地址或读取外部凭据。
	if s.cfg.Mock {
		if st.Scenario == "error" {
			return r, fmt.Errorf("模拟外部服务暂时不可用")
		}
		title := q.Query
		if title == "" {
			title = "今日精选"
		}
		r.Items = []toolbox.Item{{Title: title + " · 脱机示例", URL: "https://example.com", Artist: "演示内容"}}
		r.Text = title + " · 脱机示例\nhttps://example.com"
		if q.Plugin == "video_parser" {
			r.Media = "/sample-video.mp4"
			r.MIME = "video/mp4"
		}
		if q.Plugin == "music" {
			r.Media = "/sample-audio.wav"
			r.MIME = "audio/wav"
		}
		if q.Plugin == "meme" {
			r.data, _ = toolbox.RenderCard("表情示例", title, businessText(st, q.Plugin, "font"), nil)
			r.MIME = "image/png"
		}
		return r, nil
	}
	client := toolbox.HTTPClient()
	defer client.CloseIdleConnections()
	endpoint := businessText(st, q.Plugin, "endpoint")
	key := ""
	if env := businessText(st, q.Plugin, "key_env"); env != "" {
		key = os.Getenv(env)
		if key == "" {
			return r, fmt.Errorf("配置的凭据环境变量未设置")
		}
	}
	limit := businessNumber(st, q.Plugin, "limit")
	if limit < 1 {
		limit = 5
	}
	var e error
	if q.Plugin == "news" {
		if endpoint == "" {
			return r, fmt.Errorf("尚未配置 RSS / Atom 新闻源")
		}
		raw, _, err := toolbox.ReadURL(ctx, client, endpoint, key, 2<<20)
		if err != nil {
			return r, err
		}
		r.Items, e = toolbox.Feed(raw, q.Query, limit)
	} else {
		if q.Plugin == "video_parser" && toolbox.PublicURL(q.Query) != nil {
			return r, fmt.Errorf("请输入有效的视频分享链接")
		}
		r.Items, e = toolbox.Search(ctx, client, endpoint, key, q.Query, limit)
	}
	if e != nil {
		return r, e
	}
	if len(r.Items) == 0 {
		return r, fmt.Errorf("没有找到结果，请更换关键词或数据源")
	}
	for _, item := range r.Items {
		r.Text += item.Title
		if item.Artist != "" {
			r.Text += " · " + item.Artist
		}
		r.Text += "\n" + item.URL + "\n"
	}
	if q.Index < 0 || q.Index >= len(r.Items) {
		return r, fmt.Errorf("结果序号无效")
	}
	if q.Mode == "media" && r.Items[q.Index].MediaURL != "" {
		r.data, _, e = toolbox.ReadURL(ctx, client, r.Items[q.Index].MediaURL, "", 16<<20)
		if e != nil {
			return r, e
		}
		r.MIME = http.DetectContentType(r.data)
		if !strings.HasPrefix(r.MIME, "image/") && !strings.HasPrefix(r.MIME, "audio/") && !strings.HasPrefix(r.MIME, "video/") {
			return r, fmt.Errorf("服务未返回可识别的图片、音频或视频")
		}
	}
	return r, nil
}
func (s *Server) deliverBusiness(ctx context.Context, account string, q businessRequest, r businessResult) error {
	st := s.store.snapshot()
	if st.Account.ID != account || !businessAllowed(st, q.Plugin, q.Target) {
		return fmt.Errorf("账号或生效范围已变化，本次不发送")
	}
	if q.jobID != "" {
		if !businessAllowed(st, "cron", q.Target) {
			return fmt.Errorf("定时任务范围已变化，本次不发送")
		}
		jobs, e := s.businessJobs(account)
		if e != nil {
			return e
		}
		valid := false
		for _, j := range jobs {
			if j.ID == q.jobID && j.Revision == q.jobRevision {
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("任务已修改或删除，本次不发送")
		}
	}
	if q.Plugin == "auto_accept" {
		return fmt.Errorf("好友验证只能由真实系统申请事件触发")
	}
	a := actionRequest{Target: q.Target, Content: r.Text}
	if len(r.data) > 0 {
		a.Media = base64.StdEncoding.EncodeToString(r.data)
		a.MIME = r.MIME
		a.Group = "file"
		a.Content = "插件结果.bin"
		if strings.HasPrefix(r.MIME, "image/") {
			a.Group = "image"
			a.Content = "插件图片.png"
		} else if strings.HasPrefix(r.MIME, "video/") {
			a.Content = "解析视频.mp4"
		} else if strings.HasPrefix(r.MIME, "audio/") {
			a.Content = "音频文件"
		}
	}
	return s.send(ctx, a, "plugin:"+q.Plugin)
}

// 命令中的纯数字作为范围天数，其余文本保留为查询词。
func businessCommand(id, target, arg string) businessRequest {
	q := businessRequest{Plugin: id, Target: target, Query: arg}
	if id == "statistics" || id == "wordcloud" || id == "digest" {
		if n, e := strconv.Atoi(arg); e == nil {
			q.Days = n
			q.Query = ""
		}
	}
	return q
}
