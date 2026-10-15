// 文件名：toolbox_test.go
// 主要功能：验证 Cron 边界、内容生成、RSS/JSON 契约和外部访问限制，不请求真实外部服务。
// 作者：2guan
package toolbox

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestScheduleAndCalendar(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	base := time.Date(2026, 10, 16, 10, 0, 0, 0, loc)
	c, e := ParseSchedule("0 9 * * 1-5")
	if e != nil {
		t.Fatal(e)
	}
	next, e := c.Next(base)
	if e != nil || next.Weekday() != time.Monday || next.Hour() != 9 {
		t.Fatal(next, e)
	}
	for _, v := range []string{"* * * *", "60 2 * * *", "*/0 * * * *", "0 0 32 * *", "0 0 * * 7"} {
		if _, e = ParseSchedule(v); e == nil {
			t.Fatal("无效 Cron 通过", v)
		}
	}
	c, _ = ParseSchedule("0 0 31 2 *")
	if _, e = c.Next(base); e == nil {
		t.Fatal("不存在日期应报错")
	}
	text, e := Daily(base, `{"2026-10-20":"纪念日"}`)
	if e != nil || !strings.Contains(text, "还有 4 天") {
		t.Fatal(text, e)
	}
}
func TestContentRules(t *testing.T) {
	if id, _ := Match("今天想点歌吗"); id != "" {
		t.Fatal("误触发")
	}
	id, arg := Match("点歌 晴天")
	if id != "music" || arg != "晴天" {
		t.Fatal(id, arg)
	}
	words, e := Words([]string{"界面 设计 设计 OpenAI", "词云 7", "https://example.com"}, "界面", "", 10)
	if e != nil || len(words) != 2 || words[0].Name != "设计" || words[0].Count != 2 {
		t.Fatal(words)
	}
	if _, e := Meal("牛肉饭\n牛肉面", "牛肉"); e == nil {
		t.Fatal("排除项未生效")
	}
	text, e := Meal("饭\n面", "饭")
	if e != nil || !strings.HasSuffix(text, "面") {
		t.Fatal(text, e)
	}
}
func TestFeeds(t *testing.T) {
	rss := `<rss><channel><item><title>科技新消息</title><link>https://example.com/news</link></item><item><title>内网</title><link>http://127.0.0.1</link></item></channel></rss>`
	items, e := Feed([]byte(rss), "科技", 5)
	if e != nil || len(items) != 1 || items[0].Title != "科技新消息" {
		t.Fatal(items, e)
	}
	atom := `<feed xmlns="http://www.w3.org/2005/Atom"><entry><title>测试</title><link href="https://example.com/item" rel="alternate"/></entry></feed>`
	items, e = Feed([]byte(atom), "", 5)
	if e != nil || len(items) != 1 {
		t.Fatal(items, e)
	}
}

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestServiceAndPublicURL(t *testing.T) {
	for _, v := range []string{"file:///tmp/test", "http://127.0.0.1/a", "http://10.0.0.1", "http://[::1]", "http://169.254.169.254/", "http://100.64.0.1", "https://name:secret@example.com"} {
		if PublicURL(v) == nil {
			t.Fatal("未阻止非公网地址", v)
		}
	}
	calls := 0
	client := &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("q") != "演示" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("查询或认证丢失")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"items":[{"title":"歌曲","url":"https://example.com/track","media_url":"https://example.com/audio.mp3"}]}`))}, nil
	})}
	items, e := Search(context.Background(), client, "https://example.com/search", "fixture", "演示", 5)
	if e != nil || len(items) != 1 || calls != 1 {
		t.Fatal(items, e, calls)
	}
	if _, _, e = ReadURL(context.Background(), client, "http://127.0.0.1", "", 100); e == nil || calls != 1 {
		t.Fatal("发起了本机请求")
	}
}
