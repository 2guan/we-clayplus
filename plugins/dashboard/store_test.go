// 文件名: plugins/dashboard/store_test.go
// 主要功能: Dashboard SQLite 数据存储单元测试
// 作者: 2guan

package main

import (
	"path/filepath"
	"testing"
	"time"
)

// TestStoreUserAndSettingsCRUD 测试用户增删改查与系统配置持久化
func TestStoreUserAndSettingsCRUD(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_dashboard.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}
	defer store.Close()

	// 1. 验证用户初始为空
	count, err := store.CountUsers()
	if err != nil || count != 0 {
		t.Fatalf("初始用户数应为 0, err: %v, count: %d", err, count)
	}

	// 2. 创建初始管理员
	hashedPwd, _ := HashPassword("adminPass123")
	_, err = store.CreateUser("admin", hashedPwd, RoleAdmin, "系统管理员")
	if err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	count, _ = store.CountUsers()
	if count != 1 {
		t.Fatalf("创建后用户数应为 1")
	}

	// 3. 查询管理员
	found, err := store.GetUserByUsername("admin")
	if err != nil || found == nil {
		t.Fatalf("按用户名查询用户失败: %v", err)
	}
	if found.DisplayName != "系统管理员" || found.Role != RoleAdmin {
		t.Fatalf("用户数据不匹配: %+v", found)
	}

	// 4. 更新密码与昵称
	newHashed, _ := HashPassword("newPassword456")
	if err := store.UpdateUserPassword(found.ID, newHashed); err != nil {
		t.Fatalf("更新密码失败: %v", err)
	}
	if err := store.UpdateUserProfile(found.ID, "admin", "主控总管"); err != nil {
		t.Fatalf("更新昵称失败: %v", err)
	}

	updated, _ := store.GetUserByID(found.ID)
	if updated.DisplayName != "主控总管" || !CheckPasswordHash("newPassword456", updated.PasswordHash) {
		t.Fatalf("更新后资料未生效: %+v", updated)
	}

	// 5. 测试系统配置保存与读取
	settings, err := store.GetSettings()
	if err != nil {
		t.Fatalf("读取默认系统设置失败: %v", err)
	}
	if settings.SiteTitle != "we-clay 机器人控制台" {
		t.Fatalf("默认标题不匹配: %s", settings.SiteTitle)
	}

	settings.SiteTitle = "VIP微信中控平台"
	settings.FooterText = "测试专用版权 2026"
	if err := store.SaveSettings(settings); err != nil {
		t.Fatalf("保存系统设置失败: %v", err)
	}

	newSettings, _ := store.GetSettings()
	if newSettings.SiteTitle != "VIP微信中控平台" || newSettings.FooterText != "测试专用版权 2026" {
		t.Fatalf("保存后系统设置不匹配: %+v", newSettings)
	}
}

// TestStoreChatAuditAndOverrides 测试聊天审计记录与专属策略存储
func TestStoreChatAuditAndOverrides(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_chat.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}
	defer store.Close()

	// 1. 专属策略保存与获取
	override := &TargetOverride{
		TargetID:     "wxid_test123",
		TargetType:   "contact",
		TargetName:   "测试好友",
		CustomPrompt: "你是专业贴心管家",
		ModelName:    "deepseek-chat",
		ReplyRate:    1.0,
	}
	if err := store.SaveTargetOverride(override); err != nil {
		t.Fatalf("保存专属策略失败: %v", err)
	}

	gotOverride, err := store.GetTargetOverride("wxid_test123")
	if err != nil || gotOverride == nil {
		t.Fatalf("读取专属策略失败: %v", err)
	}
	if gotOverride.CustomPrompt != "你是专业贴心管家" {
		t.Fatalf("专属策略数据不匹配: %+v", gotOverride)
	}

	// 2. 聊天消息记录与回溯
	msg := &ChatMessage{
		MsgID:      "10001",
		SessionID:  "wxid_test123",
		SenderID:   "wxid_test123",
		SenderName: "测试好友",
		MsgType:    "text",
		Content:    "你好，机器人！",
		IsSelf:     false,
		CreatedAt:  time.Now(),
	}
	if err := store.SaveChatMessage(msg); err != nil {
		t.Fatalf("保存聊天消息失败: %v", err)
	}

	// 再次记录我方发出的一条回复
	outMsg := &ChatMessage{
		MsgID:      "10002",
		SessionID:  "wxid_test123",
		SenderID:   "self",
		SenderName: "我方",
		MsgType:    "text",
		Content:    "你好！很高兴为你服务。",
		IsSelf:     true,
		CreatedAt:  time.Now().Add(1 * time.Second),
	}
	if err := store.SaveChatMessage(outMsg); err != nil {
		t.Fatalf("保存发出消息失败: %v", err)
	}

	msgs, err := store.GetSessionMessages("wxid_test123", 10)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("历史消息读取失败, 预期 2 条, 实际 %d 条, err: %v", len(msgs), err)
	}
	if msgs[0].Content != "你好，机器人！" || msgs[1].Content != "你好！很高兴为你服务。" {
		t.Fatalf("消息时序或内容不匹配: %+v", msgs)
	}
}
