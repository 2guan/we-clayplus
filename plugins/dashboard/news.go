package dashboard

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/toolbox"
)

type businessImage struct {
	Media   string `json:"media"`
	MIME    string `json:"mime"`
	Caption string `json:"caption"`
	data    []byte
}

func migrateNewsDefaults(st *State, raw []byte) {
	var stored struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &stored) != nil {
		return
	}
	if _, ok := stored.Settings["news.default_mode"]; !ok && businessText(*st, "news", "endpoint") != "" {
		st.Settings["news.default_mode"] = "rss"
		st.ConfigRevision++
	}
}

// 口令、默认类型和显式类型共用解析，保证冷却分组与实际发送一致。
func normalizeNewsRequest(st State, q businessRequest) businessRequest {
	mode := q.Mode
	if mode == "" {
		switch strings.TrimSpace(q.Query) {
		case "今日新闻":
			mode = "today"
			q.Query = ""
		case "今日图卦":
			mode = "diagram"
			q.Query = ""
		case "RSS":
			mode = "rss"
			q.Query = ""
		default:
			if strings.HasPrefix(q.Query, "RSS ") {
				mode = "rss"
				q.Query = strings.TrimSpace(strings.TrimPrefix(q.Query, "RSS "))
			} else if q.Query != "" {
				mode = "rss"
			} else {
				mode = businessText(st, "news", "default_mode")
			}
		}
	}
	if mode == "" {
		mode = "today"
	}
	q.Mode = mode
	return q
}

// 新闻、图卦和 RSS 独立冷却；复用持久化凭据表，拒绝不会延长冷却。
func (s *Server) claimNewsCooldown(account string, q businessRequest, now time.Time) (bool, error) {
	seconds := businessNumber(s.store.snapshot(), "news", "cooldown")
	if seconds <= 0 {
		return true, nil
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte("cooldown:news:"+q.Target+":"+q.Mode)))
	res, e := s.store.db.Exec(`INSERT INTO business_claims(account,id,time) VALUES(?,?,?)
		ON CONFLICT(account,id) DO UPDATE SET time=excluded.time WHERE business_claims.time<=?`,
		account, key, now.Format(time.RFC3339), now.Add(-time.Duration(seconds)*time.Second).Format(time.RFC3339))
	if e != nil {
		return false, e
	}
	n, e := res.RowsAffected()
	return n == 1, e
}

func (s *Server) buildNews(ctx context.Context, q businessRequest) (businessResult, error) {
	return s.buildNewsFor(ctx, q, s.store.snapshot())
}

func (s *Server) buildNewsFor(ctx context.Context, q businessRequest, st State) (businessResult, error) {
	r := businessResult{Mock: s.cfg.Mock}
	q = normalizeNewsRequest(st, q)
	mode := q.Mode
	if mode != "today" && mode != "diagram" && mode != "rss" {
		return r, fmt.Errorf("新闻类型无效")
	}
	if mode != "rss" && q.Query != "" {
		return r, fmt.Errorf("图片新闻不支持关键词筛选，请选择 RSS 新闻")
	}
	if s.cfg.Mock && st.Scenario == "error" {
		return r, fmt.Errorf("模拟外部服务暂时不可用")
	}
	client := toolbox.HTTPClient()
	defer client.CloseIdleConnections()
	if mode == "rss" {
		if s.cfg.Mock {
			r.Text = "新闻 · 脱机示例"
			r.Items = []toolbox.Item{{Title: "脱机新闻示例", URL: "https://example.com"}}
			return r, nil
		}
		endpoint := businessText(st, "news", "endpoint")
		if endpoint == "" {
			return r, fmt.Errorf("尚未配置 RSS / Atom 新闻源")
		}
		key := ""
		if env := businessText(st, "news", "key_env"); env != "" {
			key = os.Getenv(env)
			if key == "" {
				return r, fmt.Errorf("配置的凭据环境变量未设置")
			}
		}
		raw, _, e := toolbox.ReadURL(ctx, client, endpoint, key, 2<<20)
		if e != nil {
			return r, e
		}
		limit := businessNumber(st, "news", "limit")
		if limit < 1 {
			limit = 8
		}
		r.Items, e = toolbox.Feed(raw, q.Query, limit)
		if e != nil {
			return r, e
		}
		if len(r.Items) == 0 {
			return r, fmt.Errorf("没有找到匹配的新闻，请更换关键词或数据源")
		}
		if q.Index < 0 || q.Index >= len(r.Items) {
			return r, fmt.Errorf("结果序号无效")
		}
		for _, item := range r.Items {
			r.Text += item.Title + "\n" + item.URL + "\n"
		}
		return r, nil
	}
	name, template := "今日新闻", businessText(st, "news", "today_template")
	if mode == "diagram" {
		name = "今日图卦"
		template = businessText(st, "news", "diagram_template")
	}
	var edition toolbox.NewsEdition
	if s.cfg.Mock {
		now := time.Now().In(time.FixedZone("北京时间", 8*3600))
		count := 1
		if mode == "diagram" {
			now = now.AddDate(0, 0, -1)
			count = 2
		}
		edition.Date = now.Format("2006-01-02")
		for i := 0; i < count; i++ {
			raw, e := toolbox.RenderCard(name, fmt.Sprintf("脱机示例 %d/%d", i+1, count), "", nil)
			if e != nil {
				return r, e
			}
			edition.Images = append(edition.Images, toolbox.NewsImage{Data: raw, MIME: "image/png"})
		}
	} else {
		var e error
		edition, e = s.newsCache.Get(ctx, client, mode, template, time.Now())
		if e != nil {
			return r, e
		}
	}
	r.Text = name + " · " + edition.Date
	if mode == "diagram" {
		r.Text += "（昨日，共 2 张）"
	}
	for i, x := range edition.Images {
		caption := name
		if len(edition.Images) > 1 {
			caption += fmt.Sprintf(" %d/%d", i+1, len(edition.Images))
		}
		r.Images = append(r.Images, businessImage{MIME: x.MIME, Caption: caption, data: x.Data})
	}
	return r, nil
}

// CallNews 保留 Golem 的能力名称，仍经过本机插件开关、目标和名单校验。
func (s *Server) CallNews(capability, receiver string) error {
	mode, ok := map[string]string{"news.today": "today", "news.diagram": "diagram"}[capability]
	if !ok {
		return fmt.Errorf("不支持的新闻能力")
	}
	st := s.store.snapshot()
	if receiver == "" || !slices.ContainsFunc(st.Contacts, func(c Contact) bool { return c.ID == receiver }) {
		return fmt.Errorf("请选择有效接收会话")
	}
	_, e := s.executeBusinessFor(s.ctx, st.Account.ID, st.AccountEpoch, businessRequest{Plugin: "news", Target: receiver, Mode: mode, Send: true}, "插件调用")
	return e
}

func (s *Server) saveBusinessImages(r *businessResult) error {
	for i := range r.Images {
		x := &r.Images[i]
		if len(x.data) == 0 || !strings.HasPrefix(x.MIME, "image/") {
			return fmt.Errorf("新闻图片数据无效")
		}
		var e error
		x.Media, e = s.saveMedia(x.data, x.MIME)
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *Server) loadBusinessImages(r *businessResult) error {
	for i := range r.Images {
		x := &r.Images[i]
		if !strings.HasPrefix(x.Media, "/media/") {
			return fmt.Errorf("预览图片引用无效")
		}
		var e error
		x.data, e = os.ReadFile(s.businessMediaPath(x.Media))
		if e != nil {
			return fmt.Errorf("预览图片不可读取，请重新预览")
		}
	}
	return nil
}
