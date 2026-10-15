// 文件名：store.go
// 主要功能：使用 SQLite 事务保存控制台状态、账号与加密凭据，保证重启可恢复。
// 作者：2guan
package dashboard

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type store struct {
	mu    sync.Mutex
	db    *sql.DB
	state State
	key   []byte
}

func openStore(dir string, mock bool) (*store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	// 数据库与密钥均只对当前系统用户开放；Mock 使用单独文件，绝不覆盖正式数据。
	name := "console.db"
	if mock {
		name = "mock.db"
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*store, error) { db.Close(); return nil, e }
	if _, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS safety_events (id INTEGER PRIMARY KEY AUTOINCREMENT, time TEXT NOT NULL, task TEXT NOT NULL, source TEXT NOT NULL, stage TEXT NOT NULL, action TEXT NOT NULL, detail TEXT NOT NULL); CREATE TABLE IF NOT EXISTS documents (id TEXT PRIMARY KEY, body BLOB NOT NULL); CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, name TEXT UNIQUE NOT NULL, nickname TEXT NOT NULL, role TEXT NOT NULL, enabled INTEGER NOT NULL, password TEXT NOT NULL); CREATE TABLE IF NOT EXISTS secrets (id TEXT PRIMARY KEY, value TEXT NOT NULL);`); err != nil {
		return fail(err)
	}
	if err = os.Chmod(filepath.Join(dir, name), 0600); err != nil {
		return fail(err)
	}
	s := &store{db: db, state: initialState(mock)}
	keyPath := filepath.Join(dir, "credential.key")
	s.key, err = os.ReadFile(keyPath)
	if os.IsNotExist(err) {
		s.key = make([]byte, 32)
		if _, err = rand.Read(s.key); err != nil {
			return fail(err)
		}
		err = os.WriteFile(keyPath, s.key, 0600)
	}
	if err != nil {
		return fail(err)
	}
	if len(s.key) != 32 {
		return fail(fmt.Errorf("凭据加密密钥长度无效"))
	}
	var raw []byte
	err = db.QueryRow("SELECT body FROM documents WHERE id='state'").Scan(&raw)
	if err == nil {
		s.state.Vendors = nil
		s.state.Pipeline = nil
		s.state.Pipelines = nil
		s.state.Policies = nil
		if err = json.Unmarshal(raw, &s.state); err != nil {
			return fail(fmt.Errorf("控制台数据损坏：%w", err))
		}
	} else if err != sql.ErrNoRows {
		return fail(err)
	}
	for i := range s.state.Contacts {
		classifyEnterpriseContact(&s.state.Contacts[i])
	}
	// 统一升级历史消息展示，客户端控制通知不会继续污染聊天。
	normalizeWechatArchive(&s.state)
	// 补注册新增基础插件，已有开关保持不变。
	for _, p := range append(safetyPlugins(), businessPlugins(mock)...) {
		if !slices.ContainsFunc(s.state.Plugins, func(old Plugin) bool { return old.ID == p.ID }) {
			s.state.Plugins = append(s.state.Plugins, p)
		}
	}
	// 旧 Mock 演示卡片升级为可执行内建插件，保留开关。
	for i := range s.state.Plugins {
		if isBusinessPlugin(s.state.Plugins[i].ID) {
			s.state.Plugins[i].Builtin = true
			s.state.Plugins[i].Version = "1.0.0"
		}
	}
	if s.state.ConfigRevision == 0 {
		s.state.ConfigRevision = 1
	}
	if !mock {
		s.state.Account.Status = "offline"
	}
	migrateBusinessSettings(s, mock)
	// 旧版 90 秒是系统默认超时；升级到 5 分钟，避免大量图片分析沿用过短的旧默认值。
	if timeout, ok := s.state.Settings["routing.timeout"].(float64); ok && timeout == 90 {
		s.state.Settings["routing.timeout"] = 300
	}
	// 升级时仅补缺省字段，不覆盖用户已保存的设置。
	for k, v := range defaults() {
		if _, ok := s.state.Settings[k]; !ok {
			s.state.Settings[k] = v
			if mock && isBusinessPlugin(strings.SplitN(k, ".", 2)[0]) && strings.HasSuffix(k, ".targets") {
				s.state.Settings[k] = "mock-contact-0,mock-contact-1,mock-contact-2"
			}
		}
	}
	if s.state.Overrides == nil {
		s.state.Overrides = map[string]map[string]any{}
	}
	// 旧版每个连接独立保存凭据；迁移时复用原密钥 ID，不合并可能属于不同账号的连接。
	if s.state.Vendors == nil {
		s.state.Vendors = []Vendor{}
		for i := range s.state.Models {
			m := &s.state.Models[i]
			m.VendorID = m.ID
			s.state.Vendors = append(s.state.Vendors, Vendor{ID: m.ID, Name: m.Name, Provider: m.Provider, Endpoint: m.Endpoint, Configured: m.Configured})
		}
	}
	if s.state.Pipeline == nil {
		s.state.Pipeline = []string{}
		for _, m := range s.state.Models {
			for _, cap := range m.Capabilities {
				if cap == "chat" && m.Enabled {
					s.state.Pipeline = append(s.state.Pipeline, m.ID)
				}
			}
		}
	}
	// 迁移旧默认路由时保持原来的首选模型，不因新流水线改变请求目的地。
	if len(s.state.Pipeline) > 0 && len(raw) > 0 {
		var legacy map[string]json.RawMessage
		_ = json.Unmarshal(raw, &legacy)
		if _, exists := legacy["pipeline"]; !exists {
			preferred, _ := s.state.Settings["routing.chat"].(string)
			if index := slices.Index(s.state.Pipeline, preferred); index > 0 {
				s.state.Pipeline = append([]string{preferred}, append(s.state.Pipeline[:index:index], s.state.Pipeline[index+1:]...)...)
			}
		}
	}
	// 仅为旧版内置 Mock 朋友圈补齐已知作者 ID，真实历史内容不根据昵称推断归属。
	if mock {
		for i := range s.state.Moments {
			m := &s.state.Moments[i]
			if m.AuthorID == "" {
				m.AuthorID = map[string]string{"mock-moment-1": "mock-contact-1", "mock-moment-2": "mock-contact-5"}[m.ID]
			}
		}
	}
	// 老版 Mock 补充演示视频模型能力；只更新内置演示项，不改真实模型。
	if mock {
		for i := range s.state.Models {
			m := &s.state.Models[i]
			if m.ID == "demo-create" && m.Provider == "mock" && !slices.Contains(m.Capabilities, "video_generation") {
				m.Capabilities = append(m.Capabilities, "video_generation")
			}
		}
	}
	if s.state.Policies == nil {
		s.state.Policies = []BehaviorPolicy{}
	}
	// 兼容开发期 Mock 的 MM-DD 样例；补齐年份，避免升级后今日指标错误显示为零。
	if mock {
		for i := range s.state.Trend {
			if len(s.state.Trend[i].Date) == 5 {
				stamp, e := time.ParseInLocation("2006-01-02", fmt.Sprintf("%d-%s", time.Now().Year(), s.state.Trend[i].Date), time.Local)
				if e == nil {
					if stamp.After(time.Now()) {
						stamp = stamp.AddDate(-1, 0, 0)
					}
					s.state.Trend[i].Date = stamp.Format("2006-01-02")
				}
			}
		}
	}
	for i := range s.state.Tasks {
		if s.state.Tasks[i].Status == "running" || s.state.Tasks[i].Status == "queued" {
			s.state.Tasks[i].Status = "interrupted"
			s.state.Tasks[i].Error = "运行在上次退出时中断；不会自动重复提交。"
		}
	}
	if err = s.saveLocked(); err != nil {
		return fail(err)
	}
	return s, nil
}
func (s *store) saveLocked() error {
	raw, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO documents(id,body) VALUES('state',?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", raw)
	return err
}

// change 先复制工作状态，只有数据库提交成功才发布，避免内存与磁盘分叉。
func (s *store) change(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(s.state)
	var next State
	if err := json.Unmarshal(raw, &next); err != nil {
		return err
	}
	if err := fn(&next); err != nil {
		return err
	}
	next.Revision++
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if _, err = s.db.Exec("UPDATE documents SET body=? WHERE id='state'", raw); err != nil {
		return err
	}
	s.state = next
	return nil
}
func (s *store) snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(s.state)
	var v State
	_ = json.Unmarshal(raw, &v)
	// 仅在读取副本时解析继承状态，避免写回后丢失“跟随默认”的语义。
	for i := range v.Contacts {
		classifyEnterpriseContact(&v.Contacts[i])
	}
	resolveReplyModes(&v)
	return v
}
func (s *store) putSecret(id, value string) error {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	sealed := aead.Seal(nonce, nonce, []byte(value), []byte(id))
	_, err = s.db.Exec("INSERT INTO secrets(id,value) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET value=excluded.value", id, base64.StdEncoding.EncodeToString(sealed))
	return err
}
func (s *store) getSecret(id string) (string, error) {
	var encoded string
	if err := s.db.QueryRow("SELECT value FROM secrets WHERE id=?", id).Scan(&encoded); err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, _ := aes.NewCipher(s.key)
	aead, _ := cipher.NewGCM(block)
	if len(raw) < aead.NonceSize() {
		return "", fmt.Errorf("凭据格式无效")
	}
	value, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(id))
	return string(value), err
}

// accountArchive 只保存账号所属的数据，模型连接、控制台管理员与品牌设置仍属于部署实例。
type accountArchive struct {
	Contacts  []Contact
	Messages  []Message
	Moments   []Moment
	Tasks     []Task
	Overrides map[string]map[string]any
	Trend     []Point
	Logs      []Record
}

func archive(v State) accountArchive {
	return accountArchive{v.Contacts, v.Messages, v.Moments, v.Tasks, v.Overrides, v.Trend, v.Logs}
}

// switchAccount 在一个 SQLite 事务里保存旧账号分区、载入新账号分区和切换活动状态。
// 账号切换不丢弃历史，也不会让旧会话、专属提示词或媒体入口出现在新账号中。
func (s *store) switchAccount(a Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	var next State
	if err = json.Unmarshal(raw, &next); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if next.Account.ID != "" && a.ID != "" && next.Account.ID != a.ID {
		raw, err = json.Marshal(archive(next))
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO documents(id,body) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", "account:"+next.Account.ID, raw); err != nil {
			return err
		}
		data := archive(initialState(false))
		err = tx.QueryRow("SELECT body FROM documents WHERE id=?", "account:"+a.ID).Scan(&raw)
		if err == nil {
			if err = json.Unmarshal(raw, &data); err != nil {
				return err
			}
		} else if err != sql.ErrNoRows {
			return err
		}
		next.Contacts = data.Contacts
		next.Messages = data.Messages
		next.Moments = data.Moments
		next.Tasks = data.Tasks
		next.Overrides = data.Overrides
		next.Trend = data.Trend
		next.Logs = data.Logs
	}
	next.Account = a
	next.Revision++
	raw, err = json.Marshal(next)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE documents SET body=? WHERE id='state'", raw); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.state = next
	return nil
}

// resolveReplyModes 兼容历史暂停状态，统一页面展示与所有自动回复执行器读取的模式。
func resolveReplyModes(v *State) {
	for i := range v.Contacts {
		c := &v.Contacts[i]
		// 已明确接管、启用自动或手动暂停的会话，不因修改默认值而改变。
		explicit, _ := v.Overrides[c.ID]["mode"].(string)
		c.ModeInherited = c.Mode != "manual" && c.Mode != "auto" && explicit == ""
		if c.Mode == "manual" || c.Mode == "auto" {
			continue
		}
		c.Mode = "manual"
		if c.ModeInherited && (c.Kind == "friend" || c.Kind == "group") && settingText(*v, "reply.default_"+c.Kind) == "自动" {
			c.Mode = "auto"
		}
	}
}
