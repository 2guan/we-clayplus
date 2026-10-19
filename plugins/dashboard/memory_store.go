// 主要功能：独立保存加密记忆、来源及变更记录，按账号和会话检索。
package dashboard

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

type MemorySource struct {
	Target    string `json:"target"`
	MessageID string `json:"message_id"`
	SenderID  string `json:"sender_id"`
	Hash      string `json:"hash"`
}
type MemoryItem struct {
	ID          string         `json:"id"`
	Scope       string         `json:"scope"`
	Subject     string         `json:"subject"`
	Target      string         `json:"target"`
	Category    string         `json:"category"`
	Key         string         `json:"key"`
	Content     string         `json:"content"`
	Status      string         `json:"status"`
	Sensitivity string         `json:"sensitivity"`
	Consent     bool           `json:"consent"`
	AllowModel  bool           `json:"allow_model"`
	Origin      string         `json:"origin"`
	CreatedAt   string         `json:"created_at"`
	ConfirmedAt string         `json:"confirmed_at"`
	ConfirmedBy string         `json:"confirmed_by"`
	ExpiresAt   string         `json:"expires_at"`
	Version     int            `json:"version"`
	Sources     []MemorySource `json:"sources"`
}
type MemoryFilter struct{ Target, Subject, Scope, Status string }

func (s *store) initMemory() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS memory_items (account TEXT NOT NULL,id TEXT NOT NULL,body BLOB NOT NULL,fingerprint TEXT NOT NULL,status TEXT NOT NULL,updated_at TEXT NOT NULL,PRIMARY KEY(account,id)); CREATE INDEX IF NOT EXISTS memory_items_account ON memory_items(account,status); CREATE INDEX IF NOT EXISTS memory_items_fingerprint ON memory_items(account,fingerprint); CREATE TABLE IF NOT EXISTS memory_sources (account TEXT NOT NULL,memory_id TEXT NOT NULL,target TEXT NOT NULL,message_id TEXT NOT NULL,sender_id TEXT NOT NULL,hash TEXT NOT NULL,PRIMARY KEY(account,memory_id,target,message_id)); CREATE INDEX IF NOT EXISTS memory_sources_message ON memory_sources(account,target,message_id); CREATE TABLE IF NOT EXISTS memory_changes (id TEXT PRIMARY KEY,account TEXT NOT NULL,item_id TEXT NOT NULL,time TEXT NOT NULL,body BLOB NOT NULL);`)
	return err
}
func (s *store) sealMemory(account, id string, v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, raw, []byte("memory:"+account+":"+id)), nil
}
func (s *store) openMemory(account, id string, raw []byte, v any) error {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	if len(raw) < aead.NonceSize() {
		return fmt.Errorf("记忆数据无效")
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte("memory:"+account+":"+id))
	if err != nil {
		return fmt.Errorf("记忆数据无法解密")
	}
	return json.Unmarshal(plain, v)
}
func memorySource(m Message) MemorySource {
	raw, _ := json.Marshal(struct {
		ID, Target, Sender, Kind, Content, Time string
		Self                                    bool
	}{m.ID, m.Target, m.SenderID, m.Kind, m.Content, m.Time, m.Self})
	hash := sha256.Sum256(raw)
	return MemorySource{Target: m.Target, MessageID: m.ID, SenderID: m.SenderID, Hash: hex.EncodeToString(hash[:])}
}
func memoryMessageAvailable(m Message) bool {
	return m.ID != "" && m.Target != "" && m.SenderID != "" && m.Content != "" && !strings.HasPrefix(m.Status, "revoked") && m.Kind != "revoke" && m.Kind != "system" && m.Status != "failed" && m.Status != "unknown" && m.Status != "deleted"
}
func memorySourceValid(st State, src MemorySource) bool {
	if src.Hash == "" || src.MessageID == "" || src.Target == "" || src.SenderID == "" {
		return false
	}
	for _, m := range st.Messages {
		if m.ID == src.MessageID && m.Target == src.Target {
			return memoryMessageAvailable(m) && !m.Self && m.Source != "ai" && memorySource(m) == src
		}
	}
	return false
}
func memoryGroup(st State, target string) bool {
	if strings.HasSuffix(target, "@chatroom") || strings.HasSuffix(target, "@im.chatroom") {
		return true
	}
	for _, c := range st.Contacts {
		if c.ID == target {
			return c.Kind == "group"
		}
	}
	return false
}
func memoryTargetKnown(st State, target string) bool {
	for _, c := range st.Contacts {
		if c.ID == target {
			return true
		}
	}
	for _, m := range st.Messages {
		if m.Target == target {
			return true
		}
	}
	return false
}
func memoryExpired(m MemoryItem) bool {
	if m.ExpiresAt == "" {
		return false
	}
	expiry, err := time.Parse(time.RFC3339, m.ExpiresAt)
	return err != nil || !expiry.After(time.Now())
}
func memorySourcesValid(st State, m MemoryItem) bool {
	if len(m.Sources) == 0 {
		return m.Origin == "manual"
	}
	for _, src := range m.Sources {
		if !memorySourceValid(st, src) {
			return false
		}
	}
	return true
}
func memoryFingerprint(m MemoryItem) string {
	content, category := m.Content, m.Category
	if m.Origin == "scan" || m.Origin == "explicit" {
		content = ""
		category = ""
	}
	sources := append([]MemorySource(nil), m.Sources...)
	sort.Slice(sources, func(i, j int) bool {
		a, b := sources[i], sources[j]
		return a.Target+"\x00"+a.MessageID+"\x00"+a.SenderID+"\x00"+a.Hash < b.Target+"\x00"+b.MessageID+"\x00"+b.SenderID+"\x00"+b.Hash
	})
	raw, _ := json.Marshal(struct {
		Scope, Subject, Target, Category, Key, Content, Origin string
		Sources                                                []MemorySource
	}{m.Scope, m.Subject, m.Target, category, m.Key, content, m.Origin, sources})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func memorySameKey(a, b MemoryItem) bool {
	return a.Scope == b.Scope && a.Subject == b.Subject && a.Target == b.Target && a.Category == b.Category && a.Key == b.Key
}
func memoryTerminal(status string) bool {
	return status == "rejected" || status == "expired" || status == "superseded" || status == "deleted"
}
func (s *store) memoriesLocked(account string) ([]MemoryItem, error) {
	rows, err := s.db.Query("SELECT id,body FROM memory_items WHERE account=? ORDER BY updated_at DESC,id", account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MemoryItem{}
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var m MemoryItem
		if err = s.openMemory(account, id, raw, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Server) memoryList(account string, f MemoryFilter) ([]MemoryItem, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if account == "" || s.store.state.Account.ID != account {
		return nil, fmt.Errorf("微信账号已切换或未就绪")
	}
	list, err := s.store.memoriesLocked(account)
	if err != nil {
		return nil, err
	}
	out := []MemoryItem{}
	for _, m := range list {
		if f.Target != "" && m.Target != f.Target || f.Subject != "" && m.Subject != f.Subject || f.Scope != "" && m.Scope != f.Scope {
			continue
		}
		if m.Status == "confirmed" && memoryExpired(m) {
			m.Status = "expired"
		}
		if f.Status != "" && m.Status != f.Status {
			continue
		}
		if m.Status == "deleted" && f.Status == "" {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}
func (s *store) writeMemoryTx(tx *sql.Tx, account string, m MemoryItem, before *MemoryItem, actor, action, fingerprint string) error {
	raw, err := s.sealMemory(account, m.ID, m)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO memory_items(account,id,body,fingerprint,status,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(account,id) DO UPDATE SET body=excluded.body,fingerprint=excluded.fingerprint,status=excluded.status,updated_at=excluded.updated_at", account, m.ID, raw, fingerprint, m.Status, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM memory_sources WHERE account=? AND memory_id=?", account, m.ID); err != nil {
		return err
	}
	for _, src := range m.Sources {
		if _, err = tx.Exec("INSERT INTO memory_sources VALUES(?,?,?,?,?,?)", account, m.ID, src.Target, src.MessageID, src.SenderID, src.Hash); err != nil {
			return err
		}
	}
	id := randomID()
	audit, err := s.sealMemory(account, id, struct {
		Actor  string      `json:"actor"`
		Action string      `json:"action"`
		Before *MemoryItem `json:"before,omitempty"`
		After  MemoryItem  `json:"after"`
	}{actor, action, before, m})
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO memory_changes VALUES(?,?,?,?,?)", id, account, m.ID, time.Now().UTC().Format(time.RFC3339Nano), audit)
	return err
}
func memoryExplicitPrefix(content string) bool {
	content = strings.TrimSpace(content)
	for _, p := range []string{"记住：", "记住，", "请记住：", "请记住，"} {
		if strings.HasPrefix(content, p) {
			return true
		}
	}
	return false
}
func (s *Server) validateMemory(st State, m MemoryItem) error {
	if m.Scope != "person" && m.Scope != "group" && m.Scope != "bot" {
		return fmt.Errorf("记忆范围无效")
	}
	if len(m.Content) > 8000 || strings.TrimSpace(m.Content) == "" || strings.TrimSpace(m.Key) == "" || len(m.Key) > 160 || len(m.Category) > 80 || m.Category == "" {
		return fmt.Errorf("记忆内容、类别或键无效")
	}
	if len(m.Sources) > 32 {
		return fmt.Errorf("来源数量超过限制")
	}
	if m.Scope == "person" && (m.Subject == "" || m.Target == "") {
		return fmt.Errorf("联系人记忆需要主体和可见会话")
	}
	if m.Scope == "group" && (m.Target == "" || !memoryGroup(st, m.Target) || m.Subject != m.Target) {
		return fmt.Errorf("群记忆必须绑定同一群")
	}
	if m.Scope == "bot" && (m.Origin != "manual" || len(m.Sources) > 0) {
		return fmt.Errorf("机器人状态只能由后台设置")
	}
	if m.Target != "" && !memoryTargetKnown(st, m.Target) {
		return fmt.Errorf("记忆会话不存在")
	}
	if m.ExpiresAt != "" {
		if _, err := time.Parse(time.RFC3339, m.ExpiresAt); err != nil {
			return fmt.Errorf("有效期格式无效")
		}
	}
	if m.Sensitivity != "low" && m.Sensitivity != "sensitive" {
		return fmt.Errorf("敏感级别无效")
	}
	if !memorySourcesValid(st, m) {
		return fmt.Errorf("记忆来源不可用或已经变化")
	}
	for _, src := range m.Sources {
		if src.Target != m.Target || m.Scope == "person" && src.SenderID != m.Subject {
			return fmt.Errorf("记忆来源超出主体或可见范围")
		}
	}
	if m.Origin == "explicit" {
		if m.Scope != "person" || m.Subject != m.Target || memoryGroup(st, m.Target) || m.Sensitivity != "low" || len(m.Sources) != 1 || (m.Category != "preference" && m.Category != "name" && m.Category != "communication") {
			return fmt.Errorf("直接记忆仅允许本人私聊低敏偏好")
		}
		for _, msg := range st.Messages {
			if memorySource(msg) == m.Sources[0] && (msg.Self || msg.Kind != "text" || !memoryExplicitPrefix(msg.Content)) {
				return fmt.Errorf("缺少本人明确记忆指令")
			}
		}
	}
	if m.Sensitivity == "sensitive" && !m.Consent {
		return fmt.Errorf("敏感内容须明确同意后才能保存")
	}
	return nil
}
func (s *Server) putMemory(account string, item MemoryItem, actor string, expectedVersion int) (MemoryItem, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	return s.putMemoryLocked(account, item, actor, expectedVersion)
}

// 调用者持有store.mu，用于扫描attempt与候选落库的同一原子检查。
func (s *Server) putMemoryLocked(account string, item MemoryItem, actor string, expectedVersion int) (MemoryItem, error) {
	st := s.store.state
	if account == "" || st.Account.ID != account {
		return MemoryItem{}, fmt.Errorf("微信账号已切换或未就绪")
	}
	if actor == "memory-scan" && !settingFlag(st, "memory.scan_enabled") {
		return MemoryItem{}, fmt.Errorf("记忆扫描已停用")
	}
	if strings.HasPrefix(actor, "explicit-request:") && (!settingFlag(st, "memory.direct_enabled") || st.Overrides[item.Target]["memory_enabled"] != true) {
		return MemoryItem{}, fmt.Errorf("明确记忆写入已停用")
	}
	if item.Origin == "" {
		item.Origin = "manual"
	}
	if item.Origin != "manual" && item.Origin != "scan" && item.Origin != "explicit" {
		return MemoryItem{}, fmt.Errorf("记忆来源类型无效")
	}
	if item.Sensitivity == "" {
		item.Sensitivity = "low"
	}
	if item.Status == "" {
		item.Status = "pending"
	}
	if item.Origin == "scan" && item.Status == "confirmed" {
		item.Status = "pending"
	}
	if item.Status != "pending" && item.Status != "confirmed" {
		return MemoryItem{}, fmt.Errorf("保存仅允许待确认或已确认状态")
	}
	list, err := s.store.memoriesLocked(account)
	if err != nil {
		return MemoryItem{}, err
	}
	var before *MemoryItem
	for _, m := range list {
		if m.ID == item.ID {
			copy := m
			before = &copy
		}
	}
	if item.ID != "" && before == nil {
		return MemoryItem{}, fmt.Errorf("记忆不存在")
	}
	if before != nil {
		if before.Version != expectedVersion {
			return MemoryItem{}, fmt.Errorf("记忆版本已变化，请刷新")
		}
		if before.Status == "deleted" {
			return MemoryItem{}, fmt.Errorf("已删除记忆不可恢复")
		}
		item.CreatedAt = before.CreatedAt
		item.Origin = before.Origin
		item.Version = before.Version + 1
	} else {
		if expectedVersion != 0 {
			return MemoryItem{}, fmt.Errorf("新增记忆版本无效")
		}
		fingerprint := memoryFingerprint(item)
		for _, m := range list {
			if memoryFingerprint(m) == fingerprint {
				return m, nil
			}
		}
		var duplicateID string
		err = s.store.db.QueryRow("SELECT id FROM memory_items WHERE account=? AND fingerprint=? AND status='deleted' LIMIT 1", account, fingerprint).Scan(&duplicateID)
		if err == nil {
			return MemoryItem{}, fmt.Errorf("该来源的记忆已删除")
		}
		if err != sql.ErrNoRows {
			return MemoryItem{}, err
		}
		if item.Origin == "scan" || item.Origin == "explicit" {
			item.AllowModel = false
			item.Consent = false
		}
		item.ID = randomID()
		item.CreatedAt = time.Now().UTC().Format(time.RFC3339)
		item.Version = 1
	}
	if item.Origin == "scan" && item.Status == "confirmed" {
		item.Status = "pending"
	}
	if err := s.validateMemory(st, item); err != nil {
		return MemoryItem{}, err
	}
	item.ConfirmedAt = ""
	item.ConfirmedBy = ""
	if item.Status == "confirmed" {
		item.ConfirmedAt = time.Now().UTC().Format(time.RFC3339)
		item.ConfirmedBy = actor
	}
	tx, err := s.store.db.Begin()
	if err != nil {
		return MemoryItem{}, err
	}
	defer tx.Rollback()
	for _, m := range list {
		if m.ID == item.ID || memoryTerminal(m.Status) || memoryExpired(m) || !memorySourcesValid(st, m) || !memorySameKey(m, item) || m.Content == item.Content {
			continue
		}
		item.Status = "conflicted"
		item.ConfirmedAt = ""
		item.ConfirmedBy = ""
		old := m
		m.Status = "conflicted"
		m.Version++
		if err = s.store.writeMemoryTx(tx, account, m, &old, actor, "conflict", memoryFingerprint(m)); err != nil {
			return MemoryItem{}, err
		}
	}
	if err = s.store.writeMemoryTx(tx, account, item, before, actor, "save", memoryFingerprint(item)); err != nil {
		return MemoryItem{}, err
	}
	if err = tx.Commit(); err != nil {
		return MemoryItem{}, err
	}
	return item, nil
}
func (s *Server) memoryAction(account, id, action string, version int, actor string) (MemoryItem, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	st := s.store.state
	if account == "" || st.Account.ID != account {
		return MemoryItem{}, fmt.Errorf("微信账号已切换或未就绪")
	}
	list, err := s.store.memoriesLocked(account)
	if err != nil {
		return MemoryItem{}, err
	}
	var item MemoryItem
	for _, m := range list {
		if m.ID == id {
			item = m
		}
	}
	if item.ID == "" {
		return item, fmt.Errorf("记忆不存在")
	}
	if item.Version != version {
		return MemoryItem{}, fmt.Errorf("记忆版本已变化，请刷新")
	}
	if item.Status == "deleted" {
		return MemoryItem{}, fmt.Errorf("记忆已删除")
	}
	before := item
	switch action {
	case "confirm":
		if err = s.validateMemory(st, item); err != nil {
			return MemoryItem{}, err
		}
		if item.Sensitivity == "sensitive" && !item.Consent {
			return MemoryItem{}, fmt.Errorf("敏感记忆需要明确同意")
		}
		if memoryExpired(item) {
			return MemoryItem{}, fmt.Errorf("记忆已过期")
		}
		item.Status = "confirmed"
		item.ConfirmedAt = time.Now().UTC().Format(time.RFC3339)
		item.ConfirmedBy = actor
	case "reject":
		item.Status = "rejected"
	case "expire":
		item.Status = "expired"
	case "delete":
		item.Status = "deleted"
		item.Content = ""
		item.Sources = nil
	default:
		return MemoryItem{}, fmt.Errorf("未知记忆操作")
	}
	item.Version++
	tx, err := s.store.db.Begin()
	if err != nil {
		return MemoryItem{}, err
	}
	defer tx.Rollback()
	if action == "confirm" {
		for _, m := range list {
			if m.ID != item.ID && !memoryTerminal(m.Status) && memorySameKey(m, item) {
				old := m
				m.Status = "superseded"
				m.Version++
				if err = s.store.writeMemoryTx(tx, account, m, &old, actor, "supersede", memoryFingerprint(m)); err != nil {
					return MemoryItem{}, err
				}
			}
		}
	}
	if action == "delete" {
		if _, err = tx.Exec("DELETE FROM memory_changes WHERE account=? AND item_id=?", account, item.ID); err != nil {
			return MemoryItem{}, err
		}
	}
	auditBefore := &before
	if action == "delete" {
		auditBefore = nil
	}
	if err = s.store.writeMemoryTx(tx, account, item, auditBefore, actor, action, memoryFingerprint(before)); err != nil {
		return MemoryItem{}, err
	}
	if err = tx.Commit(); err != nil {
		return MemoryItem{}, err
	}
	return item, nil
}
func memoryRelevant(m MemoryItem, text string) bool {
	if m.Category == "name" || m.Category == "communication" {
		return true
	}
	text = strings.ToLower(text)
	for _, word := range strings.FieldsFunc(strings.ToLower(m.Key+" "+m.Content), func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) }) {
		if len([]rune(word)) >= 2 && strings.Contains(text, word) {
			return true
		}
		runes := []rune(word)
		for i := 0; i+1 < len(runes); i++ {
			if runes[i] > 127 && runes[i+1] > 127 && strings.Contains(text, string(runes[i:i+2])) {
				return true
			}
		}
	}
	return false
}
func (s *Server) replyMemories(account string, current Message, limit int) ([]MemoryItem, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	st := s.store.state
	if account == "" || st.Account.ID != account {
		return nil, fmt.Errorf("微信账号已切换或未就绪")
	}
	if limit <= 0 {
		return []MemoryItem{}, nil
	}
	if limit > 8 {
		limit = 8
	}
	list, err := s.store.memoriesLocked(account)
	if err != nil {
		return nil, err
	}
	out := []MemoryItem{}
	for _, m := range list {
		if m.Status != "confirmed" || memoryExpired(m) || !memorySourcesValid(st, m) || m.Sensitivity == "sensitive" && (!m.Consent || !m.AllowModel) || !memoryRelevant(m, current.Content) {
			continue
		}
		allowed := m.Scope == "person" && current.SenderID != "" && m.Subject == current.SenderID && m.Target == current.Target || m.Scope == "group" && memoryGroup(st, current.Target) && m.Target == current.Target || m.Scope == "bot" && m.Origin == "manual" && (m.Target == "" || m.Target == current.Target)
		if allowed {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ConfirmedAt > out[j].ConfirmedAt })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
