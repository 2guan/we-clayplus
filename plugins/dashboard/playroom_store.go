// 互动插件共用独立加密存档与逐条发送账本；不改写聊天归档。
package dashboard

import (
	"database/sql"
	"fmt"
	"github.com/we-clay/we-clay/plugins/playroom"
	"time"
)

type playDailyText struct {
	Text string `json:"text"`
	Draw int    `json:"draw,omitempty"`
}

// 旧缓存未保存签号，只从同一人、同一天、正文完全相同的模型生成记录中恢复。
func (s *Server) cachedFortuneDraw(account, target, actor, day, text string) (int, error) {
	st := s.store.snapshot()
	rows, e := s.store.db.Query("SELECT id,body FROM play_turns WHERE account=? AND target=? ORDER BY seq DESC", account, target)
	if e != nil {
		return 0, e
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var b []byte
		var turn playTurn
		if e = rows.Scan(&id, &b); e != nil {
			return 0, e
		}
		if e = s.store.openMemory(account, "play-turn:"+id, b, &turn); e != nil {
			return 0, e
		}
		stamp, err := time.Parse(time.RFC3339Nano, turn.Created)
		if err != nil || turn.Plugin != "fortune" || turn.Draw < 1 || turn.Draw > 100 || turn.TaskID == "" || playActor(st, turn.Message) != actor || stamp.In(time.FixedZone("北京时间", 8*3600)).Format("2006-01-02") != day {
			continue
		}
		if len(turn.Deliveries) == 1 && turn.Deliveries[0].Text == text {
			return turn.Draw, nil
		}
	}
	return 0, rows.Err()
}

type playTurn struct {
	Created          string              `json:"created"`
	Draw             int                 `json:"draw"`
	Start            bool                `json:"start"`
	Sources          []Message           `json:"sources,omitempty"`
	ID               string              `json:"id"`
	GameID           string              `json:"game_id"`
	Plugin           string              `json:"plugin"`
	Target           string              `json:"target"`
	Epoch            uint64              `json:"epoch"`
	Message          Message             `json:"message"`
	Text             string              `json:"text"`
	Private          bool                `json:"private"`
	Status           string              `json:"status"`
	Error            string              `json:"error"`
	TaskID           string              `json:"task_id"`
	Deliveries       []playroom.Delivery `json:"deliveries,omitempty"`
	Cursor           int                 `json:"cursor"`
	PreparedRevision int                 `json:"prepared_revision"`
	Updated          string              `json:"updated"`
	Retryable        bool                `json:"retryable"`
}
type playMeme struct {
	SourceAvailable bool   `json:"source_available"`
	ID              string `json:"id"`
	Target          string `json:"target"`
	SourceID        string `json:"source_id"`
	SourceHash      string `json:"source_hash"`
	Text            string `json:"text"`
	Explanation     string `json:"explanation"`
	Status          string `json:"status"`
	Created         string `json:"created"`
}

func (s *Server) initPlayroom() error {
	_, err := s.store.db.Exec(`CREATE TABLE IF NOT EXISTS play_games(account TEXT NOT NULL,id TEXT NOT NULL,code TEXT NOT NULL,target TEXT NOT NULL,kind TEXT NOT NULL,status TEXT NOT NULL,revision INTEGER NOT NULL,updated TEXT NOT NULL,body BLOB NOT NULL,PRIMARY KEY(account,id),UNIQUE(account,code));
CREATE UNIQUE INDEX IF NOT EXISTS play_one_live ON play_games(account,target) WHERE status IN ('lobby','playing','paused');
CREATE TABLE IF NOT EXISTS play_turns(seq INTEGER PRIMARY KEY AUTOINCREMENT,account TEXT NOT NULL,id TEXT NOT NULL UNIQUE,game_id TEXT NOT NULL,target TEXT NOT NULL,source_target TEXT NOT NULL,message_id TEXT NOT NULL,status TEXT NOT NULL,body BLOB NOT NULL,UNIQUE(account,source_target,message_id));
CREATE INDEX IF NOT EXISTS play_queue ON play_turns(account,target,status,seq);
CREATE TABLE IF NOT EXISTS play_memes(account TEXT NOT NULL,id TEXT NOT NULL,target TEXT NOT NULL,source_id TEXT NOT NULL,status TEXT NOT NULL,body BLOB NOT NULL,PRIMARY KEY(account,id),UNIQUE(account,target,source_id));
CREATE TABLE IF NOT EXISTS play_daily(account TEXT NOT NULL,target TEXT NOT NULL,actor TEXT NOT NULL,day TEXT NOT NULL,body BLOB NOT NULL,PRIMARY KEY(account,target,actor,day));`)
	if err != nil {
		return err
	}
	rows, err := s.store.db.Query("SELECT account,id,body FROM play_turns WHERE status IN ('queued','running','prepared','sending')")
	if err != nil {
		return err
	}
	type stale struct {
		a string
		t playTurn
	}
	var pending []stale
	for rows.Next() {
		var a, id string
		var b []byte
		var t playTurn
		if err = rows.Scan(&a, &id, &b); err != nil {
			break
		}
		if err = s.store.openMemory(a, "play-turn:"+id, b, &t); err != nil {
			break
		}
		if t.Status == "sending" {
			t.Status = "unknown"
			t.Error = "服务在发送时中断，须先核对微信结果，不自动重发"
			t.Retryable = false
		} else {
			t.Status = "failed"
			t.Error = "服务已重启，可人工重试本回合"
			t.Retryable = true
		}
		pending = append(pending, stale{a, t})
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, x := range pending {
		if err = s.savePlayTurn(x.a, x.t); err != nil {
			return err
		}
	}
	return nil
}
func (s *Server) playGame(account, id string) (playroom.Game, error) {
	var g playroom.Game
	var b []byte
	err := s.store.db.QueryRow("SELECT body FROM play_games WHERE account=? AND id=?", account, id).Scan(&b)
	if err == nil {
		err = s.store.openMemory(account, "play-game:"+id, b, &g)
	}
	return g, err
}
func (s *Server) playByCode(account, code string) (playroom.Game, error) {
	var id string
	err := s.store.db.QueryRow("SELECT id FROM play_games WHERE account=? AND code=?", account, code).Scan(&id)
	if err != nil {
		return playroom.Game{}, err
	}
	return s.playGame(account, id)
}
func (s *Server) livePlay(account, target string) (playroom.Game, error) {
	var id string
	err := s.store.db.QueryRow("SELECT id FROM play_games WHERE account=? AND target=? AND status IN ('lobby','playing','paused')", account, target).Scan(&id)
	if err != nil {
		return playroom.Game{}, err
	}
	return s.playGame(account, id)
}
func (s *Server) playGames(account, target, kind string) ([]playroom.Game, error) {
	rows, e := s.store.db.Query("SELECT id,body FROM play_games WHERE account=? AND (?='' OR target=?) AND (?='' OR kind=?) ORDER BY updated DESC LIMIT 100", account, target, target, kind, kind)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []playroom.Game{}
	for rows.Next() {
		var id string
		var b []byte
		var g playroom.Game
		if e = rows.Scan(&id, &b); e != nil {
			return nil, e
		}
		if e = s.store.openMemory(account, "play-game:"+id, b, &g); e != nil {
			return nil, e
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (s *Server) writePlayGame(db soupDB, account string, g *playroom.Game) error {
	previous := g.Revision
	next := *g
	next.Revision++
	next.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	b, e := s.store.sealMemory(account, "play-game:"+g.ID, next)
	if e != nil {
		return e
	}
	var r sql.Result
	if previous == 0 {
		r, e = db.Exec("INSERT INTO play_games VALUES(?,?,?,?,?,?,?,?,?)", account, g.ID, g.Code, g.Target, g.Kind, g.Status, next.Revision, next.Updated, b)
	} else {
		r, e = db.Exec("UPDATE play_games SET status=?,revision=?,updated=?,body=? WHERE account=? AND id=? AND revision=?", g.Status, next.Revision, next.Updated, b, account, g.ID, previous)
	}
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("对局版本已变化，请刷新")
	}
	*g = next
	return nil
}
func (s *Server) playTurn(account, id string) (playTurn, error) {
	var t playTurn
	var b []byte
	e := s.store.db.QueryRow("SELECT body FROM play_turns WHERE account=? AND id=?", account, id).Scan(&b)
	if e == nil {
		e = s.store.openMemory(account, "play-turn:"+id, b, &t)
	}
	return t, e
}
func (s *Server) savePlayTurn(account string, t playTurn) error {
	t.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	b, e := s.store.sealMemory(account, "play-turn:"+t.ID, t)
	if e != nil {
		return e
	}
	_, e = s.store.db.Exec("UPDATE play_turns SET game_id=?,status=?,body=? WHERE account=? AND id=?", t.GameID, t.Status, b, account, t.ID)
	s.notify()
	return e
}
func (s *Server) playTurns(account, target string) ([]playTurn, error) {
	rows, e := s.store.db.Query("SELECT id,body FROM play_turns WHERE account=? AND (?='' OR target=?) ORDER BY CASE WHEN status IN ('failed','unknown') THEN 0 ELSE 1 END, seq DESC LIMIT 100", account, target, target)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []playTurn{}
	for rows.Next() {
		var id string
		var b []byte
		var t playTurn
		if e = rows.Scan(&id, &b); e != nil {
			return nil, e
		}
		if e = s.store.openMemory(account, "play-turn:"+id, b, &t); e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Server) preparePlay(account string, g *playroom.Game, t *playTurn, out []playroom.Delivery) error {
	privateChemistry := g != nil && g.Kind == "chemistry" && !memoryGroup(s.store.snapshot(), g.Target)
	tx, e := s.store.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if g != nil {
		if e = s.writePlayGame(tx, account, g); e != nil {
			return e
		}
		t.GameID = g.ID
		t.PreparedRevision = g.Revision
	}
	if privateChemistry {
		var fanout []playroom.Delivery
		for _, d := range out {
			if d.Private {
				fanout = append(fanout, d)
			} else {
				for _, p := range g.Players {
					fanout = append(fanout, playroom.Secret(p.ID, d.Text))
				}
			}
		}
		out = fanout
	}
	var split []playroom.Delivery
	for _, d := range out {
		runes := []rune(d.Text)
		for len(runes) > 3000 {
			part := d
			part.Text = string(runes[:3000])
			split = append(split, part)
			runes = runes[3000:]
		}
		d.Text = string(runes)
		split = append(split, d)
	}
	t.Deliveries = split
	t.Status = "prepared"
	b, e := s.store.sealMemory(account, "play-turn:"+t.ID, t)
	if e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE play_turns SET game_id=?,status=?,body=? WHERE account=? AND id=?", t.GameID, t.Status, b, account, t.ID); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Server) playMemes(account, target string) ([]playMeme, error) {
	rows, e := s.store.db.Query("SELECT id,body FROM play_memes WHERE account=? AND (?='' OR target=?) ORDER BY rowid DESC LIMIT 200", account, target, target)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []playMeme{}
	for rows.Next() {
		var id string
		var b []byte
		var m playMeme
		if e = rows.Scan(&id, &b); e != nil {
			return nil, e
		}
		if e = s.store.openMemory(account, "play-meme:"+id, b, &m); e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Server) savePlayMeme(account string, m playMeme) error {
	b, e := s.store.sealMemory(account, "play-meme:"+m.ID, m)
	if e != nil {
		return e
	}
	_, e = s.store.db.Exec("INSERT INTO play_memes VALUES(?,?,?,?,?,?) ON CONFLICT(account,id) DO UPDATE SET status=excluded.status,body=excluded.body", account, m.ID, m.Target, m.SourceID, m.Status, b)
	return e
}

// 管理员结束对局时，取消未完成回合，解除失败/未知状态对新局的阻塞；不重发任何消息。
func (s *Server) endPlayGame(account string, g *playroom.Game) error {
	turns, e := s.pendingPlayTurns(account, g.ID)
	if e != nil {
		return e
	}
	tx, e := s.store.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	g.Status = "ended"
	if e = s.writePlayGame(tx, account, g); e != nil {
		return e
	}
	for _, t := range turns {
		if t.GameID != g.ID || t.Status == "completed" || t.Status == "cancelled" {
			continue
		}
		t.Status = "cancelled"
		t.Retryable = false
		t.Error = "管理员结束对局，未发送内容已取消"
		b, e := s.store.sealMemory(account, "play-turn:"+t.ID, t)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("UPDATE play_turns SET status='cancelled',body=? WHERE account=? AND id=?", b, account, t.ID); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (s *Server) pendingPlayTurns(account, game string) ([]playTurn, error) {
	rows, e := s.store.db.Query("SELECT id,body FROM play_turns WHERE account=? AND game_id=? AND status NOT IN ('completed','cancelled') ORDER BY seq", account, game)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []playTurn{}
	for rows.Next() {
		var id string
		var b []byte
		var t playTurn
		if e = rows.Scan(&id, &b); e != nil {
			return nil, e
		}
		if e = s.store.openMemory(account, "play-turn:"+id, b, &t); e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// 暂停/恢复只改变控制状态；已提交的待发账本随同绑定新的版本，不重新计算游戏效果。
func (s *Server) controlPlayGame(account string, g *playroom.Game) error {
	turns, e := s.pendingPlayTurns(account, g.ID)
	if e != nil {
		return e
	}
	previous := g.Revision
	tx, e := s.store.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.writePlayGame(tx, account, g); e != nil {
		return e
	}
	for _, t := range turns {
		if t.PreparedRevision != previous {
			continue
		}
		t.PreparedRevision = g.Revision
		b, e := s.store.sealMemory(account, "play-turn:"+t.ID, t)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("UPDATE play_turns SET body=? WHERE account=? AND id=?", b, account, t.ID); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (s *Server) reservePlayGame(account string, g *playroom.Game) error {
	s.gameStartMu.Lock()
	defer s.gameStartMu.Unlock()
	if _, e := s.liveSoup(account, g.Target); e == nil {
		return fmt.Errorf("本会话已有海龟汤，请先结束")
	} else if e != sql.ErrNoRows {
		return e
	}
	if _, e := s.livePlay(account, g.Target); e == nil {
		return fmt.Errorf("本会话已有活动互动局，请先结束")
	} else if e != sql.ErrNoRows {
		return e
	}
	return s.writePlayGame(s.store.db, account, g)
}
func (s *Server) reserveSoupGame(account string, g *soupGame) error {
	s.gameStartMu.Lock()
	defer s.gameStartMu.Unlock()
	if _, e := s.livePlay(account, g.Target); e == nil {
		return fmt.Errorf("本会话已有互动局，请先结束")
	} else if e != sql.ErrNoRows {
		return e
	}
	return s.saveSoupGame(account, g)
}
