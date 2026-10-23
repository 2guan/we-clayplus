package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewsCommandsHaveIndependentCooldownAndRecordSkips(t *testing.T) {
	s := newServer(t, true)
	self := s.store.snapshot().Account.ID
	for i, text := range []string{"今日新闻", "今日图卦", "RSS新闻", "新闻", "今日图卦"} {
		if e := s.Record(Message{Mentions: []string{self}, ID: fmt.Sprintf("news-cooldown-%d", i), Target: "mock-contact-0", SenderID: "mock-contact-0", Kind: "text", Content: text, Source: "wechat", Time: time.Now().Format(time.RFC3339)}); e != nil {
			t.Fatal(e)
		}
	}
	waitBusiness(t, func() bool {
		var n int
		_ = s.store.db.QueryRow("SELECT count(*) FROM business_runs WHERE plugin='news' AND status='sent'").Scan(&n)
		return n == 3
	})
	var photos, texts int
	for _, m := range s.store.snapshot().Messages {
		if m.Source == "plugin:news" {
			if m.Kind == "image" {
				photos++
			} else {
				texts++
			}
		}
	}
	if photos != 3 || texts != 1 {
		t.Fatalf("新闻与图卦相互拦截或重复发送：图片=%d，文字=%d", photos, texts)
	}
	var skipped int
	_ = s.store.db.QueryRow("SELECT count(*) FROM business_runs WHERE plugin='news' AND status='skipped' AND detail LIKE '%冷却中%'").Scan(&skipped)
	if skipped != 2 {
		t.Fatal("同类冷却未生效或未记录原因", skipped)
	}
}

func TestNewsCooldownRejectDoesNotExtendWindow(t *testing.T) {
	s := newServer(t, true)
	now := time.Now().Truncate(time.Second)
	q := businessRequest{Plugin: "news", Target: "mock-contact-0", Mode: "diagram"}
	for _, tc := range []struct {
		after time.Duration
		want  bool
	}{{0, true}, {29 * time.Second, false}, {30 * time.Second, true}} {
		got, e := s.claimNewsCooldown("mock-account", q, now.Add(tc.after))
		if e != nil || got != tc.want {
			t.Fatalf("冷却窗口错误：%s，允许=%v，错误=%v", tc.after, got, e)
		}
	}
	q.Target = "mock-contact-1"
	if ok, e := s.claimNewsCooldown("mock-account", q, now); e != nil || !ok {
		t.Fatal("不同会话共用了冷却", e)
	}
}

func TestNewsPreviewImagesAndMissingSecondDoesNotSend(t *testing.T) {
	s := newServer(t, true)
	before := len(s.store.snapshot().Messages)
	r := businessCall(t, s, businessRequest{Plugin: "news", Target: "mock-contact-0", Mode: "diagram"})
	if len(r.Images) != 2 || r.Images[0].Media == r.Images[1].Media || !strings.Contains(r.Text, "昨日") || len(s.store.snapshot().Messages) != before {
		t.Fatal("预览不完整或有发送副作用")
	}
	data, _ := os.ReadFile(s.businessMediaPath(r.Images[1].Media))
	_ = os.Remove(s.businessMediaPath(r.Images[1].Media))
	if e := s.sendBusinessPreview(context.Background(), r.Preview); e == nil || len(s.store.snapshot().Messages) != before {
		t.Fatal("预览缺图仍部分发送")
	}
	_ = os.WriteFile(s.businessMediaPath(r.Images[1].Media), data, 0600)
	if e := s.sendBusinessPreview(context.Background(), r.Preview); e != nil {
		t.Fatal(e)
	}
	messages := s.store.snapshot().Messages
	if len(messages) != before+2 || messages[before].Kind != "image" || !strings.HasSuffix(messages[before].Content, "1/2") || !strings.HasSuffix(messages[before+1].Content, "2/2") {
		t.Fatal("未按顺序发送双图")
	}
	if e := s.sendBusinessPreview(context.Background(), r.Preview); e == nil {
		t.Fatal("同一预览可以重复发送")
	}
}
func TestNewsRechecksPermissionBetweenImages(t *testing.T) {
	s := newServer(t, true)
	r := businessCall(t, s, businessRequest{Plugin: "news", Target: "mock-contact-0", Mode: "diagram"})
	before := len(s.store.snapshot().Messages)
	done := make(chan error, 1)
	go func() { done <- s.sendBusinessPreview(context.Background(), r.Preview) }()
	waitBusiness(t, func() bool { return len(s.store.snapshot().Messages) == before+1 })
	_ = s.store.change(func(st *State) error {
		for i := range st.Plugins {
			if st.Plugins[i].ID == "news" {
				st.Plugins[i].Enabled = false
			}
		}
		return nil
	})
	if e := <-done; e == nil || !strings.Contains(e.Error(), "第 2/2") || len(s.store.snapshot().Messages) != before+1 {
		t.Fatal("停用后继续发送，或丢失部分发送信息", e)
	}
}
func TestNewsDefaultsKeepRSSAndCapabilitiesRespectScope(t *testing.T) {
	s := newServer(t, true)
	if businessText(s.store.snapshot(), "news", "default_mode") != "today" {
		t.Fatal("新安装不是图片新闻")
	}
	st := s.store.snapshot()
	st.Settings["news.endpoint"] = "https://example.com/feed"
	raw, _ := json.Marshal(map[string]any{"settings": map[string]any{"news.endpoint": "https://example.com/feed"}})
	migrateNewsDefaults(&st, raw)
	if businessText(st, "news", "default_mode") != "rss" {
		t.Fatal("旧 RSS 默认被覆盖")
	}
	st.Settings["news.default_mode"] = "diagram"
	raw, _ = json.Marshal(map[string]any{"settings": st.Settings})
	migrateNewsDefaults(&st, raw)
	if businessText(st, "news", "default_mode") != "diagram" {
		t.Fatal("覆盖人工类型配置")
	}
	before := len(s.store.snapshot().Messages)
	for _, q := range []struct{ cap, target string }{{"news.unknown", "mock-contact-0"}, {"news.today", ""}, {"news.today", "missing-peer"}} {
		if e := s.CallNews(q.cap, q.target); e == nil {
			t.Fatal("能力缺少有效参数仍发送")
		}
	}
	_ = s.store.change(func(st *State) error {
		st.Settings["news.scope"] = "whitelist"
		st.Settings["news.targets"] = ""
		return nil
	})
	if e := s.CallNews("news.today", "mock-contact-0"); e == nil || len(s.store.snapshot().Messages) != before {
		t.Fatal("能力绕过范围")
	}
	_ = s.store.change(func(st *State) error { st.Settings["news.scope"] = "blacklist"; return nil })
	if e := s.CallNews("news.today", "mock-contact-0"); e != nil {
		t.Fatal(e)
	}
	if len(s.store.snapshot().Messages) != before+1 {
		t.Fatal("能力未发送新闻图")
	}
}
func TestNewsPreviewsProtectedFromMediaCleanup(t *testing.T) {
	s := newServer(t, true)
	r := businessCall(t, s, businessRequest{Plugin: "news", Target: "mock-contact-0", Mode: "diagram"})
	for _, x := range r.Images {
		old := time.Now().Add(-2 * time.Hour)
		_ = os.Chtimes(s.businessMediaPath(x.Media), old, old)
	}
	files, e := s.cacheFiles("media", 0, s.store.snapshot())
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range files {
		for _, x := range r.Images {
			if f.name == strings.TrimPrefix(x.Media, "/media/") {
				t.Fatal("有效预览被当作无引用缓存")
			}
		}
	}
}
