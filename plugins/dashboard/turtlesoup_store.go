// 海龟汤题库、对局和回合独立加密保存；重启不自动重发未知回执。
package dashboard

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/we-clay/we-clay/plugins/turtlesoup"
)

type soupGame struct {
	ID            string            `json:"id"`
	Target        string            `json:"target"`
	Owner         string            `json:"owner"`
	Theme         string            `json:"theme"`
	Status        string            `json:"status"`
	Source        string            `json:"source"`
	Puzzle        turtlesoup.Puzzle `json:"puzzle"`
	Hints         int               `json:"hints"`
	Questions     int               `json:"questions"`
	Known         []string          `json:"known"`
	Created       string            `json:"created"`
	Updated       string            `json:"updated"`
	Revision      int               `json:"revision"`
	ChangePending bool              `json:"change_pending"`
}
type soupTurn struct {
	Epoch            uint64              `json:"epoch"`
	ID               string              `json:"id"`
	GameID           string              `json:"game_id"`
	Message          Message             `json:"message"`
	Command          string              `json:"command"`
	Theme            string              `json:"theme"`
	Status           string              `json:"status"`
	TaskID           string              `json:"task_id"`
	Output           string              `json:"output"`
	Error            string              `json:"error"`
	Retryable        bool                `json:"retryable"`
	Verdict          *turtlesoup.Verdict `json:"verdict,omitempty"`
	Updated          string              `json:"updated"`
	PreparedRevision int                 `json:"prepared_revision,omitempty"`
	NoticeAttempted  bool                `json:"notice_attempted,omitempty"`
}

func (s *Server) initSoup() error {
	_, err := s.store.db.Exec(`CREATE TABLE IF NOT EXISTS soup_puzzles(account TEXT NOT NULL,id TEXT NOT NULL,body BLOB NOT NULL,PRIMARY KEY(account,id));
CREATE TABLE IF NOT EXISTS soup_games(account TEXT NOT NULL,id TEXT NOT NULL,target TEXT NOT NULL,status TEXT NOT NULL,updated TEXT NOT NULL,revision INTEGER NOT NULL,body BLOB NOT NULL,PRIMARY KEY(account,id));
CREATE UNIQUE INDEX IF NOT EXISTS soup_one_live ON soup_games(account,target) WHERE status IN ('generating','playing','paused','failed');
CREATE INDEX IF NOT EXISTS soup_games_recent ON soup_games(account,updated);
CREATE TABLE IF NOT EXISTS soup_turns(seq INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT NOT NULL UNIQUE,account TEXT NOT NULL,target TEXT NOT NULL,message_id TEXT NOT NULL,status TEXT NOT NULL,updated TEXT NOT NULL,body BLOB NOT NULL,UNIQUE(account,target,message_id));
CREATE INDEX IF NOT EXISTS soup_turn_queue ON soup_turns(account,target,status,seq);`)
	if err == nil {
		_, err = s.store.db.Exec(`CREATE TABLE IF NOT EXISTS soup_played(account TEXT NOT NULL,target TEXT NOT NULL,puzzle_id TEXT NOT NULL,PRIMARY KEY(account,target,puzzle_id))`)
	}
	if err != nil {
		return err
	}
	rows, err := s.store.db.Query("SELECT account,id,body FROM soup_turns WHERE status IN ('queued','running','prepared','sending')")
	if err != nil {
		return err
	}
	type stale struct {
		account string
		turn    soupTurn
	}
	pending := []stale{}
	for rows.Next() {
		var account, id string
		var raw []byte
		var t soupTurn
		if err = rows.Scan(&account, &id, &raw); err != nil {
			break
		}
		if err = s.store.openMemory(account, "soup-turn:"+id, raw, &t); err != nil {
			break
		}
		if t.Status == "sending" {
			t.Status = "unknown"
			t.Retryable = false
			t.Error = "服务在发送期间中断，请核对微信；不会自动重发"
		} else {
			t.Status = "failed"
			t.Retryable = true
			t.Error = "服务已重启，可重试这一回合"
		}
		pending = append(pending, stale{account, t})
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, x := range pending {
		if err = s.saveSoupTurn(x.account, x.turn); err != nil {
			return err
		}
	}
	rows, err = s.store.db.Query("SELECT account,id,body FROM soup_games WHERE status='generating'")
	if err != nil {
		return err
	}
	type staleGame struct {
		account string
		game    soupGame
	}
	games := []staleGame{}
	for rows.Next() {
		var account, id string
		var raw []byte
		var g soupGame
		if err = rows.Scan(&account, &id, &raw); err != nil {
			break
		}
		if err = s.store.openMemory(account, "soup-game:"+id, raw, &g); err != nil {
			break
		}
		g.Status = "failed"
		games = append(games, staleGame{account, g})
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, x := range games {
		if err = s.saveSoupGame(x.account, &x.game); err != nil {
			return err
		}
	}
	return nil
}
func (s *Server) soupPuzzles(account string) ([]turtlesoup.Puzzle, error) {
	rows, err := s.store.db.Query("SELECT id,body FROM soup_puzzles WHERE account=? ORDER BY id", account)
	if err != nil {
		return nil, err
	}
	var out []turtlesoup.Puzzle
	for rows.Next() {
		var id string
		var body []byte
		var p turtlesoup.Puzzle
		if err = rows.Scan(&id, &body); err != nil {
			break
		}
		if err = s.store.openMemory(account, "soup-puzzle:"+id, body, &p); err != nil {
			break
		}
		out = append(out, p)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	// 内置题也可编辑停用；没有覆盖项时提供默认版本。
	for _, p := range turtlesoup.Defaults {
		found := false
		for _, old := range out {
			if old.ID == p.ID {
				found = true
				break
			}
		}
		if !found {
			out = append(out, p)
		}
	}
	return out, nil
}
func (s *Server) saveSoupPuzzle(account string, p turtlesoup.Puzzle) error {
	if err := turtlesoup.Validate(p); err != nil {
		return err
	}
	if p.ID == "" {
		return fmt.Errorf("题目 ID 不能为空")
	}
	body, err := s.store.sealMemory(account, "soup-puzzle:"+p.ID, p)
	if err != nil {
		return err
	}
	_, err = s.store.db.Exec("INSERT INTO soup_puzzles VALUES(?,?,?) ON CONFLICT(account,id) DO UPDATE SET body=excluded.body", account, p.ID, body)
	return err
}
func (s *Server) soupGames(account, target string) ([]soupGame, error) {
	rows, err := s.store.db.Query("SELECT id,body FROM soup_games WHERE account=? AND (?='' OR target=?) ORDER BY updated DESC LIMIT 100", account, target, target)
	if err != nil {
		return nil, err
	}
	out := []soupGame{}
	for rows.Next() {
		var id string
		var raw []byte
		var g soupGame
		if err = rows.Scan(&id, &raw); err != nil {
			break
		}
		if err = s.store.openMemory(account, "soup-game:"+id, raw, &g); err != nil {
			break
		}
		out = append(out, g)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	return out, err
}
func (s *Server) soupGame(account, id string) (soupGame, error) {
	var g soupGame
	var raw []byte
	err := s.store.db.QueryRow("SELECT body FROM soup_games WHERE account=? AND id=?", account, id).Scan(&raw)
	if err == nil {
		err = s.store.openMemory(account, "soup-game:"+id, raw, &g)
	}
	return g, err
}
func (s *Server) liveSoup(account, target string) (soupGame, error) {
	var id string
	err := s.store.db.QueryRow("SELECT id FROM soup_games WHERE account=? AND target=? AND status IN ('generating','playing','paused','failed')", account, target).Scan(&id)
	if err != nil {
		return soupGame{}, err
	}
	return s.soupGame(account, id)
}
func (s *Server) saveSoupGame(account string, g *soupGame) error {
	return s.writeSoupGame(s.store.db, account, g)
}

type soupDB interface {
	Exec(string, ...any) (sql.Result, error)
}

func (s *Server) writeSoupGame(db soupDB, account string, g *soupGame) error {
	previous := g.Revision
	g.Revision++
	g.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	body, err := s.store.sealMemory(account, "soup-game:"+g.ID, g)
	if err != nil {
		return err
	}
	var res sql.Result
	if previous == 0 {
		res, err = db.Exec("INSERT INTO soup_games VALUES(?,?,?,?,?,?,?)", account, g.ID, g.Target, g.Status, g.Updated, g.Revision, body)
	} else {
		res, err = db.Exec("UPDATE soup_games SET status=?,updated=?,revision=?,body=? WHERE account=? AND id=? AND revision=?", g.Status, g.Updated, g.Revision, body, account, g.ID, previous)
	}
	if err != nil {
		g.Revision = previous
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		err = fmt.Errorf("对局已变化，请刷新后重试")
	}
	return err
}

// 对局变更与待发正文同一事务提交；崩溃后重试不重复扣提示或重新出题。
func (s *Server) prepareSoupEffect(account string, g *soupGame, t *soupTurn, text string) error {
	tx, err := s.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.writeSoupGame(tx, account, g); err != nil {
		return err
	}
	if g.Source == "bank" && g.Puzzle.ID != "" {
		if _, err = tx.Exec("INSERT OR IGNORE INTO soup_played VALUES(?,?,?)", account, g.Target, g.Puzzle.ID); err != nil {
			return err
		}
	}
	next := *t
	next.Status = "prepared"
	next.Output = text
	next.PreparedRevision = g.Revision
	raw, err := s.store.sealMemory(account, "soup-turn:"+next.ID, next)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE soup_turns SET status='prepared',body=? WHERE account=? AND id=?", raw, account, next.ID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	*t = next
	return nil
}
func (s *Server) soupTurn(account, id string) (soupTurn, error) {
	var t soupTurn
	var raw []byte
	err := s.store.db.QueryRow("SELECT body FROM soup_turns WHERE account=? AND id=?", account, id).Scan(&raw)
	if err == nil {
		err = s.store.openMemory(account, "soup-turn:"+id, raw, &t)
	}
	return t, err
}
func (s *Server) saveSoupTurn(account string, t soupTurn) error {
	t.Updated = time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := s.store.sealMemory(account, "soup-turn:"+t.ID, t)
	if err != nil {
		return err
	}
	_, err = s.store.db.Exec("UPDATE soup_turns SET status=?,updated=?,body=? WHERE account=? AND id=?", t.Status, t.Updated, raw, account, t.ID)
	s.notify()
	return err
}
func (s *Server) soupTurns(account, target string) ([]soupTurn, error) {
	rows, err := s.store.db.Query("SELECT id,body FROM soup_turns WHERE account=? AND (?='' OR target=?) ORDER BY seq DESC LIMIT 100", account, target, target)
	if err != nil {
		return nil, err
	}
	out := []soupTurn{}
	for rows.Next() {
		var id string
		var raw []byte
		var t soupTurn
		if err = rows.Scan(&id, &raw); err != nil {
			break
		}
		if err = s.store.openMemory(account, "soup-turn:"+id, raw, &t); err != nil {
			break
		}
		out = append(out, t)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	return out, err
}
