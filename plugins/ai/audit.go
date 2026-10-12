// 文件名: plugins/ai/audit.go
// 主要功能: SQLite 专属策略读取 (Target Overrides) 与 Token 消耗审计异步持久化
// 作者: 2guan

package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite" // 纯 Go 跨平台 SQLite 驱动
)

// TargetOverride 目标专属策略
type TargetOverride struct {
	TargetID       string
	TargetType     string
	TargetName     string
	CustomPrompt   string
	PersonaSummary string
	ModelName      string
	ReplyRate      float64
	ContextRounds  int
	SilentMode     bool
}

// Auditor 审计与策略管理引擎
type Auditor struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewAuditor 实例化审计存储连接
func NewAuditor(dataDir string) (*Auditor, error) {
	if dataDir == "" {
		dataDir = filepath.Join("runtime", "gateway", "data")
	}

	dbPath := filepath.Join(dataDir, "dashboard.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 数据库失败: %w", err)
	}

	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	return &Auditor{db: db}, nil
}

// Close 关闭数据库
func (a *Auditor) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// GetTargetOverride 查询联系人或群聊的专属策略覆盖
func (a *Auditor) GetTargetOverride(targetID string) (*TargetOverride, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.db == nil {
		return nil, nil
	}

	query := `
	SELECT target_id, target_type, target_name, custom_prompt, persona_summary, 
	       model_name, reply_rate, context_rounds, silent_mode
	FROM target_overrides
	WHERE target_id = ?
	LIMIT 1
	`
	var (
		t       TargetOverride
		silent  int
		cPrompt sql.NullString
		pSumm   sql.NullString
		mName   sql.NullString
		tName   sql.NullString
	)

	err := a.db.QueryRow(query, targetID).Scan(
		&t.TargetID, &t.TargetType, &tName,
		&cPrompt, &pSumm, &mName,
		&t.ReplyRate, &t.ContextRounds, &silent,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	t.TargetName = tName.String
	t.CustomPrompt = cPrompt.String
	t.PersonaSummary = pSumm.String
	t.ModelName = mName.String
	t.SilentMode = (silent == 1)

	return &t, nil
}

// RecordTokenUsage 异步落盘单次对话 Token 消耗
func (a *Auditor) RecordTokenUsage(sessionID, modelName string, promptTokens, completionTokens, totalTokens int, durationMs int64) {
	go func() {
		a.mu.Lock()
		defer a.mu.Unlock()

		if a.db == nil {
			return
		}

		query := `
		INSERT INTO token_usage_logs (session_id, model_name, prompt_tokens, completion_tokens, total_tokens, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		`
		_, err := a.db.Exec(query, sessionID, modelName, promptTokens, completionTokens, totalTokens, durationMs)
		if err != nil {
			slog.Warn("[audit] 记录 Token 消耗日志失败", "session", sessionID, "err", err)
		}
	}()
}
