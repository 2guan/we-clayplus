// 文件名：auth.go
// 主要功能：实现首次管理员初始化、密码哈希、服务端会话与角色校验。
// 作者：2guan
package dashboard

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type session struct {
	UserID  string
	Expires time.Time
}
type attempt struct {
	Count int
	Until time.Time
}

func randomID() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		panic("安全随机源不可用")
	}
	return hex.EncodeToString(b)
}
func (s *Server) users() ([]User, error) {
	rows, err := s.store.db.Query("SELECT id,name,nickname,role,enabled FROM users ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []User{}
	for rows.Next() {
		var u User
		if err = rows.Scan(&u.ID, &u.Name, &u.Nickname, &u.Role, &u.Enabled); err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, rows.Err()
}
func (s *Server) currentUser(r *http.Request) (User, bool) {
	if s.cfg.Mock {
		return User{ID: "mock-admin", Name: "admin", Nickname: "演示管理员", Role: "admin", Enabled: true}, true
	}
	c, err := r.Cookie("weclay_session")
	if err != nil {
		return User{}, false
	}
	s.authMu.Lock()
	v, ok := s.sessions[c.Value]
	if ok && time.Now().After(v.Expires) {
		delete(s.sessions, c.Value)
		ok = false
	}
	s.authMu.Unlock()
	if !ok {
		return User{}, false
	}
	var u User
	err = s.store.db.QueryRow("SELECT id,name,nickname,role,enabled FROM users WHERE id=?", v.UserID).Scan(&u.ID, &u.Name, &u.Nickname, &u.Role, &u.Enabled)
	return u, err == nil && u.Enabled
}
func (s *Server) auth(w http.ResponseWriter, r *http.Request) {
	users, err := s.users()
	if err != nil {
		writeError(w, 500, "读取用户失败")
		return
	}
	if r.Method == http.MethodGet {
		user, ok := s.currentUser(r)
		// 登录前仅公开品牌名称，不能将整份设置或账号数据交给认证页。
		brandName, _ := s.store.snapshot().Settings["brand.name"].(string)
		if strings.TrimSpace(brandName) == "" {
			brandName = "we-clay"
		}
		writeJSON(w, 200, map[string]any{"initialized": len(users) > 0 || s.cfg.Mock, "authenticated": ok, "user": user, "mock": s.cfg.Mock, "brandName": brandName})
		return
	}
	if r.Method != http.MethodPost || (r.URL.Path != "/api/auth/login" && r.URL.Path != "/api/auth/setup") {
		writeError(w, 405, "请求方法不支持")
		return
	}
	if !sameOrigin(r) {
		writeError(w, 403, "请求来源不允许")
		return
	}
	var req struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Password) > 72 || len(req.Name) > 64 {
		writeError(w, 400, "登录名或密码过长")
		return
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	s.authMu.Lock()
	a := s.attempts[host]
	if time.Now().After(a.Until) {
		a = attempt{Until: time.Now().Add(15 * time.Minute)}
	}
	limit := int(s.number("security.login_attempts", 10))
	if a.Count >= limit {
		s.authMu.Unlock()
		writeError(w, 429, "尝试次数过多，请稍后重试")
		return
	}
	a.Count++
	s.attempts[host] = a
	s.authMu.Unlock()
	if r.URL.Path == "/api/auth/setup" {
		s.setupMu.Lock()
		defer s.setupMu.Unlock()
		// 初始化串行执行，只允许在尚无管理员时创建第一个账号。
		users, err = s.users()
		if err != nil {
			writeError(w, 500, "读取用户失败")
			return
		}
		if len(users) > 0 {
			writeError(w, 403, "已经完成初始化")
			return
		}
		if len(req.Password) < 6 || strings.TrimSpace(req.Name) == "" {
			writeError(w, 400, "请设置登录名和至少 6 字符的密码")
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			writeError(w, 500, "密码处理失败")
			return
		}
		_, err = s.store.db.Exec("INSERT INTO users VALUES(?,?,?,?,?,?)", randomID(), req.Name, req.Name, "admin", true, string(hash))
		if err != nil {
			writeError(w, 500, "创建管理员失败")
			return
		}
	}
	var u User
	err = s.store.db.QueryRow("SELECT id,name,nickname,role,enabled,password FROM users WHERE name=?", req.Name).Scan(&u.ID, &u.Name, &u.Nickname, &u.Role, &u.Enabled, &u.Password)
	if err != nil || !u.Enabled || bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(req.Password)) != nil {
		writeError(w, 401, "登录名或密码不正确")
		return
	}
	token := randomID()
	duration := time.Duration(s.number("security.session_hours", 12)) * time.Hour
	s.authMu.Lock()
	s.sessions[token] = session{u.ID, time.Now().Add(duration)}
	delete(s.attempts, host)
	s.authMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "weclay_session", Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: int(duration.Seconds())})
	writeJSON(w, 200, map[string]any{"user": u})
}

// sameOrigin 配合 JSON 请求和 SameSite Cookie 阻断跨站写入；不信任任意代理头。
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host == r.Host && (u.Scheme == "http" || u.Scheme == "https")
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if contentType != "application/json" {
		writeError(w, 415, "请使用 JSON 请求")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 24<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		writeError(w, 400, "请求格式不正确或内容过大")
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		writeError(w, 400, "请求包含多余内容")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func requireAdmin(u User) error {
	if u.Role != "admin" {
		return fmt.Errorf("仅管理员可执行此操作")
	}
	return nil
}
