// 文件名: plugins/dashboard/store.go
// 主要功能: 基于纯 Go 跨平台 SQLite 的本地持久化存储层（用户、审计消息、专属策略、Token统计、系统配置）
// 作者: 2guan

package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // 注册纯 Go 跨平台 SQLite 驱动
)

// Store 持久化存储引擎管理器
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewStore 初始化或打开本地 SQLite 数据库，并自动执行数据表结构初始化与迁移
func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	dbPath := filepath.Join(dataDir, "dashboard.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 数据库失败: %w", err)
	}

	// 配置连接池参数
	db.SetMaxOpenConns(1) // SQLite 保持单写连接防锁
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	store := &Store{db: db}
	if err := store.initTables(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化数据库表结构失败: %w", err)
	}

	return store, nil
}

// Close 安全关闭数据库连接
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// initTables 自动创建所有必要的业务数据表和查询索引
func (s *Store) initTables() error {
	schema := `
	-- 1. 用户与管理员账号表
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'operator', -- 'admin' 或 'operator'
		display_name TEXT,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_login_at DATETIME
	);

	-- 2. 目标专属策略覆盖表 (Target Overrides)
	CREATE TABLE IF NOT EXISTS target_overrides (
		target_id TEXT PRIMARY KEY,
		target_type TEXT NOT NULL,
		target_name TEXT,
		custom_prompt TEXT,
		persona_summary TEXT,
		model_name TEXT,
		reply_rate REAL DEFAULT -1,
		context_rounds INTEGER DEFAULT -1,
		silent_mode INTEGER DEFAULT 0,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	-- 3. 聊天消息审计历史表
	CREATE TABLE IF NOT EXISTS chat_audit_messages (
		msg_id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		sender_id TEXT NOT NULL,
		sender_name TEXT,
		msg_type TEXT NOT NULL,
		content TEXT,
		media_path TEXT,
		is_self INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_chat_session ON chat_audit_messages(session_id, created_at);
	CREATE INDEX IF NOT EXISTS idx_chat_created ON chat_audit_messages(created_at);

	-- 4. 大模型 Token 消耗审计表
	CREATE TABLE IF NOT EXISTS token_usage_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT,
		model_name TEXT NOT NULL,
		prompt_tokens INTEGER DEFAULT 0,
		completion_tokens INTEGER DEFAULT 0,
		total_tokens INTEGER DEFAULT 0,
		duration_ms INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_token_date ON token_usage_logs(created_at);

	-- 5. 系统基础与品牌定制设置表 (单行键值)
	CREATE TABLE IF NOT EXISTS system_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		site_title TEXT NOT NULL DEFAULT 'we-clay 机器人控制台',
		footer_text TEXT NOT NULL DEFAULT '© 2026 we-clayplus · 微信协议自动化运行系统',
		owner TEXT NOT NULL DEFAULT '',
		forbidden TEXT NOT NULL DEFAULT '无权限执行此操作',
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	INSERT OR IGNORE INTO system_settings (id, site_title, footer_text) 
	VALUES (1, 'we-clay 机器人控制台', '© 2026 we-clayplus · 微信协议自动化运行系统');

	-- 6. AI能力授权矩阵与全维度拟人延时设置表
	CREATE TABLE IF NOT EXISTS ai_matrix_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		data_json TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

// ----------------- 用户账号管理 (User CRUD) -----------------

// CountUsers 统计系统中现有注册的用户数量，用于首次安装引导判断
func (s *Store) CountUsers() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

// CreateUser 新增用户账号
func (s *Store) CreateUser(username, passwordHash, role, displayName string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	res, err := s.db.Exec(`
		INSERT INTO users (username, password_hash, role, display_name, enabled, created_at)
		VALUES (?, ?, ?, ?, 1, ?)`,
		username, passwordHash, role, displayName, now)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return &User{
		ID:          id,
		Username:    username,
		Role:        role,
		DisplayName: displayName,
		Enabled:     true,
		CreatedAt:   now,
	}, nil
}

// GetUserByUsername 根据登录用户名查找用户记录
func (s *Store) GetUserByUsername(username string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var u User
	var lastLogin sql.NullTime
	err := s.db.QueryRow(`
		SELECT id, username, password_hash, role, display_name, enabled, created_at, last_login_at
		FROM users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.DisplayName, &u.Enabled, &u.CreatedAt, &lastLogin)
	if err != nil {
		return nil, err
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Time
	}
	return &u, nil
}

// GetUserByID 根据用户主键 ID 查找用户记录
func (s *Store) GetUserByID(id int64) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var u User
	var lastLogin sql.NullTime
	err := s.db.QueryRow(`
		SELECT id, username, password_hash, role, display_name, enabled, created_at, last_login_at
		FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.DisplayName, &u.Enabled, &u.CreatedAt, &lastLogin)
	if err != nil {
		return nil, err
	}
	if lastLogin.Valid {
		u.LastLoginAt = &lastLogin.Time
	}
	return &u, nil
}

// ListUsers 列出所有用户账号清单
func (s *Store) ListUsers() ([]User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, username, role, display_name, enabled, created_at, last_login_at
		FROM users ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []User
	for rows.Next() {
		var u User
		var lastLogin sql.NullTime
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.DisplayName, &u.Enabled, &u.CreatedAt, &lastLogin); err != nil {
			return nil, err
		}
		if lastLogin.Valid {
			u.LastLoginAt = &lastLogin.Time
		}
		list = append(list, u)
	}
	return list, nil
}

// UpdateUserRoleAndStatus 更新用户角色与启停状态
func (s *Store) UpdateUserRoleAndStatus(id int64, role string, enabled bool, displayName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE users SET role = ?, enabled = ?, display_name = ? WHERE id = ?`, role, enabled, displayName, id)
	return err
}

// UpdateUserPassword 更新用户登录密码哈希
func (s *Store) UpdateUserPassword(id int64, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	return err
}

// UpdateUserProfile 更新用户自身的展示名与登录名
func (s *Store) UpdateUserProfile(id int64, username, displayName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE users SET username = ?, display_name = ? WHERE id = ?`, username, displayName, id)
	return err
}

// UpdateUserLastLogin 更新用户最后一次登录时间
func (s *Store) UpdateUserLastLogin(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec(`UPDATE users SET last_login_at = ? WHERE id = ?`, time.Now(), id)
}

// DeleteUser 删除指定用户账号
func (s *Store) DeleteUser(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	return err
}

// ----------------- 专属策略管理 (Target Overrides) -----------------

// GetTargetOverride 获取特定好友或群聊的专属策略覆盖
func (s *Store) GetTargetOverride(targetID string) (*TargetOverride, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var t TargetOverride
	var silent int
	err := s.db.QueryRow(`
		SELECT target_id, target_type, target_name, custom_prompt, persona_summary, model_name, reply_rate, context_rounds, silent_mode, updated_at
		FROM target_overrides WHERE target_id = ?`, targetID).
		Scan(&t.TargetID, &t.TargetType, &t.TargetName, &t.CustomPrompt, &t.PersonaSummary, &t.ModelName, &t.ReplyRate, &t.ContextRounds, &silent, &t.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // 未配置策略返回 nil
		}
		return nil, err
	}
	t.SilentMode = silent == 1
	return &t, nil
}

// ListTargetOverrides 列出全量已配置专属策略的目标清单
func (s *Store) ListTargetOverrides() ([]TargetOverride, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT target_id, target_type, target_name, custom_prompt, persona_summary, model_name, reply_rate, context_rounds, silent_mode, updated_at
		FROM target_overrides ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []TargetOverride
	for rows.Next() {
		var t TargetOverride
		var silent int
		if err := rows.Scan(&t.TargetID, &t.TargetType, &t.TargetName, &t.CustomPrompt, &t.PersonaSummary, &t.ModelName, &t.ReplyRate, &t.ContextRounds, &silent, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.SilentMode = silent == 1
		list = append(list, t)
	}
	return list, nil
}

// SaveTargetOverride 保存或更新目标专属策略覆盖配置
func (s *Store) SaveTargetOverride(t *TargetOverride) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	silent := 0
	if t.SilentMode {
		silent = 1
	}

	_, err := s.db.Exec(`
		INSERT INTO target_overrides (target_id, target_type, target_name, custom_prompt, persona_summary, model_name, reply_rate, context_rounds, silent_mode, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(target_id) DO UPDATE SET
			target_name = excluded.target_name,
			custom_prompt = excluded.custom_prompt,
			persona_summary = excluded.persona_summary,
			model_name = excluded.model_name,
			reply_rate = excluded.reply_rate,
			context_rounds = excluded.context_rounds,
			silent_mode = excluded.silent_mode,
			updated_at = excluded.updated_at`,
		t.TargetID, t.TargetType, t.TargetName, t.CustomPrompt, t.PersonaSummary, t.ModelName, t.ReplyRate, t.ContextRounds, silent, time.Now())
	return err
}

// DeleteTargetOverride 删除目标专属策略配置
func (s *Store) DeleteTargetOverride(targetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM target_overrides WHERE target_id = ?`, targetID)
	return err
}

// ----------------- 聊天审计与消息历史 (Chat Audit) -----------------

// AppendChatMessage 审计写入一条新微信消息
func (s *Store) AppendChatMessage(msg *ChatMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	isSelfInt := 0
	if msg.IsSelf {
		isSelfInt = 1
	}

	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO chat_audit_messages (msg_id, session_id, sender_id, sender_name, msg_type, content, media_path, is_self, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.MsgID, msg.SessionID, msg.SenderID, msg.SenderName, msg.MsgType, msg.Content, msg.MediaPath, isSelfInt, msg.CreatedAt)
	return err
}

// GetSessionMessages 分页读取特定会话的近 N 条消息
func (s *Store) GetSessionMessages(sessionID string, limit int) ([]ChatMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > 200 {
		limit = 50
	}

	rows, err := s.db.Query(`
		SELECT msg_id, session_id, sender_id, sender_name, msg_type, content, media_path, is_self, created_at
		FROM chat_audit_messages
		WHERE session_id = ?
		ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []ChatMessage
	for rows.Next() {
		var m ChatMessage
		var isSelf int
		if err := rows.Scan(&m.MsgID, &m.SessionID, &m.SenderID, &m.SenderName, &m.MsgType, &m.Content, &m.MediaPath, &isSelf, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.IsSelf = isSelf == 1
		m.IsOutgoing = m.IsSelf
		list = append(list, m)
	}

	// 翻转为时间升序，方便前端自然排版
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}

	return list, nil
}

// CountTotalMessages 统计已记录入库的微信消息总数
func (s *Store) CountTotalMessages() (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int64
	err := s.db.QueryRow("SELECT COUNT(*) FROM chat_audit_messages").Scan(&count)
	return count, err
}

// GetMessageTrafficTrends 统计近 X 日的消息吞吐趋势（分为接收消息与发出消息）
func (s *Store) GetMessageTrafficTrends(days int) ([]string, []int64, []int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if days <= 0 {
		days = 7
	}

	dateList := make([]string, days)
	inboundMap := make(map[string]int64)
	outboundMap := make(map[string]int64)

	now := time.Now()
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		dateList[days-1-i] = d
	}

	since := now.AddDate(0, 0, -days).Format("2006-01-02 00:00:00")
	rows, err := s.db.Query(`
		SELECT strftime('%Y-%m-%d', created_at) as day, is_self, COUNT(*)
		FROM chat_audit_messages
		WHERE created_at >= ?
		GROUP BY day, is_self`, since)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var day string
		var isSelf int
		var count int64
		if err := rows.Scan(&day, &isSelf, &count); err == nil {
			if isSelf == 1 {
				outboundMap[day] = count
			} else {
				inboundMap[day] = count
			}
		}
	}

	inbounds := make([]int64, days)
	outbounds := make([]int64, days)
	for i, d := range dateList {
		inbounds[i] = inboundMap[d]
		outbounds[i] = outboundMap[d]
	}

	return dateList, inbounds, outbounds, nil
}

// ----------------- 大模型 Token 统计 (Token Usage) -----------------

// RecordTokenUsage 审计记录一次大模型交互的 Token 消耗
func (s *Store) RecordTokenUsage(log *TokenLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO token_usage_logs (session_id, model_name, prompt_tokens, completion_tokens, total_tokens, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		log.SessionID, log.ModelName, log.PromptTokens, log.CompletionTokens, log.TotalTokens, log.DurationMs, log.CreatedAt)
	return err
}

// GetTokenKPI 统计今日及本周 Token 消耗总和与对话总次数
func (s *Store) GetTokenKPI() (todayTokens int64, todayChats int, weekTokens int64, weekChats int, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Format("2006-01-02 00:00:00")
	weekStart := now.AddDate(0, 0, -7).Format("2006-01-02 00:00:00")

	// 今日
	_ = s.db.QueryRow(`
		SELECT COALESCE(SUM(total_tokens), 0), COUNT(*)
		FROM token_usage_logs WHERE created_at >= ?`, todayStart).Scan(&todayTokens, &todayChats)

	// 本周 (近7日)
	_ = s.db.QueryRow(`
		SELECT COALESCE(SUM(total_tokens), 0), COUNT(*)
		FROM token_usage_logs WHERE created_at >= ?`, weekStart).Scan(&weekTokens, &weekChats)

	return todayTokens, todayChats, weekTokens, weekChats, nil
}

// GetTokenDailyTrends 获取近 X 日的 Token 消耗双曲线走势
func (s *Store) GetTokenDailyTrends(days int) ([]string, []int64, []int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if days <= 0 {
		days = 7
	}

	dateList := make([]string, days)
	promptMap := make(map[string]int64)
	compMap := make(map[string]int64)

	now := time.Now()
	for i := days - 1; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		dateList[days-1-i] = d
	}

	since := now.AddDate(0, 0, -days).Format("2006-01-02 00:00:00")
	rows, err := s.db.Query(`
		SELECT strftime('%Y-%m-%d', created_at) as day, 
		       COALESCE(SUM(prompt_tokens), 0), 
		       COALESCE(SUM(completion_tokens), 0)
		FROM token_usage_logs
		WHERE created_at >= ?
		GROUP BY day`, since)
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var day string
		var p, c int64
		if err := rows.Scan(&day, &p, &c); err == nil {
			promptMap[day] = p
			compMap[day] = c
		}
	}

	prompts := make([]int64, days)
	completions := make([]int64, days)
	for i, d := range dateList {
		prompts[i] = promptMap[d]
		completions[i] = compMap[d]
	}

	return dateList, prompts, completions, nil
}

// GetTokenModelDistribution 获取各模型消耗总量的分布饼图数据
func (s *Store) GetTokenModelDistribution() ([]string, []int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT model_name, COALESCE(SUM(total_tokens), 0) as total
		FROM token_usage_logs
		GROUP BY model_name
		ORDER BY total DESC LIMIT 6`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var labels []string
	var values []int64
	for rows.Next() {
		var m string
		var v int64
		if err := rows.Scan(&m, &v); err == nil {
			labels = append(labels, m)
			values = append(values, v)
		}
	}
	return labels, values, nil
}

// ----------------- 系统设置 (System Settings) -----------------

// GetSystemSettings 读取当前系统品牌与基础设置
func (s *Store) GetSystemSettings() (*SystemSettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var cfg SystemSettings
	err := s.db.QueryRow(`
		SELECT site_title, footer_text, owner, forbidden
		FROM system_settings WHERE id = 1`).
		Scan(&cfg.SiteTitle, &cfg.FooterText, &cfg.Owner, &cfg.Forbidden)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveSystemSettings 保存系统品牌与基础配置
func (s *Store) SaveSystemSettings(cfg *SystemSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		UPDATE system_settings
		SET site_title = ?, footer_text = ?, owner = ?, forbidden = ?, updated_at = ?
		WHERE id = 1`,
		cfg.SiteTitle, cfg.FooterText, cfg.Owner, cfg.Forbidden, time.Now())
	return err
}

// GetSettings 是 GetSystemSettings 的简便别名
func (s *Store) GetSettings() (*SystemSettings, error) {
	return s.GetSystemSettings()
}

// SaveSettings 是 SaveSystemSettings 的简便别名
func (s *Store) SaveSettings(cfg *SystemSettings) error {
	return s.SaveSystemSettings(cfg)
}

// SaveChatMessage 是 AppendChatMessage 的简便别名
func (s *Store) SaveChatMessage(msg *ChatMessage) error {
	return s.AppendChatMessage(msg)
}

// GetAIMatrixSettings 获取 AI 权限矩阵与全维度拟人延时配置
func (s *Store) GetAIMatrixSettings() (*AIMatrixSettings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var dataJSON string
	err := s.db.QueryRow(`SELECT data_json FROM ai_matrix_settings WHERE id = 1`).Scan(&dataJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			def := DefaultAIMatrixSettings()
			return &def, nil
		}
		return nil, err
	}

	var res AIMatrixSettings
	if err := json.Unmarshal([]byte(dataJSON), &res); err != nil {
		def := DefaultAIMatrixSettings()
		return &def, nil
	}
	return &res, nil
}

// SaveAIMatrixSettings 保存 AI 权限矩阵与全维度拟人延时配置
func (s *Store) SaveAIMatrixSettings(cfg *AIMatrixSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dataBytes, err := json.Marshal(cfg)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT INTO ai_matrix_settings (id, data_json, updated_at)
		VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET data_json = excluded.data_json, updated_at = excluded.updated_at`,
		string(dataBytes), time.Now())
	return err
}

