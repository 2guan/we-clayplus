// 文件名: plugins/dashboard/auth_test.go
// 主要功能: Dashboard 鉴权、JWT 签发校验与密码哈希单元测试
// 作者: 2guan

package main

import (
	"testing"
	"time"
)

// TestHashPasswordAndCheck 测试 bcrypt 密码加盐哈希生成与比对校验
func TestHashPasswordAndCheck(t *testing.T) {
	rawPassword := "adminSecret123"

	// 1. 生成哈希
	hashed, err := HashPassword(rawPassword)
	if err != nil {
		t.Fatalf("密码哈希生成失败: %v", err)
	}

	// 2. 正确密码校验
	if !CheckPasswordHash(rawPassword, hashed) {
		t.Fatalf("正确密码比对失败")
	}

	// 3. 错误密码校验
	if CheckPasswordHash("wrongPassword", hashed) {
		t.Fatalf("错误密码不应比对成功")
	}
}

// TestJWTTokenLifecycle 测试 JWT 令牌生命周期（签发、解析与过期失效）
func TestJWTTokenLifecycle(t *testing.T) {
	secret := "test-dashboard-secret-key"

	claims := UserClaims{
		UserID:    1,
		Username:    "admin",
		DisplayName: "系统管理员",
		Role:        RoleAdmin,
		ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
	}

	// 1. 签发合法令牌
	tokenStr, err := GenerateJWT(claims, secret)
	if err != nil {
		t.Fatalf("签发 JWT 失败: %v", err)
	}
	if tokenStr == "" {
		t.Fatalf("生成的令牌不能为空")
	}

	// 2. 解析与验签
	parsed, err := ParseJWT(tokenStr, secret)
	if err != nil {
		t.Fatalf("解析有效 JWT 失败: %v", err)
	}
	if parsed.Username != "admin" || parsed.Role != RoleAdmin {
		t.Fatalf("解析得到的载荷不匹配: %+v", parsed)
	}

	// 3. 篡改秘钥验签应失败
	_, err = ParseJWT(tokenStr, "tampered-secret-key")
	if err == nil {
		t.Fatalf("使用错误密钥验签未报错")
	}

	// 4. 过期令牌应返回错误
	expiredClaims := UserClaims{
		UserID:    2,
		Username:  "expiredUser",
		Role:      RoleOperator,
		ExpiresAt: time.Now().Add(-10 * time.Minute).Unix(),
	}
	expiredToken, _ := GenerateJWT(expiredClaims, secret)
	_, err = ParseJWT(expiredToken, secret)
	if err == nil {
		t.Fatalf("解析过期令牌未报错")
	}
}
