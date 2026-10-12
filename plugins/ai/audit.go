// 文件名: plugins/ai/audit.go
// 主要功能: SQLite 专属策略读取 (Target Overrides) 与 Token 消耗审计异步持久化
// 作者: 2guan

package main

import (
	"database/sql"
	"encoding/json"
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

// aiMatrixDBSettings 对应 dashboard 存储在 ai_matrix_settings 的结构体
type aiMatrixDBSettings struct {
	MomentsEnabled       bool   `json:"moments_enabled"`
	MomentsTimes         string `json:"moments_times"`
	MomentsMinWait       int    `json:"moments_min_wait"`
	MomentsMaxWait       int    `json:"moments_max_wait"`
	MomentsMaxOps        int    `json:"moments_max_ops"`
	LikeThreshold        int    `json:"like_threshold"`
	AddFriendEnabled     bool   `json:"add_friend_enabled"`
	AddFriendMsg         string `json:"add_friend_msg"`
	AddFriendLimit       int    `json:"add_friend_limit"`
	AddFriendMinWait     int    `json:"add_friend_min_wait"`
	AddFriendMaxWait     int    `json:"add_friend_max_wait"`
	AutoVerify           bool   `json:"auto_verify"`
	VerifyKeyword        string `json:"verify_keyword"`
	VerifyMinDelay       int    `json:"verify_min_delay"`
	VerifyMaxDelay       int    `json:"verify_max_delay"`
	VerifyGreet          string `json:"verify_greet"`
	GreetMinDelay        int    `json:"greet_min_delay"`
	GreetMaxDelay        int    `json:"greet_max_delay"`
	PatBack              bool   `json:"pat_back"`
	PatProactive         bool   `json:"pat_proactive"`
	PatCooldown          int    `json:"pat_cooldown"`
	PatMinDelay          int    `json:"pat_min_delay"`
	PatMaxDelay          int    `json:"pat_max_delay"`
	RevokeEnabled        bool   `json:"revoke_enabled"`
	RevokeWindow         int    `json:"revoke_window"`
	RevokeAutoApologize  bool   `json:"revoke_auto_apologize"`
	RevokeApologizeDelay int    `json:"revoke_apologize_delay"`
	AtMode               string `json:"at_mode"`
	TypingSimulate       bool   `json:"typing_simulate"`
	TypingMinMs          int    `json:"typing_min_ms"`
	TypingMaxMs          int    `json:"typing_max_ms"`
	SetRemarkEnabled     bool   `json:"set_remark_enabled"`
	RemarkTemplate       string `json:"remark_template"`
	ChatroomAdminEnabled bool   `json:"chatroom_admin_enabled"`
	ChatroomKickWords    string `json:"chatroom_kick_words"`
	VoiceEnabled         bool   `json:"voice_enabled"`
	VoiceName            string `json:"voice_name"`
}

// LoadMatrixSettingsFromDB 从 SQLite 读取控制台保存的最新矩阵与延时配置
func (a *Auditor) LoadMatrixSettingsFromDB(cfg *MatrixConfig) error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.db == nil {
		return nil
	}

	var dataJSON string
	err := a.db.QueryRow(`SELECT data_json FROM ai_matrix_settings WHERE id = 1`).Scan(&dataJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	var s aiMatrixDBSettings
	if err := json.Unmarshal([]byte(dataJSON), &s); err != nil {
		return err
	}

	// 动态更新矩阵与全维度延时
	cfg.Moments.Enabled = s.MomentsEnabled
	if s.MomentsTimes != "" {
		cfg.Moments.CronSchedule = s.MomentsTimes
	}
	if s.MomentsMinWait > 0 {
		cfg.Moments.MinWaitSec = s.MomentsMinWait
	}
	if s.MomentsMaxWait > 0 {
		cfg.Moments.MaxWaitSec = s.MomentsMaxWait
	}
	if s.MomentsMaxOps > 0 {
		cfg.Moments.MaxOpsPerRun = s.MomentsMaxOps
	}
	if s.LikeThreshold > 0 {
		cfg.Moments.SentimentThreshold = float64(s.LikeThreshold)
	}

	cfg.AddFriend.Enabled = s.AddFriendEnabled
	if s.AddFriendMsg != "" {
		cfg.AddFriend.GreetTemplate = s.AddFriendMsg
	}
	if s.AddFriendLimit > 0 {
		cfg.AddFriend.DailyLimit = s.AddFriendLimit
	}
	if s.AddFriendMinWait > 0 {
		cfg.AddFriend.MinWaitSec = s.AddFriendMinWait
	}
	if s.AddFriendMaxWait > 0 {
		cfg.AddFriend.MaxWaitSec = s.AddFriendMaxWait
	}

	cfg.VerifyFriend.Enabled = s.AutoVerify
	cfg.VerifyFriend.KeywordFilter = s.VerifyKeyword
	if s.VerifyMinDelay > 0 {
		cfg.VerifyFriend.MinVerifyDelaySec = s.VerifyMinDelay
	}
	if s.VerifyMaxDelay > 0 {
		cfg.VerifyFriend.MaxVerifyDelaySec = s.VerifyMaxDelay
	}
	if s.VerifyGreet != "" {
		cfg.VerifyFriend.AutoGreet = s.VerifyGreet
	}
	if s.GreetMinDelay > 0 {
		cfg.VerifyFriend.MinDelaySec = s.GreetMinDelay
	}
	if s.GreetMaxDelay > 0 {
		cfg.VerifyFriend.MaxDelaySec = s.GreetMaxDelay
	}

	cfg.Pat.PatBack = s.PatBack
	cfg.Pat.Proactive = s.PatProactive
	if s.PatCooldown > 0 {
		cfg.Pat.CooldownSec = s.PatCooldown
	}
	if s.PatMinDelay > 0 {
		cfg.Pat.MinDelaySec = s.PatMinDelay
	}
	if s.PatMaxDelay > 0 {
		cfg.Pat.MaxDelaySec = s.PatMaxDelay
	}

	cfg.Revoke.Enabled = s.RevokeEnabled
	if s.RevokeWindow > 0 {
		cfg.Revoke.WindowSec = s.RevokeWindow
	}
	cfg.Revoke.AutoApologize = s.RevokeAutoApologize
	if s.RevokeApologizeDelay > 0 {
		cfg.Revoke.ApologizeDelaySec = s.RevokeApologizeDelay
	}

	if s.AtMode != "" {
		cfg.AtMode = s.AtMode
	}
	cfg.TypingSimulate.Enabled = s.TypingSimulate
	if s.TypingMinMs > 0 {
		cfg.TypingSimulate.MinMs = s.TypingMinMs
	}
	if s.TypingMaxMs > 0 {
		cfg.TypingSimulate.MaxMs = s.TypingMaxMs
	}

	cfg.SetRemark.Enabled = s.SetRemarkEnabled
	if s.RemarkTemplate != "" {
		cfg.SetRemark.Template = s.RemarkTemplate
	}

	cfg.ChatroomAdmin.Enabled = s.ChatroomAdminEnabled
	cfg.VoiceReply.Enabled = s.VoiceEnabled
	if s.VoiceName != "" {
		cfg.VoiceReply.VoiceName = s.VoiceName
	}

	return nil
}
