// 文件名: plugins/dashboard/auth.go
// 主要功能: 用户密码安全哈希（bcrypt）、JWT 令牌签发校验与 RBAC 权限控制中间件
// 作者: 2guan

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// 定义系统支持的双角色枚举常量
const (
	RoleAdmin    = "admin"    // 管理员：拥有全局管理、用户CRUD、系统设置所有权限
	RoleOperator = "operator" // 操作员：拥有聊天控制台、通讯录、朋友圈、AI配置等业务权限，不可管理用户
)

// 上下文传递用户信息的 Context Key 类型
type contextKey string

const userContextKey contextKey = "dashboard_user"

// UserClaims 存储在 JWT 荷载（Payload）中的用户信息
type UserClaims struct {
	UserID      int64  `json:"uid"`
	Username    string `json:"usr"`
	Role        string `json:"rol"`
	DisplayName string `json:"name"`
	ExpiresAt   int64  `json:"exp"`
}

// HashPassword 使用 bcrypt 算法将明文密码计算为安全不可逆的哈希字符串
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// CheckPasswordHash 验证输入的明文密码与数据库中存储的 bcrypt 哈希是否匹配
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// GenerateJWT 为指定用户签发带有过期时间的 HMAC-SHA256 签名 JWT 令牌（默认有效期 7 天）
func GenerateJWT(claims UserClaims, secret string) (string, error) {
	// 默认 7 天有效期
	if claims.ExpiresAt == 0 {
		claims.ExpiresAt = time.Now().Add(7 * 24 * time.Hour).Unix()
	}

	// 1. 组装标准 JWT 头部
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	// 2. 组装 JWT Payload 载荷
	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	// 3. 计算 HMAC-SHA256 签名
	unsignedToken := headerB64 + "." + payloadB64
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsignedToken))
	signatureB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return unsignedToken + "." + signatureB64, nil
}

// ParseJWT 校验并解析传入的 JWT 令牌，若验签失败或令牌过期则返回明确错误
func ParseJWT(tokenString string, secret string) (*UserClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("令牌格式不正确")
	}

	headerB64, payloadB64, signatureB64 := parts[0], parts[1], parts[2]

	// 1. 验证签名完整性
	unsignedToken := headerB64 + "." + payloadB64
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsignedToken))
	expectedSignature := mac.Sum(nil)
	actualSignature, err := base64.RawURLEncoding.DecodeString(signatureB64)
	if err != nil || !hmac.Equal(actualSignature, expectedSignature) {
		return nil, errors.New("令牌签名无效或已被篡改")
	}

	// 2. 解析载荷数据
	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, errors.New("解析载荷数据失败")
	}

	var claims UserClaims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, errors.New("解码用户信息失败")
	}

	// 3. 检查有效期
	if time.Now().Unix() > claims.ExpiresAt {
		return nil, errors.New("登录令牌已过期，请重新登录")
	}

	return &claims, nil
}

// AuthMiddleware 保护需要登录态的 API 路由，解析请求头中的 Authorization Bearer 令牌
func (s *Server) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		token := ""

		// 优先从 Authorization 头获取: Bearer <token>
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				token = strings.TrimSpace(parts[1])
			}
		}

		// 若 Header 未携带，尝试从 URL Query 中获取（主要用于 SSE 流和媒体直链）
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		if token == "" {
			s.writeJSON(w, http.StatusUnauthorized, APIResponse{Code: 401, Message: "未提供登录凭证，请先登录"})
			return
		}

		claims, err := ParseJWT(token, s.plugin.Config.JWTSecret)
		if err != nil {
			s.writeJSON(w, http.StatusUnauthorized, APIResponse{Code: 401, Message: err.Error()})
			return
		}

		// 将用户信息注入请求上下文
		ctx := context.WithValue(r.Context(), userContextKey, claims)
		next(w, r.WithContext(ctx))
	}
}

// RequireAdmin 中间件：在通过登录认证的前提下，确保当前操作者必须具备管理员角色
func (s *Server) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.AuthMiddleware(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(userContextKey).(*UserClaims)
		if !ok || claims.Role != RoleAdmin {
			s.writeJSON(w, http.StatusForbidden, APIResponse{Code: 403, Message: "权限不足：该操作仅限系统管理员执行"})
			return
		}
		next(w, r)
	})
}

// GetCurrentUser 从当前请求上下文中提取已登录的用户信息
func GetCurrentUser(r *http.Request) *UserClaims {
	if val, ok := r.Context().Value(userContextKey).(*UserClaims); ok {
		return val
	}
	return nil
}
