package toolbox

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func newsFixture() []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	return b.Bytes()
}
func TestNewsCommands(t *testing.T) {
	for input, arg := range map[string]string{"今日新闻": "今日新闻", "今日图卦": "今日图卦", "@测试昵称\u2005今日图卦": "今日图卦", "新闻": "", "新闻 科技": "科技", "RSS新闻": "RSS ", "RSS新闻 体育": "RSS 体育"} {
		id, got := Match(input)
		if id != "news" || got != arg {
			t.Errorf("%q: %q %q", input, id, got)
		}
	}
	for _, text := range []string{"今天的新闻不错", "今日新闻很好看", "图卦", "RSS新闻学"} {
		if id, _ := Match(text); id == "news" {
			t.Fatal("误触发普通聊天", text)
		}
	}
}
func TestNewsImagesUseBeijingDateAndCompleteCache(t *testing.T) {
	var calls []string
	failSecond := true
	client := &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.String())
		if r.Header.Get("Authorization") != "" {
			t.Error("公开图片携带凭据")
		}
		status, body := 200, newsFixture()
		if failSecond && strings.HasSuffix(r.URL.Path, "2.jpg") {
			status = 404
			body = []byte("missing")
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	now := time.Date(2026, 1, 1, 17, 0, 0, 0, time.UTC) // 北京 1 月 2 日，图卦应取 1 月 1 日。
	var cache NewsCache
	if _, e := cache.Get(context.Background(), client, "diagram", "", now); e == nil {
		t.Fatal("第二张失败仍返回不完整图卦")
	}
	failSecond = false
	edition, e := cache.Get(context.Background(), client, "diagram", "", now)
	if e != nil || edition.Date != "2026-01-01" || len(edition.Images) != 2 || !strings.Contains(calls[0], "/202601/tugua/202601011.jpg") || len(calls) != 4 {
		t.Fatal("日期错误或缓存了半组图", e, calls)
	}
	edition.Images[0].Data[0] = 0
	cached, e := cache.Get(context.Background(), client, "diagram", "", now)
	if e != nil || cached.Images[0].Data[0] == 0 || len(calls) != 4 {
		t.Fatal("缓存被调用者修改或未命中", e)
	}
	if _, e := cache.Get(context.Background(), client, "today", "", now); e != nil || !strings.HasSuffix(calls[len(calls)-1], "/2026-01-02.png") {
		t.Fatal("今日新闻没有按北京时间请求", e)
	}
	if _, e := cache.Get(context.Background(), client, "today", "", now.AddDate(0, 0, 1)); e != nil || !strings.HasSuffix(calls[len(calls)-1], "/2026-01-03.png") {
		t.Fatal("跨日仍使用旧缓存", e)
	}
	before := len(calls)
	cache.Clear()
	_, _ = cache.Get(context.Background(), client, "today", "", now.AddDate(0, 0, 1))
	if len(calls) != before+1 {
		t.Fatal("清理未失效")
	}
}
func TestNewsRejectsNonImageAndPrivateSources(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(strings.NewReader("<html>not an image</html>"))}, nil
	})}
	var cache NewsCache
	for _, template := range []string{"https://example.com/{date}.png", "http://127.0.0.1/{date}.png"} {
		if _, e := cache.Get(context.Background(), client, "today", template, time.Now()); e == nil {
			t.Fatal("接受伪图片或内网地址")
		}
	}
	if calls != 1 {
		t.Fatal("内网请求进入传输层")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := cache.Get(ctx, client, "today", "", time.Now()); e == nil || calls != 1 {
		t.Fatal("取消后仍请求")
	}
}
