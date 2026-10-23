package dashboard

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/turtlesoup"
)

func (s *Server) soupAPI(w http.ResponseWriter, r *http.Request, u User) {
	w.Header().Set("Cache-Control", "no-store")
	if err := requireAdmin(u); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	st := s.store.snapshot()
	account := st.Account.ID
	if account == "" {
		writeError(w, 400, "微信账号尚未就绪")
		return
	}
	if r.Method == http.MethodGet {
		puzzles, err := s.soupPuzzles(account)
		if err != nil {
			writeError(w, 500, "题库读取失败")
			return
		}
		games, err := s.soupGames(account, r.URL.Query().Get("target"))
		if err != nil {
			writeError(w, 500, "对局读取失败")
			return
		}
		turns, err := s.soupTurns(account, r.URL.Query().Get("target"))
		if err != nil {
			writeError(w, 500, "回合读取失败")
			return
		}
		writeJSON(w, 200, map[string]any{"puzzles": puzzles, "games": games, "turns": turns})
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "请求方法不支持")
		return
	}
	var q struct {
		Operation string              `json:"operation"`
		ID        string              `json:"id"`
		Revision  int                 `json:"revision"`
		Puzzle    *turtlesoup.Puzzle  `json:"puzzle"`
		Puzzles   []turtlesoup.Puzzle `json:"puzzles"`
		Verdict   *turtlesoup.Verdict `json:"verdict"`
		Target    string              `json:"target"`
		Text      string              `json:"text"`
	}
	if !decode(w, r, &q) {
		return
	}
	s.soupMu.Lock()
	defer s.soupMu.Unlock()
	if active := s.store.snapshot(); active.Account.ID != account || active.AccountEpoch != st.AccountEpoch {
		writeError(w, 409, "账号已变化，请刷新")
		return
	}
	var err error
	switch q.Operation {
	case "simulate":
		if !s.cfg.Mock {
			err = fmt.Errorf("真实模式不能模拟微信消息")
			break
		}
		if len(q.Text) > 3000 || !slices.ContainsFunc(st.Contacts, func(c Contact) bool { return c.ID == q.Target }) {
			err = fmt.Errorf("请选择会话并输入不超过 3000 字节的消息")
			break
		}
		m := Message{ID: randomID(), Target: q.Target, SenderID: q.Target, Sender: "演示玩家", Kind: "text", Content: q.Text, Source: "wechat", Status: "sent", Time: time.Now().Format(time.RFC3339)}
		if memoryGroup(st, q.Target) {
			m.SenderID = "mock-soup-player"
			if command, _ := soupCommand(q.Text); command == "start" {
				m.Mentions = []string{st.Account.ID}
				m.Content = "@" + st.Account.Name + "\u2005" + m.Content
			}
		}
		s.soupMu.Unlock()
		err = s.Record(m)
		s.soupMu.Lock()
	case "save-puzzle":
		if q.Puzzle == nil {
			err = fmt.Errorf("缺少题目")
			break
		}
		if q.Puzzle.ID == "" {
			q.Puzzle.ID = randomID()
		}
		if len(q.Puzzle.ID) > 100 || strings.ContainsAny(q.Puzzle.ID, "\r\n\x00") {
			err = fmt.Errorf("题目 ID 无效")
			break
		}
		err = s.saveSoupPuzzle(account, *q.Puzzle)
	case "import":
		if len(q.Puzzles) == 0 || len(q.Puzzles) > 50 {
			err = fmt.Errorf("每次导入 1—50 道题目")
			break
		}
		for i := range q.Puzzles {
			p := &q.Puzzles[i]
			p.ID = randomID()
			if err = turtlesoup.Validate(*p); err != nil {
				break
			}
		}
		if err != nil {
			break
		}
		// 全部校验后统一提交，导入不会产生半份题库。
		tx, e := s.store.db.Begin()
		if e != nil {
			err = e
			break
		}
		for _, p := range q.Puzzles {
			raw, e := s.store.sealMemory(account, "soup-puzzle:"+p.ID, p)
			if e != nil {
				err = e
				break
			}
			_, err = tx.Exec("INSERT INTO soup_puzzles VALUES(?,?,?)", account, p.ID, raw)
			if err != nil {
				break
			}
		}
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
	case "pause", "resume", "end":
		g, e := s.soupGame(account, q.ID)
		err = e
		if err != nil {
			break
		}
		if g.Revision != q.Revision {
			err = fmt.Errorf("对局已变化，请刷新")
			break
		}
		if !slices.Contains([]string{"playing", "paused", "failed", "generating"}, g.Status) {
			err = fmt.Errorf("这局已经结束")
			break
		}
		if q.Operation == "resume" && g.Puzzle.Surface == "" {
			err = fmt.Errorf("题目尚未生成，请重试出题")
			break
		}
		g.Status = map[string]string{"pause": "paused", "resume": "playing", "end": "ended"}[q.Operation]
		err = s.saveSoupGame(account, &g)
	case "retry":
		t, e := s.soupTurn(account, q.ID)
		err = e
		if err != nil {
			break
		}
		if t.Status != "failed" || !t.Retryable {
			err = fmt.Errorf("只有未发送的失败回合可以重试")
			break
		}
		if err = s.soupTurnAllowed(account, st.AccountEpoch, t); err != nil {
			break
		}
		if t.GameID != "" {
			g, e := s.soupGame(account, t.GameID)
			live, liveErr := s.liveSoup(account, t.Message.Target)
			if e != nil || liveErr == nil && live.ID != t.GameID || t.Output == "" && !slices.Contains([]string{"playing", "generating", "failed", "paused"}, g.Status) {
				err = fmt.Errorf("原对局已结束或已换题，不能重试旧回合")
				break
			}
			if g.Status == "paused" && t.Command == "question" && t.Output == "" {
				err = fmt.Errorf("对局已暂停，请先恢复再重试")
				break
			}
		}
		t.Status = "queued"
		t.Epoch = st.AccountEpoch
		t.Retryable = false
		t.Error = ""
		err = s.saveSoupTurn(account, t)
		if err == nil {
			s.startSoupWorkerLocked(account, st.AccountEpoch, t.Message.Target)
		}
	case "correct":
		t, e := s.soupTurn(account, q.ID)
		err = e
		if err != nil {
			break
		}
		g, e := s.liveSoup(account, t.Message.Target)
		err = e
		if err != nil {
			break
		}
		if s.soupWorkers[account+"\x00"+g.Target] {
			err = fmt.Errorf("当前会话正在处理，请稍后纠正")
			break
		}
		if t.GameID != g.ID || q.Verdict == nil || g.Revision != q.Revision {
			err = fmt.Errorf("原对局已变化或缺少裁定")
			break
		}
		if err = turtlesoup.ValidateVerdict(*q.Verdict, g.Puzzle); err != nil {
			break
		}
		t.Verdict = q.Verdict
		t.Output = "后台纠正：" + turtlesoup.Answer(*q.Verdict)
		if err = s.saveSoupTurn(account, t); err != nil {
			break
		}
		turns, e := s.soupTurns(account, g.Target)
		err = e
		if err != nil {
			break
		}
		g.Known = nil
		for _, old := range turns {
			if old.GameID != g.ID || old.Verdict == nil || old.Verdict.Intent != "guess" {
				continue
			}
			for _, id := range old.Verdict.Facts {
				if !slices.Contains(g.Known, id) {
					g.Known = append(g.Known, id)
				}
			}
		}
		err = s.saveSoupGame(account, &g)
	default:
		err = fmt.Errorf("操作不支持")
	}
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.notify()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
