// 主要功能：保存按入库顺序排列的加密消息事件，供单会话记忆扫描使用。
package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

func (s *store) initMemoryArchive() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS memory_archive_events (seq INTEGER PRIMARY KEY AUTOINCREMENT,account TEXT NOT NULL,target TEXT NOT NULL,message_id TEXT NOT NULL,event_hash TEXT NOT NULL,kind TEXT NOT NULL,body BLOB NOT NULL,created_at TEXT NOT NULL,UNIQUE(account,target,message_id,event_hash)); CREATE INDEX IF NOT EXISTS memory_archive_target ON memory_archive_events(account,target,seq); CREATE TABLE IF NOT EXISTS memory_revocations (account TEXT NOT NULL,target TEXT NOT NULL,message_id TEXT NOT NULL,event_id TEXT NOT NULL,PRIMARY KEY(account,target,message_id)); CREATE TABLE IF NOT EXISTS memory_scans (id TEXT PRIMARY KEY,account TEXT NOT NULL,target TEXT NOT NULL,status TEXT NOT NULL,body BLOB NOT NULL,updated_at TEXT NOT NULL); CREATE INDEX IF NOT EXISTS memory_scans_target ON memory_scans(account,target,updated_at);`)
	if err != nil {
		return err
	}
	if _, err = s.db.Exec(`CREATE TABLE IF NOT EXISTS memory_deleted_sources (account TEXT NOT NULL,target TEXT NOT NULL,message_id TEXT NOT NULL,PRIMARY KEY(account,target,message_id))`); err != nil {
		return err
	}
	// 进程重启后原 worker 不再存在；保留 cursor 和固定上界，可由管理员续扫。
	rows, err := s.db.Query("SELECT id,account,body FROM memory_scans WHERE status='running'")
	if err != nil {
		return err
	}
	type stale struct {
		id, account string
		scan        memoryScan
	}
	var scans []stale
	for rows.Next() {
		var x stale
		var raw []byte
		if err = rows.Scan(&x.id, &x.account, &raw); err != nil {
			rows.Close()
			return err
		}
		if err = s.openMemory(x.account, "scan:"+x.id, raw, &x.scan); err != nil {
			rows.Close()
			return err
		}
		scans = append(scans, x)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, x := range scans {
		x.scan.Status = "failed"
		x.scan.Error = "服务已重启，可继续扫描"
		x.scan.TaskID = ""
		if err = s.saveMemoryScanLocked(x.account, x.scan); err != nil {
			return err
		}
	}
	return nil
}

func (s *store) recordMemoryArchiveLocked(account string, m Message, legacy bool) error {
	if account == "" || account != s.state.Account.ID {
		return fmt.Errorf("微信账号已切换或未就绪")
	}
	if m.ID == "" || m.Target == "" {
		return nil
	}
	// 不复制协议回执中的票据；结构化互动、@、引用和来源正文随密文保存。
	m.RevokeReceipt = nil
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	body, err := s.sealMemory(account, "archive:"+m.Target+":"+m.ID+":"+hash, m)
	if err != nil {
		return err
	}
	kind := "event"
	if legacy {
		kind = "legacy_snapshot"
	}
	_, err = s.db.Exec("INSERT OR IGNORE INTO memory_archive_events(account,target,message_id,event_hash,kind,body,created_at) VALUES(?,?,?,?,?,?,?)", account, m.Target, m.ID, hash, kind, body, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Server) recordMemoryArchive(account string, m Message) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	return s.store.recordMemoryArchiveLocked(account, m, false)
}

// 首次手动扫描时补索引，不生成记忆，也不将历史快照冒充原始入站事件。
func (s *store) backfillMemoryArchiveLocked(account, target string) error {
	for _, m := range s.state.Messages {
		if m.Target != target {
			continue
		}
		var n int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM memory_archive_events WHERE account=? AND target=? AND message_id=?", account, target, m.ID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if err := s.recordMemoryArchiveLocked(account, m, true); err != nil {
				return err
			}
		}
	}
	return nil
}

type memoryArchiveEvent struct {
	Seq     int64
	Message Message
}

func (s *store) memoryArchiveBatchLocked(account, target string, cursor, upper int64) ([]memoryArchiveEvent, error) {
	rows, err := s.db.Query(`SELECT e.seq,e.message_id,e.event_hash,e.body FROM memory_archive_events e WHERE e.account=? AND e.target=? AND e.seq>? AND e.seq<=? AND NOT EXISTS (SELECT 1 FROM memory_archive_events newer WHERE newer.account=e.account AND newer.target=e.target AND newer.message_id=e.message_id AND newer.seq>e.seq AND newer.seq<=?) ORDER BY e.seq LIMIT 20`, account, target, cursor, upper, upper)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []memoryArchiveEvent{}
	for rows.Next() {
		var e memoryArchiveEvent
		var id, hash string
		var raw []byte
		if err = rows.Scan(&e.Seq, &id, &hash, &raw); err != nil {
			return nil, err
		}
		if err = s.openMemory(account, "archive:"+target+":"+id+":"+hash, raw, &e.Message); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func currentMemorySource(st State, m Message) bool {
	if !memoryScanEligible(st, m) {
		return false
	}
	return memorySourceValid(st, memorySource(m))
}

func (s *store) deleteMemoryScanResult(account, id string) error {
	_, err := s.db.Exec("DELETE FROM secrets WHERE id=?", "memory-scan-result:"+account+":"+id)
	return err
}

// 重叠最近一批作为抽取证据，检查点仍只统计本批新事件。
func (s *store) memoryArchiveOverlapLocked(account, target string, cursor int64) ([]Message, error) {
	rows, err := s.db.Query(`SELECT e.message_id,e.event_hash,e.body FROM memory_archive_events e WHERE e.account=? AND e.target=? AND e.seq<=? AND NOT EXISTS (SELECT 1 FROM memory_archive_events n WHERE n.account=e.account AND n.target=e.target AND n.message_id=e.message_id AND n.seq>e.seq AND n.seq<=?) ORDER BY e.seq DESC LIMIT 20`, account, target, cursor, cursor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var id, hash string
		var body []byte
		var m Message
		if err = rows.Scan(&id, &hash, &body); err != nil {
			return nil, err
		}
		if err = s.openMemory(account, "archive:"+target+":"+id+":"+hash, body, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// 撤回 tombstone 由可信事件入口写入，不能根据普通正文推断。
func (s *store) recordMemoryRevokeLocked(account, target, messageID, eventID string) error {
	if account == "" || account != s.state.Account.ID || target == "" || messageID == "" {
		return fmt.Errorf("撤回来源或账号无效")
	}
	_, err := s.db.Exec("INSERT INTO memory_revocations(account,target,message_id,event_id) VALUES(?,?,?,?) ON CONFLICT(account,target,message_id) DO NOTHING", account, target, messageID, eventID)
	return err
}
func (s *store) memoryRevokedLocked(account, target, messageID string) (bool, error) {
	if account == "" || account != s.state.Account.ID {
		return false, fmt.Errorf("微信账号已切换或未就绪")
	}
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM memory_revocations WHERE account=? AND target=? AND message_id=?", account, target, messageID).Scan(&count)
	return count > 0, err
}

func (s *store) memoryDeletedLocked(account, target, messageID string) (bool, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM memory_deleted_sources WHERE account=? AND target=? AND message_id=?", account, target, messageID).Scan(&count)
	return count > 0, err
}
