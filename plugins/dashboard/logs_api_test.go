package dashboard

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLogsAreSummariesUntilExplicitRead(t *testing.T) {
	s := newServer(t, true)
	body := strings.Repeat("fixture-large-response ", 5000) + "fixture-deep-search"
	if err := s.store.change(func(v *State) error { addLog(v, "DEBUG", "debug", body); return nil }); err != nil {
		t.Fatal(err)
	}
	id := s.store.snapshot().Logs[len(s.store.snapshot().Logs)-1].ID
	w := call(t, s, "/api/state", nil, nil, "")
	if strings.Contains(w.Body.String(), "fixture-deep-search") || w.Body.Len() > 200000 {
		t.Fatal("原始日志仍随全局刷新传输", w.Body.Len())
	}
	w = call(t, s, "/api/logs?q=fixture-deep-search", nil, nil, "")
	var list []Record
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != id || len(list[0].Message) > 2048 {
		t.Fatal("全文搜索或摘要不符")
	}
	w = call(t, s, "/api/logs?id="+id, nil, nil, "")
	var original Record
	if err := json.Unmarshal(w.Body.Bytes(), &original); err != nil || original.Message != body {
		t.Fatal("原文丢失", err)
	}
}

func TestLogOriginalsAndCacheRequireCorrectRole(t *testing.T) {
	s := newServer(t, false)
	if err := s.store.switchAccount(Account{ID: "fixture-account"}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.change(func(v *State) error { addLog(v, "DEBUG", "debug", "fixture-private-log"); return nil }); err != nil {
		t.Fatal(err)
	}
	st := s.store.snapshot()
	id := st.Logs[len(st.Logs)-1].ID
	w := call(t, s, "/api/auth/setup", map[string]string{"name": "fixture-admin", "password": "fixture-password-123"}, nil, "")
	admin := w.Result().Cookies()[0]
	w = call(t, s, "/api/action", actionRequest{Kind: "user", Name: "fixture-viewer", Password: "fixture-password-123", Role: "operator", Enabled: true}, admin, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	w = call(t, s, "/api/auth/login", map[string]string{"name": "fixture-viewer", "password": "fixture-password-123"}, nil, "")
	viewer := w.Result().Cookies()[0]
	if w = call(t, s, "/api/logs?id="+id, nil, viewer, ""); w.Code != 404 {
		t.Fatal("非管理员读取DEBUG原文", w.Code)
	}
	if w = call(t, s, "/api/logs?full=1", nil, viewer, ""); strings.Contains(w.Body.String(), "fixture-private-log") {
		t.Fatal("导出泄露DEBUG")
	}
	for _, body := range []any{nil, map[string]any{"kind": "logs", "days": 0, "confirm": true}} {
		if w = call(t, s, "/api/cache", body, viewer, ""); w.Code != 403 {
			t.Fatal("缓存清理越权", w.Code)
		}
	}
	if err := s.store.switchAccount(Account{ID: "other-account"}); err != nil {
		t.Fatal(err)
	}
	if w = call(t, s, "/api/logs?id="+id, nil, admin, ""); w.Code != 404 {
		t.Fatal("原文跨账号读取", w.Code)
	}
}
