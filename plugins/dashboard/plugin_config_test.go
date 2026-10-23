// 文件名：plugin_config_test.go
// 主要功能：验证插件配置的持久化、管理员边界、文本校验和并发编辑保护。
// 作者：2guan
package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func readConfigFixture(t *testing.T, s *Server, id string) pluginDocument {
	t.Helper()
	w := call(t, s, "/api/plugin-config?id="+id, nil, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var doc pluginDocument
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// Mock 目录说明必须明确演示边界，不能伪造可在微信执行的触发词。
func TestPluginCatalogUsage(t *testing.T) {
	s := newServer(t, true)
	w := call(t, s, "/api/plugins", nil, nil, "")
	var items []pluginCatalogItem
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &items) != nil || len(items) != 30 {
		t.Fatal("目录读取失败", w.Body.String())
	}
	for _, item := range items {
		if item.Usage == nil || len(item.Usage.Steps) == 0 {
			t.Fatalf("插件缺少使用说明：%s", item.ID)
		}
		if !isBusinessPlugin(item.ID) && item.ID != "ai" && len(item.Usage.Commands) != 0 {
			t.Fatal("内置插件不应伪造触发词")
		}
		if item.ID == "ai" && !strings.Contains(strings.Join(item.Usage.Steps, ""), "没有统一固定聊天口令") {
			t.Fatal("AI 自然语言示例被误写成固定口令")
		}
		if item.ID == "welcome" && !strings.Contains(strings.Join(item.Usage.Steps, ""), "脱机演示不会操作真实微信") {
			t.Fatal("演示边界缺失")
		}
	}
}
func TestPluginConfigRoundTripAndConflict(t *testing.T) {
	s := newServer(t, true)
	for _, id := range []string{"dashboard", "ai", "welcome"} {
		doc := readConfigFixture(t, s, id)
		if id == "dashboard" {
			doc.Values["brand"].(map[string]any)["name"] = "配置编辑测试"
		}
		if id == "ai" {
			doc.Values["routing"].(map[string]any)["timeout"] = 123.0
		}
		if id == "welcome" {
			doc.Values["welcome"].(map[string]any)["message"] = "新的问候"
		}
		q := pluginConfigRequest{ID: id, Values: doc.Values, Revision: doc.Revision, Save: true}
		w := call(t, s, "/api/plugin-config", q, nil, "")
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		again := readConfigFixture(t, s, id)
		if again.Revision == doc.Revision {
			t.Fatal("保存后版本未改变")
		}
		if id == "welcome" && !strings.Contains(again.Text, "新的问候") {
			t.Fatal("文本未持久化")
		}
		if w = call(t, s, "/api/plugin-config", q, nil, ""); w.Code != 400 {
			t.Fatal("旧版本覆盖新配置")
		}
		// 切换编辑方式只解析草稿，不落盘。
		preview := "invalid = ["
		if w = call(t, s, "/api/plugin-config", pluginConfigRequest{ID: id, Text: &preview}, nil, ""); w.Code != 400 {
			t.Fatal("错误 TOML 被接受")
		}
		if readConfigFixture(t, s, id).Text != again.Text {
			t.Fatal("解析失败改动了配置")
		}
	}
	if s.store.snapshot().Settings["routing.timeout"] != 123.0 {
		t.Fatal("AI 设置未共用原有配置")
	}
	state := call(t, s, "/api/state", nil, nil, "")
	if !strings.Contains(state.Body.String(), "新的问候") {
		t.Fatal("管理员状态应包含当前配置")
	}
	// 重新打开数据库也能读取扩展插件配置。
	reopened, err := openStore(s.cfg.DataDir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.db.Close()
	if reopened.snapshot().Settings["welcome.message"] != "新的问候" {
		t.Fatal("重启后配置丢失")
	}
}
func TestPluginConfigAdminAndValidation(t *testing.T) {
	s := newServer(t, false)
	if w := call(t, s, "/api/plugin-config?id=dashboard", nil, nil, ""); w.Code != 401 {
		t.Fatal("匿名可读配置")
	}
	w := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-admin", "password": "fixture-password-123"}, nil, "")
	admin := w.Result().Cookies()[0]
	w = call(t, s, "/api/action", actionRequest{Kind: "user", Name: "fixture-operator", Password: "fixture-password-123", Role: "operator", Enabled: true}, admin, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = call(t, s, "/api/auth/login", map[string]string{"name": "fixture-operator", "password": "fixture-password-123"}, nil, "")
	operator := w.Result().Cookies()[0]
	for _, body := range []any{nil, pluginConfigRequest{ID: "dashboard", Values: map[string]any{"brand": map[string]any{"name": "越权"}}, Save: true}} {
		if w = call(t, s, "/api/plugin-config?id=dashboard", body, operator, ""); w.Code != http.StatusForbidden {
			t.Fatal("操作员可读写配置")
		}
	}
	for _, text := range []string{"[routing]\ntimeout = 0", "[unknown]\na = 1", "[memory]\nsummary = true"} {
		w = call(t, s, "/api/plugin-config", pluginConfigRequest{ID: "ai", Text: &text, Save: true, Revision: "1"}, admin, "")
		if w.Code != 400 {
			t.Fatal("无效字段或真实模式预览限制被绕过", text, w.Body.String())
		}
	}
}
