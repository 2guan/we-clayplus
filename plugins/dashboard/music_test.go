package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/plugins/toolbox"
	sdkmessage "github.com/we-clay/we-clay/sdk/message"
)

func TestMusicDefaultsUpgradePreservesExistingService(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{true: "custom-items", false: "unconfigured"}[custom], func(t *testing.T) {
			dir := t.TempDir()
			s, e := openStore(dir, false)
			if e != nil {
				t.Fatal(e)
			}
			if businessText(s.snapshot(), "music", "endpoint") != toolbox.DefaultMusicEndpoint || businessText(s.snapshot(), "music", "send_mode") != "card" {
				t.Fatal("新安装未带入 Golem 默认")
			}
			e = s.change(func(st *State) error {
				delete(st.Settings, "music.source")
				delete(st.Settings, "music.api_format")
				delete(st.Settings, "music.send_mode")
				delete(st.Settings, "music.app_id")
				st.Settings["music.endpoint"] = ""
				if custom {
					st.Settings["music.endpoint"] = "https://example.com/legacy?fixed=1"
				}
				st.Settings["music.scope"] = "whitelist"
				st.Settings["music.targets"] = "fixture-peer"
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
			s.db.Close()
			s, e = openStore(dir, false)
			if e != nil {
				t.Fatal(e)
			}
			st := s.snapshot()
			wantFormat, wantMode, wantSource := "golem", "card", "auto"
			if custom {
				wantFormat, wantMode, wantSource = "items", "link", "custom"
			}
			if businessText(st, "music", "source") != wantSource || businessText(st, "music", "api_format") != wantFormat || businessText(st, "music", "send_mode") != wantMode || businessText(st, "music", "scope") != "whitelist" || businessText(st, "music", "targets") != "fixture-peer" {
				t.Fatal("旧配置升级错误")
			}
			if custom && businessText(st, "music", "endpoint") != "https://example.com/legacy?fixed=1" {
				t.Fatal("覆盖原调用地址")
			}
			e = s.change(func(st *State) error {
				st.Settings["music.source"] = "douyin"
				st.Settings["music.api_format"] = "items"
				st.Settings["music.send_mode"] = "link"
				st.Settings["music.app_id"] = ""
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
			s.db.Close()
			s, e = openStore(dir, false)
			if e != nil {
				t.Fatal(e)
			}
			defer s.db.Close()
			if businessText(s.snapshot(), "music", "source") != "douyin" || businessText(s.snapshot(), "music", "api_format") != "items" || businessText(s.snapshot(), "music", "send_mode") != "link" || businessText(s.snapshot(), "music", "app_id") != "" {
				t.Fatal("重启覆盖手动配置")
			}
		})
	}
}

func TestMusicPreviewSendAndTrustedRetry(t *testing.T) {
	s := newServer(t, true)
	r := businessCall(t, s, businessRequest{Plugin: "music", Target: "mock-contact-0", Query: "测试歌名"})
	if r.MusicSource != "脱机示例" || r.Music == nil || r.Music.Title == "" || r.Music.Lyric == "" {
		t.Fatal("没有生成完整音乐卡片")
	}
	if w := call(t, s, "/api/music-media?preview=1&id="+r.Preview+"&part=audio", nil, nil, ""); w.Code != http.StatusTemporaryRedirect {
		t.Fatal("脱机试听访问了外部资源", w.Code)
	}
	if w := call(t, s, "/api/business", map[string]any{"operation": "send", "id": r.Preview, "music": map[string]string{"title": "伪造标题"}}, nil, ""); w.Code != 400 {
		t.Fatal("预览发送接受了额外卡片参数", w.Code)
	}
	if w := call(t, s, "/api/business", map[string]any{"operation": "send", "id": r.Preview}, nil, ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	st := s.store.snapshot()
	m := st.Messages[len(st.Messages)-1]
	if m.Kind != "music" || m.Music == nil || *m.Music != *r.Music || m.Status != "sent" {
		t.Fatal("发送没有使用可信快照")
	}
	if e := s.store.change(func(st *State) error { st.Messages[len(st.Messages)-1].Status = "failed"; return nil }); e != nil {
		t.Fatal(e)
	}
	if e := s.resend(context.Background(), actionRequest{ID: m.ID, Target: m.Target, Content: "伪造正文", Music: &sdkmessage.Music{Title: "伪造标题"}}); e != nil {
		t.Fatal(e)
	}
	st = s.store.snapshot()
	next := st.Messages[len(st.Messages)-1]
	if next.Music == nil || *next.Music != *r.Music {
		t.Fatal("重发替换了原卡片")
	}
	var q actionRequest
	if json.Unmarshal([]byte(`{"group":"music","content":"伪造","music":{"title":"伪造","audio_url":"https://example.com/a.mp3"}}`), &q) != nil || q.Music != nil {
		t.Fatal("浏览器可以注入原生音乐参数")
	}
	q.Target = m.Target
	if e := s.send(context.Background(), q, "manual"); e == nil {
		t.Fatal("没有可信卡片数据仍发送")
	}
	for _, query := range []string{"id=missing&part=audio", "id=" + m.ID + "&part=arbitrary", "url=https://example.com/audio&part=audio"} {
		if w := call(t, s, "/api/music-media?"+query, nil, nil, ""); w.Code == 200 || w.Code == http.StatusTemporaryRedirect {
			t.Fatal("任意资源可被代理")
		}
	}
	if w := call(t, s, "/api/music-media", map[string]string{}, nil, ""); w.Code != 405 {
		t.Fatal("代理接受写请求")
	}
	if _, e := s.buildMusic(context.Background(), businessRequest{Query: "歌名", Mode: "arbitrary"}); e == nil {
		t.Fatal("接受无效分享方式")
	}
}

func TestMusicInboundRichCard(t *testing.T) {
	music := sdkmessage.Music{Title: "歌名<&>", Artist: "歌手", AudioURL: "https://example.com/a.mp3", Cover: "https://example.com/a.jpg", Lyric: "歌词"}
	raw, e := music.XML()
	if e != nil {
		t.Fatal(e)
	}
	m, ok := parseRichMessage(Message{ID: "fixture-music", Kind: "app"}, "<msg>"+raw+"</msg>")
	if !ok || m.Kind != "music" || m.Music == nil || *m.Music != music || m.Content != "[音乐] 歌名<&> - 歌手" {
		t.Fatal("未识别收到的音乐卡片", m)
	}
	if _, ok = parseRichMessage(Message{Kind: "text"}, "<msg>"+raw+"</msg>"); ok {
		t.Fatal("普通文字可以冒充音乐卡片")
	}
}

func TestMusicPreparationFailureRepliesOnlyToAutomaticRequest(t *testing.T) {
	s := newServer(t, true)
	before := len(s.store.snapshot().Messages)
	q := businessRequest{Plugin: "music", Target: "mock-contact-0", Query: "晴天", Mode: "invalid"}
	if _, e := s.executeBusiness(context.Background(), q, "工作台"); e == nil {
		t.Fatal("准备失败未返回错误")
	}
	if len(s.store.snapshot().Messages) != before {
		t.Fatal("工作台失败自动发送了微信消息")
	}
	q.Send = true
	if _, e := s.executeBusiness(context.Background(), q, "微信触发"); e == nil {
		t.Fatal("准备失败被报告为成功")
	}
	st := s.store.snapshot()
	if len(st.Messages) != before+1 {
		t.Fatal("没有且仅有一条失败提示")
	}
	m := st.Messages[len(st.Messages)-1]
	if m.Target != q.Target || m.Kind != "text" || m.Status != "sent" || !strings.HasPrefix(m.Content, "点歌失败：") || m.Music != nil {
		t.Fatal("失败提示会话或内容错误", m.Kind, m.Status)
	}
	var status string
	if e := s.store.db.QueryRow("SELECT status FROM business_runs WHERE plugin='music' ORDER BY rowid DESC LIMIT 1").Scan(&status); e != nil || status != "failed" {
		t.Fatal("提示发送成功掩盖了点歌失败", status, e)
	}
	if e := s.store.change(func(st *State) error {
		for i, p := range st.Plugins {
			if p.ID == "music" {
				st.Plugins[i].Enabled = false
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.executeBusiness(context.Background(), q, "微信触发"); e == nil || len(s.store.snapshot().Messages) != before+1 {
		t.Fatal("停用后仍发送提示")
	}
}
