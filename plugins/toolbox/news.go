// 今日新闻与图卦的数据源、双图顺序参考 Golem plugins/news（作者 ovo，MIT）。
package toolbox

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	DefaultNewsTemplate    = "https://cdn.jsdmirror.com/gh/vikiboss/60s-static-host@main/static/images/{date}.png"
	DefaultDiagramTemplate = "https://penti.5aihj.com/{month}/tugua/{date}{index}.jpg"
)

type NewsImage struct {
	Data []byte
	MIME string
}
type NewsEdition struct {
	Date   string
	Images []NewsImage
}
type newsEntry struct {
	key     string
	edition NewsEdition
}

// 只保留新闻和图卦各一份有效日期的公开图片；失败不缓存。
type NewsCache struct {
	mu      sync.Mutex
	entries map[string]newsEntry
}

func (c *NewsCache) Clear() { c.mu.Lock(); defer c.mu.Unlock(); c.entries = nil }
func cloneEdition(v NewsEdition) NewsEdition {
	out := NewsEdition{Date: v.Date}
	for _, x := range v.Images {
		out.Images = append(out.Images, NewsImage{Data: bytes.Clone(x.Data), MIME: x.MIME})
	}
	return out
}
func (c *NewsCache) Get(ctx context.Context, client *http.Client, mode, template string, now time.Time) (NewsEdition, error) {
	if ctx.Err() != nil {
		return NewsEdition{}, ctx.Err()
	}
	t := now.In(time.FixedZone("北京时间", 8*3600))
	count := 1
	switch mode {
	case "today":
		if template == "" {
			template = DefaultNewsTemplate
		}
	case "diagram":
		t = t.AddDate(0, 0, -1)
		count = 2
		if template == "" {
			template = DefaultDiagramTemplate
		}
	default:
		return NewsEdition{}, fmt.Errorf("图片新闻类型无效")
	}
	date := t.Format("2006-01-02")
	key := date + "|" + template
	c.mu.Lock()
	cached, ok := c.entries[mode]
	c.mu.Unlock()
	if ok && cached.key == key {
		return cloneEdition(cached.edition), nil
	}
	result := NewsEdition{Date: date}
	for i := 1; i <= count; i++ {
		stamp := date
		if mode == "diagram" {
			stamp = t.Format("20060102")
		}
		u := strings.NewReplacer("{date}", stamp, "{month}", t.Format("200601"), "{index}", fmt.Sprint(i)).Replace(template)
		raw, _, e := ReadURL(ctx, client, u, "", 16<<20)
		if e != nil {
			return NewsEdition{}, fmt.Errorf("%s 第 %d/%d 张图片获取失败：%w", date, i, count, e)
		}
		cfg, format, e := image.DecodeConfig(bytes.NewReader(raw))
		if e != nil || (format != "png" && format != "jpeg") || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 50000 || cfg.Height > 50000 || int64(cfg.Width)*int64(cfg.Height) > 50_000_000 {
			return NewsEdition{}, fmt.Errorf("新闻源未返回有效的 PNG / JPEG 图片，或图片尺寸超过上限")
		}
		result.Images = append(result.Images, NewsImage{Data: raw, MIME: map[string]string{"png": "image/png", "jpeg": "image/jpeg"}[format]})
	}
	if ctx.Err() != nil {
		return NewsEdition{}, ctx.Err()
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]newsEntry{}
	}
	c.entries[mode] = newsEntry{key, cloneEdition(result)}
	c.mu.Unlock()
	return result, nil
}

func newsCommand(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "@") {
		if i := strings.IndexAny(text, " \t\r\n\u2005\u00a0"); i > 0 {
			text = strings.TrimSpace(text[i:])
		}
	}
	if text == "今日新闻" || text == "今日图卦" {
		return text, true
	}
	if text == "RSS新闻" || strings.HasPrefix(text, "RSS新闻 ") {
		return "RSS " + strings.TrimSpace(strings.TrimPrefix(text, "RSS新闻")), true
	}
	if text == "新闻" || strings.HasPrefix(text, "新闻 ") {
		return strings.TrimSpace(strings.TrimPrefix(text, "新闻")), true
	}
	return "", false
}
