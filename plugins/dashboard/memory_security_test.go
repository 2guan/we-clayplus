// 主要功能：独立验证记忆API信任边界、来源与账号隔离、敏感保存和披露授权。
package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMemorySecurityAPITrustBoundaries(t *testing.T) {
	s, account, msg := memoryFixture(t)
	admin := User{Name: "observer-admin", Role: "admin"}
	invoke := func(method, path string, body any, user User) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.memoryAPI(w, r, user)
		return w
	}
	for _, path := range []string{"/api/memory", "/api/memory/source", "/api/memory/history", "/api/memory/preview"} {
		if w := invoke("GET", path, nil, User{Role: "operator"}); w.Code != 403 {
			t.Fatalf("operator %s status=%d", path, w.Code)
		}
	}
	base := memoryPreference(msg)
	base.Origin = "manual"
	base.Status = "confirmed"
	base.ConfirmedBy = "forged-human"
	base.ConfirmedAt = "1900-01-01T00:00:00Z"
	base.Content = "observer-memory-not-chat"
	base.Key = "observer-name"
	for _, origin := range []string{"scan", "explicit", "untrusted"} {
		bad := base
		bad.Origin = origin
		if w := invoke("POST", "/api/memory", map[string]any{"action": "save", "item": bad}, admin); w.Code != 400 {
			t.Fatalf("origin %s accepted: %d %s", origin, w.Code, w.Body.String())
		}
	}
	bad := base
	bad.Sources = append([]MemorySource(nil), base.Sources...)
	bad.Sources[0].Hash = "forged"
	if w := invoke("POST", "/api/memory", map[string]any{"action": "save", "item": bad}, admin); w.Code != 400 {
		t.Fatalf("forged hash accepted %d", w.Code)
	}
	bad = base
	bad.Subject = "other-person"
	if w := invoke("POST", "/api/memory", map[string]any{"action": "save", "item": bad}, admin); w.Code != 400 {
		t.Fatalf("forged subject accepted %d", w.Code)
	}
	w := invoke("POST", "/api/memory", map[string]any{"action": "save", "item": base}, admin)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Item MemoryItem `json:"item"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	item := result.Item
	if item.ConfirmedBy != "observer-admin" || item.ConfirmedAt == base.ConfirmedAt || item.Version != 1 {
		t.Fatalf("confirmation metadata trusted: %+v", item)
	}
	w = invoke("POST", "/api/memory", map[string]any{"action": "delete", "id": item.ID, "version": item.Version - 1}, admin)
	if w.Code != 400 {
		t.Fatalf("stale version accepted %d", w.Code)
	}
	state := call(t, s, "/api/state", nil, nil, "")
	if state.Code != 200 {
		t.Fatal("state handler unavailable", state.Code)
	}
	if strings.Contains(state.Body.String(), base.Content) {
		t.Fatal("memory body leaked through actual state HTTP")
	}
	if w = invoke("GET", "/api/memory/source?target=fixture-person&id=fixture-source", nil, admin); w.Code != 200 {
		t.Fatal("valid source contract", w.Code)
	}
	_ = s.store.change(func(st *State) error { st.Messages[0].Status = "revoked_by_other"; return nil })
	if w = invoke("GET", "/api/memory/source?target=fixture-person&id=fixture-source", nil, admin); w.Code != 400 {
		t.Fatal("revoked source readable", w.Code)
	}
	got, err := s.replyMemories(account, msg, 8)
	if err != nil || len(got) != 0 {
		t.Fatalf("revoked source injected %+v %v", got, err)
	}
	_ = s.store.change(func(st *State) error { st.Messages[0] = msg; st.Account.ID = "observer-other-account"; return nil })
	if _, err = s.memoryAction(account, item.ID, "delete", item.Version, "observer-admin"); err == nil {
		t.Fatal("old account mutation succeeded")
	}
	if got, err = s.memoryList("observer-other-account", MemoryFilter{}); err != nil || len(got) != 0 {
		t.Fatal("cross account memory list", got, err)
	}
	w = invoke("POST", "/api/memory", map[string]any{"action": "delete", "id": item.ID, "version": item.Version}, admin)
	if w.Code != 400 {
		t.Fatalf("API cross account item mutation succeeded %d", w.Code)
	}
	t.Log("HTTP 403 operator; 400 forged origin/hash/subject/stale version; confirmed_by overwritten; state HTTP no body; source revoke immediately blocks; account switch blocks mutation")
}
func TestMemorySecurityBotAndSensitivePersistence(t *testing.T) {
	s, account, msg := memoryFixture(t)
	bot := MemoryItem{Scope: "bot", Category: "state", Key: "observer-status", Content: "observer-bot-manual-state", Status: "confirmed", Origin: "manual", Sensitivity: "low"}
	item, err := s.putMemory(account, bot, "observer-admin", 0)
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.replyMemories(account, Message{Target: msg.Target, SenderID: msg.SenderID, Content: "observer-bot-manual-state"}, 8)
	if err != nil || len(list) != 1 || list[0].ID != item.ID {
		t.Fatal("manual bot unavailable", list, err)
	}
	bot.Key = "observer-bot-inferred"
	bot.Origin = "scan"
	if _, err = s.putMemory(account, bot, "scanner", 0); err == nil {
		t.Fatal("inferred bot saved")
	}
	personal := memoryPreference(msg)
	personal.Key = "observer-sensitive"
	personal.Content = "observer-sensitive-marker"
	personal.Origin = "manual"
	personal.Sensitivity = "sensitive"
	personal.Status = "confirmed"
	personal.Consent = true
	sens, err := s.putMemory(account, personal, "observer-admin", 0)
	if err != nil {
		t.Fatal(err)
	}
	current := msg
	current.Content = "observer-sensitive-marker"
	list, err = s.replyMemories(account, current, 8)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range list {
		if m.ID == sens.ID {
			t.Fatal("save consent granted model disclosure")
		}
	}
	sens.AllowModel = true
	sens, err = s.putMemory(account, sens, "observer-admin", sens.Version)
	if err != nil {
		t.Fatal(err)
	}
	list, err = s.replyMemories(account, current, 8)
	found := false
	for _, m := range list {
		found = found || m.ID == sens.ID
	}
	if err != nil || !found {
		t.Fatal("double-consented sensitive memory missing", list, err)
	}
	sens.ExpiresAt = time.Now().Add(-time.Minute).Format(time.RFC3339)
	sens, err = s.putMemory(account, sens, "observer-admin", sens.Version)
	if err != nil {
		t.Fatal(err)
	}
	list, _ = s.replyMemories(account, current, 8)
	for _, m := range list {
		if m.ID == sens.ID {
			t.Fatal("expired sensitive injected")
		}
	}
	if _, err = s.memoryAction(account, sens.ID, "delete", sens.Version, "observer-admin"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.store.db.QueryRow("SELECT count(*) FROM memory_sources WHERE account=? AND memory_id=?", account, sens.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("deleted sources retained", count, err)
	}
	changes, err := s.memoryHistory(account, sens.ID)
	if err != nil || len(changes) != 1 || changes[0].Before != nil || changes[0].After.Content != "" {
		t.Fatal("delete retained original body", changes, err)
	}
	t.Log("manual bot accepted; scanned bot rejected; sensitive consent independently enforced; expiry immediate; SQL deleted source count=0 and audit no body")
}
func TestMemorySecurityAuthenticatedRoutes(t *testing.T) {
	s := newServer(t, false)
	for _, path := range []string{"/api/memory", "/api/memory/source", "/api/memory/history", "/api/memory/preview", "/api/memory/scans", "/api/memory/trace"} {
		if w := call(t, s, path, nil, nil, ""); w.Code != http.StatusUnauthorized {
			t.Fatalf("unauth route %s status=%d", path, w.Code)
		}
	}
	w := call(t, s, "/api/auth/setup", map[string]string{"name": "observer-admin", "password": "observer-test-only-123"}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	admin := w.Result().Cookies()[0]
	w = call(t, s, "/api/action", actionRequest{Kind: "user", Name: "observer-operator", Password: "observer-test-only-456", Role: "operator", Enabled: true}, admin, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = call(t, s, "/api/auth/login", map[string]string{"name": "observer-operator", "password": "observer-test-only-456"}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	op := w.Result().Cookies()[0]
	for _, path := range []string{"/api/memory", "/api/memory/source", "/api/memory/history", "/api/memory/preview", "/api/memory/scans", "/api/memory/trace"} {
		if w := call(t, s, path, nil, op, ""); w.Code != http.StatusForbidden {
			t.Fatalf("operator route %s status=%d", path, w.Code)
		}
	}
	w = call(t, s, "/api/action", actionRequest{Kind: "settings", Group: "memory", Revision: s.store.snapshot().ConfigRevision, Values: map[string]any{"enabled": true}}, op, "")
	if w.Code != 400 && w.Code != 403 {
		t.Fatalf("operator changes memory settings status=%d", w.Code)
	}
	t.Log("actual mux all memory/source/history/preview/scans/trace returns 401 unauth and 403 operator; settings denied")
}
func TestMemorySecuritySensitiveSaveRequiresConsent(t *testing.T) {
	s, _, msg := memoryFixture(t)
	item := memoryPreference(msg)
	item.Origin = "manual"
	item.Status = "pending"
	item.Sensitivity = "sensitive"
	item.Consent = false
	item.Content = "observer-unconsented-sensitive"
	item.Key = "observer-unconsented"
	raw, _ := json.Marshal(map[string]any{"action": "save", "item": item})
	req := httptest.NewRequest("POST", "/api/memory", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.memoryAPI(w, req, User{Role: "admin", Name: "observer-admin"})
	var stored int
	if err := s.store.db.QueryRow("SELECT count(*) FROM memory_items WHERE account='fixture-account'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusBadRequest || stored != 0 {
		t.Fatalf("sensitive memory persisted without save consent: HTTP=%d DB rows=%d body=%s", w.Code, stored, w.Body.String())
	}
}
