package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/toolbox"
	sdkmessage "github.com/we-clay/we-clay/sdk/message"
)

// 只读取当前账号已归档或已预览的卡片资源，浏览器不能指定任意 URL。
func (s *Server) musicMedia(w http.ResponseWriter, r *http.Request, u User) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "请求方法不支持")
		return
	}
	part, id := r.URL.Query().Get("part"), r.URL.Query().Get("id")
	if id == "" || part != "cover" && part != "audio" {
		writeError(w, 400, "音乐资源参数无效")
		return
	}
	st := s.store.snapshot()
	var music *sdkmessage.Music
	if r.URL.Query().Get("preview") == "1" {
		if e := requireAdmin(u); e != nil {
			writeError(w, 403, e.Error())
			return
		}
		var raw []byte
		var saved struct{ Result businessResult }
		if s.store.db.QueryRow("SELECT body FROM business_previews WHERE id=? AND account=? AND time>=?", id, st.Account.ID, time.Now().Add(-time.Hour).Format(time.RFC3339)).Scan(&raw) == nil && json.Unmarshal(raw, &saved) == nil {
			music = saved.Result.Music
		}
	} else {
		for _, m := range st.Messages {
			if m.ID == id && m.Kind == "music" {
				music = m.Music
				break
			}
		}
	}
	if music == nil || music.Validate() != nil {
		writeError(w, 404, "音乐卡片不存在或已过期")
		return
	}
	raw, limit := music.AudioURL, int64(16<<20)
	if part == "cover" {
		raw = music.Cover
		limit = 8 << 20
	}
	if raw == "" {
		writeError(w, 404, "音乐资源不存在")
		return
	}
	if s.cfg.Mock {
		target := "/sample-audio.wav"
		if part == "cover" {
			target = "/sample-avatar.svg"
		}
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		return
	}
	client := toolbox.HTTPClient()
	defer client.CloseIdleConnections()
	body, _, e := toolbox.ReadMusicURL(r.Context(), client, raw, "", limit)
	if e != nil {
		writeError(w, 502, e.Error())
		return
	}
	mime := http.DetectContentType(body)
	if part == "audio" {
		mime = toolbox.MusicAudioMIME(body)
	}
	if part == "cover" && mime != "image/jpeg" && mime != "image/png" && mime != "image/gif" && mime != "image/webp" || part == "audio" && mime == "" {
		writeError(w, 502, "音乐资源格式不支持")
		return
	}
	if s.store.snapshot().Account.ID != st.Account.ID {
		writeError(w, 409, "账号发生变化")
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
}

// 保留自定义服务；原 Golem 默认升级为酷我优先、抖音备用。
func migrateMusicDefaults(st *State, raw []byte) {
	if len(raw) == 0 {
		return
	}
	var stored struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if json.Unmarshal(raw, &stored) != nil {
		return
	}
	changed := false
	custom := businessText(*st, "music", "endpoint") != "" && strings.Split(businessText(*st, "music", "endpoint"), "?")[0] != toolbox.DefaultMusicEndpoint
	if _, ok := stored.Settings["music.source"]; !ok {
		st.Settings["music.source"] = map[bool]string{true: "custom", false: "auto"}[custom]
		changed = true
	}
	if _, ok := stored.Settings["music.api_format"]; !ok {
		st.Settings["music.api_format"] = map[bool]string{true: "items", false: "golem"}[custom]
		changed = true
	}
	if _, ok := stored.Settings["music.send_mode"]; !ok {
		st.Settings["music.send_mode"] = map[bool]string{true: "link", false: "card"}[custom]
		changed = true
	}
	if !custom && businessText(*st, "music", "endpoint") == "" {
		st.Settings["music.endpoint"] = toolbox.DefaultMusicEndpoint
		changed = true
	}
	if changed {
		st.ConfigRevision++
	}
}

func (s *Server) buildMusic(ctx context.Context, q businessRequest) (businessResult, error) {
	return s.buildMusicFor(ctx, q, s.store.snapshot())
}

func (s *Server) buildMusicFor(ctx context.Context, q businessRequest, st State) (businessResult, error) {
	r := businessResult{Mock: s.cfg.Mock}
	if s.cfg.Mock {
		r.MusicSource = "脱机示例"
	}
	if len(strings.TrimSpace(q.Query)) == 0 {
		return r, fmt.Errorf("请填写歌名或歌手")
	}
	if q.Mode != "" && q.Mode != "card" && q.Mode != "link" && q.Mode != "media" {
		return r, fmt.Errorf("音乐分享形式无效")
	}
	if s.cfg.Mock {
		if st.Scenario == "error" {
			return r, fmt.Errorf("模拟音乐服务暂时不可用")
		}
		r.Items = []toolbox.Item{{Title: q.Query + " · 脱机示例", Artist: "演示歌手", URL: "https://example.com/song", MediaURL: "https://example.com/song.mp3", Lyric: "演示歌词，不连接真实音乐服务。"}}
	} else {
		queries := []string{strings.TrimSpace(q.Query)}
		if musicNeedsPick(q.Query) {
			var e error
			queries, r.Task, e = s.pickMusic(ctx, q, st)
			if e != nil {
				return r, e
			}
		}
		client := toolbox.HTTPClient()
		defer client.CloseIdleConnections()
		var e error
		for _, query := range queries {
			if businessText(st, "music", "source") == "custom" {
				key := ""
				if env := businessText(st, "music", "key_env"); env != "" {
					key = os.Getenv(env)
					if key == "" {
						return r, fmt.Errorf("音乐凭据环境变量未设置")
					}
				}
				r.Items, e = toolbox.SearchMusic(ctx, client, businessText(st, "music", "endpoint"), key, query, businessText(st, "music", "api_format"), businessNumber(st, "music", "limit"))
				r.MusicSource = "自定义服务"
			} else {
				r.Items, r.MusicSource, e = toolbox.SearchMusicSources(ctx, client, businessText(st, "music", "source"), query, businessNumber(st, "music", "limit"))
			}
			if e == nil && len(r.Items) > 0 || ctx.Err() != nil {
				break
			}
		}
		if e != nil {
			return r, e
		}
	}
	if len(r.Items) == 0 {
		return r, fmt.Errorf("没有找到歌曲，请更换关键词")
	}
	if q.Index < 0 || q.Index >= len(r.Items) {
		return r, fmt.Errorf("歌曲序号无效")
	}
	item := r.Items[q.Index]
	r.Text = item.Title + " - " + item.Artist + "\n" + item.URL
	mode := q.Mode
	if mode == "" {
		mode = businessText(st, "music", "send_mode")
	}
	if mode == "" {
		mode = "card"
	}
	if mode == "card" {
		id, e := toolbox.MusicAppID(businessText(st, "music", "app_id"))
		if e != nil {
			return r, e
		}
		r.Music = &sdkmessage.Music{AppID: id, Title: item.Title, Artist: item.Artist, URL: item.URL, AudioURL: item.MediaURL, Cover: item.Cover, Lyric: item.Lyric}
		if e := r.Music.Validate(); e != nil {
			return r, e
		}
	} else if mode == "media" {
		if s.cfg.Mock {
			r.Media = "/sample-audio.wav"
			r.MIME = "audio/wav"
			return r, nil
		}
		if item.MediaURL == "" {
			return r, fmt.Errorf("此歌曲没有可试听的媒体地址")
		}
		client := toolbox.HTTPClient()
		defer client.CloseIdleConnections()
		var e error
		r.data, r.MIME, e = toolbox.ReadMusicURL(ctx, client, item.MediaURL, "", 16<<20)
		if e != nil {
			return r, e
		}
		r.MIME = toolbox.MusicAudioMIME(r.data)
		if r.MIME == "" {
			return r, fmt.Errorf("音乐服务未返回可识别的音频")
		}
	}
	return r, nil
}
