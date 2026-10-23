package dashboard

import (
	"fmt"
	"github.com/we-clay/we-clay/plugins/playroom"
	"net/http"
	"slices"
	"strings"
	"time"
)

func (s *Server) playAPI(w http.ResponseWriter, r *http.Request, u User) {
	w.Header().Set("Cache-Control", "no-store")
	if e := requireAdmin(u); e != nil {
		writeError(w, 403, e.Error())
		return
	}
	st := s.store.snapshot()
	account := st.Account.ID
	if account == "" {
		writeError(w, 400, "微信账号未就绪")
		return
	}
	if r.Method == http.MethodGet {
		games, e := s.playGames(account, r.URL.Query().Get("target"), r.URL.Query().Get("plugin"))
		if e != nil {
			writeError(w, 500, "对局读取失败")
			return
		}
		turns, e := s.playTurns(account, r.URL.Query().Get("target"))
		if e != nil {
			writeError(w, 500, "回合读取失败")
			return
		}
		if kind := r.URL.Query().Get("plugin"); kind != "" {
			turns = slices.DeleteFunc(turns, func(t playTurn) bool { return t.Plugin != kind })
		}
		memes, e := s.playMemes(account, r.URL.Query().Get("target"))
		if e != nil {
			writeError(w, 500, "群梗读取失败")
			return
		}
		for i := range memes {
			source, e := s.playSourceMessage(account, memes[i].Target, memes[i].SourceID)
			memes[i].SourceAvailable = e == nil && replyEvidenceHash(source) == memes[i].SourceHash
		}
		writeJSON(w, 200, map[string]any{"games": games, "turns": turns, "memes": memes, "definitions": playroom.Definitions})
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "请求方法不支持")
		return
	}
	var q struct {
		Operation string `json:"operation"`
		ID        string `json:"id"`
		Revision  int    `json:"revision"`
		Target    string `json:"target"`
		Actor     string `json:"actor"`
		Text      string `json:"text"`
		QuoteID   string `json:"quote_id"`
	}
	if !decode(w, r, &q) {
		return
	}
	s.playMu.Lock()
	defer s.playMu.Unlock()
	if now := s.store.snapshot(); now.Account.ID != account || now.AccountEpoch != st.AccountEpoch {
		writeError(w, 409, "账号已变化，请刷新")
		return
	}
	var e error
	switch q.Operation {
	case "simulate":
		if !s.cfg.Mock {
			e = fmt.Errorf("真实模式不允许模拟微信消息")
			break
		}
		if len([]rune(q.Text)) > 4000 || q.Text == "" || !slices.ContainsFunc(st.Contacts, func(c Contact) bool { return c.ID == q.Target }) {
			e = fmt.Errorf("请选择会话并输入不超过4000字的模拟消息")
			break
		}
		actor := q.Actor
		if actor == "" {
			actor = q.Target
			if memoryGroup(st, q.Target) {
				actor = "mock-play-player"
			}
		}
		m := Message{ID: randomID(), Target: q.Target, SenderID: actor, Sender: "演示玩家", Content: q.Text, Kind: "text", Source: "wechat", Status: "sent", Time: time.Now().Format(time.RFC3339)}
		if q.QuoteID != "" {
			m.Rich = &RichMessage{QuoteID: q.QuoteID}
			m.Kind = "quote"
		}
		if memoryGroup(st, q.Target) {
			m.Mentions = []string{st.Account.ID}
			m.Content = "@" + st.Account.Name + "\u2005" + m.Content
		}
		s.playMu.Unlock()
		e = s.Record(m)
		s.playMu.Lock()
	case "retry", "confirm-sent":
		t, err := s.playTurn(account, q.ID)
		if err != nil {
			e = err
			break
		}
		if s.playWorkers[account+"\x00"+t.Target] {
			e = fmt.Errorf("该会话正在处理，请稍后重试")
			break
		}
		if q.Operation == "retry" {
			if t.Status != "failed" || !t.Retryable {
				e = fmt.Errorf("只能重试未发送的失败回合")
				break
			}
		} else {
			if t.Status != "unknown" || t.Cursor >= len(t.Deliveries) {
				e = fmt.Errorf("只有发送结果未知的当前条目可以核对")
				break
			}
			t.Deliveries[t.Cursor].Sent = true
			t.Cursor++
		}
		t.Epoch = st.AccountEpoch
		if e = s.playAllowed(account, st.AccountEpoch, t); e != nil {
			break
		}
		t.Status = "queued"
		t.Retryable = false
		t.Error = ""
		e = s.savePlayTurn(account, t)
		if e == nil {
			s.startPlayWorkerLocked(account, st.AccountEpoch, t.Target)
		}
	case "pause", "resume", "end":
		g, err := s.playGame(account, q.ID)
		if err != nil {
			e = err
			break
		}
		if q.Operation != "end" && s.playWorkers[account+"\x00"+g.Target] {
			e = fmt.Errorf("会话正在处理，请完成后再暂停或恢复；需要立即停止可结束对局")
			break
		}
		if q.Revision != g.Revision {
			e = fmt.Errorf("对局已变化，请刷新")
			break
		}
		if g.Status == "ended" {
			e = fmt.Errorf("对局已结束")
			break
		}
		if q.Operation == "pause" {
			g.Status = "paused"
		} else if q.Operation == "end" {
			g.Status = "ended"
		} else {
			g.Status = "playing"
			if g.Phase == "" {
				g.Status = "lobby"
			}
		}
		if q.Operation == "end" {
			e = s.endPlayGame(account, &g)
		} else {
			e = s.controlPlayGame(account, &g)
		}
	case "meme-confirm", "meme-reject", "meme-delete", "meme-edit":
		memes, err := s.playMemes(account, "")
		if err != nil {
			e = err
			break
		}
		i := slices.IndexFunc(memes, func(m playMeme) bool { return m.ID == q.ID })
		if i < 0 {
			e = fmt.Errorf("群梗不存在")
			break
		}
		m := memes[i]
		if q.Operation == "meme-confirm" {
			source, err := s.playSourceMessage(account, m.Target, m.SourceID)
			if err != nil || replyEvidenceHash(source) != m.SourceHash {
				e = fmt.Errorf("原始来源已变化，不可确认")
				break
			}
			if m.Status != "pending" {
				e = fmt.Errorf("只能确认待审核候选")
				break
			}
			m.Status = "confirmed"
		} else if q.Operation == "meme-edit" {
			if len([]rune(q.Text)) > 2000 || strings.TrimSpace(q.Text) == "" {
				e = fmt.Errorf("说明需要1—2000字")
				break
			}
			if m.Status == "deleted" {
				e = fmt.Errorf("已删除的群梗不能编辑")
				break
			}
			m.Explanation = q.Text
			m.Status = "pending"
		} else if q.Operation == "meme-delete" {
			m.Status = "deleted"
			m.Text = ""
			m.Explanation = ""
		} else {
			m.Status = "rejected"
		}
		e = s.savePlayMeme(account, m)
	default:
		e = fmt.Errorf("不支持的操作")
	}
	if e != nil {
		writeError(w, 400, e.Error())
		return
	}
	s.notify()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
