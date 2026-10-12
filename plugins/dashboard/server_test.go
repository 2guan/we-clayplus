// 文件名: plugins/dashboard/server_test.go
// 主要功能: Dashboard HTTP API 接口与静态资源托管集成测试
// 作者: 2guan

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)


// TestAuthStatusAndSetupHTTP 测试首次安装状态检查与管理员账号初始设立接口
func TestAuthStatusAndSetupHTTP(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_server.db")

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	defer store.Close()

	p := &DashboardPlugin{
		store: store,
	}
	p.Config = Config{
		JWTSecret: "test-jwt-secret-xyz",
		SiteTitle: "测试控制台",
	}

	sse := NewSSEBroker(100)
	s := NewServer(p, store, sse)

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	// 1. 测试未初始化状态检查
	reqStatus := httptest.NewRequest("GET", "/api/auth/status", nil)
	recStatus := httptest.NewRecorder()
	mux.ServeHTTP(recStatus, reqStatus)

	if recStatus.Code != http.StatusOK {
		t.Fatalf("状态检查接口返回码错误: %d", recStatus.Code)
	}

	var respStatus APIResponse
	_ = json.Unmarshal(recStatus.Body.Bytes(), &respStatus)
	dataMap, ok := respStatus.Data.(map[string]any)
	if !ok || dataMap["initialized"] != false {
		t.Fatalf("新系统 initialized 状态应为 false: %+v", respStatus)
	}

	// 2. 执行首次管理员设立
	setupBody := map[string]string{
		"username": "rootadmin",
		"password": "mypassword123",
		"nickname": "首号管理员",
	}
	rawSetup, _ := json.Marshal(setupBody)
	reqSetup := httptest.NewRequest("POST", "/api/auth/setup", bytes.NewReader(rawSetup))
	recSetup := httptest.NewRecorder()
	mux.ServeHTTP(recSetup, reqSetup)

	if recSetup.Code != http.StatusOK {
		t.Fatalf("首次设立管理员失败, 状态码: %d, body: %s", recSetup.Code, recSetup.Body.String())
	}

	// 3. 执行登录
	loginBody := map[string]string{
		"username": "rootadmin",
		"password": "mypassword123",
	}
	rawLogin, _ := json.Marshal(loginBody)
	reqLogin := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(rawLogin))
	recLogin := httptest.NewRecorder()
	mux.ServeHTTP(recLogin, reqLogin)

	if recLogin.Code != http.StatusOK {
		t.Fatalf("登录失败, 状态码: %d, body: %s", recLogin.Code, recLogin.Body.String())
	}

	var respLogin APIResponse
	_ = json.Unmarshal(recLogin.Body.Bytes(), &respLogin)
	loginData := respLogin.Data.(map[string]any)
	tokenStr, _ := loginData["token"].(string)
	if tokenStr == "" {
		t.Fatalf("登录返回的 JWT 凭证不能为空")
	}

	// 4. 携带 Bearer 凭证访问用户个人资料
	reqProfile := httptest.NewRequest("GET", "/api/auth/profile", nil)
	reqProfile.Header.Set("Authorization", "Bearer "+tokenStr)
	recProfile := httptest.NewRecorder()
	mux.ServeHTTP(recProfile, reqProfile)

	if recProfile.Code != http.StatusOK {
		t.Fatalf("个人信息访问失败, 状态码: %d", recProfile.Code)
	}

	// 5. 测试 SPA 静态资源路由回退
	reqSPA := httptest.NewRequest("GET", "/some/virtual/route", nil)
	recSPA := httptest.NewRecorder()
	mux.ServeHTTP(recSPA, reqSPA)

	if recSPA.Code != http.StatusOK {
		t.Fatalf("SPA 静态回退失败, 状态码: %d", recSPA.Code)
	}
	if !bytes.Contains(recSPA.Body.Bytes(), []byte("we-clay")) {
		t.Fatalf("SPA 回退应返回包含 index.html 的内容")
	}
}
