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

func memoryFixture(t *testing.T) (*Server, string, Message) {
	t.Helper()
	s := newServer(t, true)
	if err := s.store.initMemory(); err != nil {
		t.Fatal(err)
	}
	m := Message{ID: "fixture-source", Target: "fixture-person", SenderID: "fixture-person", Kind: "text", Content: "记住：叫我小林", Time: time.Now().UTC().Format(time.RFC3339), Source: "wechat"}
	if err := s.store.change(func(st *State) error {
		st.Account.ID = "fixture-account"
		st.Contacts = append(st.Contacts, Contact{ID: m.Target, Kind: "friend"}, Contact{ID: "fixture-group@chatroom", Kind: "group"})
		st.Messages = []Message{m}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, "fixture-account", m
}
func memoryPreference(m Message) MemoryItem {
	return MemoryItem{Scope: "person", Subject: m.SenderID, Target: m.Target, Category: "name", Key: "address", Content: "称呼小林", Origin: "scan", Sensitivity: "low", Status: "pending", Sources: []MemorySource{memorySource(m)}}
}
func mustMemory(t *testing.T, s *Server, account string, item MemoryItem) MemoryItem {
	t.Helper()
	got, err := s.putMemory(account, item, "fixture-admin", 0)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func confirmMemory(t *testing.T, s *Server, account string, item MemoryItem) MemoryItem {
	t.Helper()
	got, err := s.memoryAction(account, item.ID, "confirm", item.Version, "fixture-admin")
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func TestMemoryEncryptionRestartAndAudit(t *testing.T) {
	s, account, msg := memoryFixture(t)
	item := confirmMemory(t, s, account, mustMemory(t, s, account, memoryPreference(msg)))
	var raw []byte
	if err := s.store.db.QueryRow("SELECT body FROM memory_items WHERE account=? AND id=?", account, item.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(item.Content)) {
		t.Fatal("正文未加密")
	}
	if err := s.store.db.QueryRow("SELECT body FROM memory_changes WHERE account=? LIMIT 1", account).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(item.Content)) {
		t.Fatal("审计正文未加密")
	}
	state, _ := json.Marshal(s.store.snapshot())
	if bytes.Contains(state, []byte(item.Content)) {
		t.Fatal("正文泄露到state")
	}
	reopened, err := openStore(s.cfg.DataDir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.db.Close()
	next := &Server{store: reopened}
	list, err := next.memoryList(account, MemoryFilter{})
	if err != nil || len(list) != 1 || list[0].Content != item.Content {
		t.Fatalf("重启丢失记忆: %v %+v", err, list)
	}
	history, err := next.memoryHistory(account, item.ID)
	if err != nil || len(history) != 2 || history[0].Actor != "fixture-admin" {
		t.Fatalf("审计缺失: %v %+v", err, history)
	}
	var count int
	_ = s.store.db.QueryRow("SELECT count(*) FROM memory_sources WHERE account=? AND memory_id=?", account, item.ID).Scan(&count)
	if count != 1 {
		t.Fatal("来源索引丢失")
	}
}
func TestMemoryIsolationExpiryAndSourceInvalidation(t *testing.T) {
	s, account, msg := memoryFixture(t)
	item := confirmMemory(t, s, account, mustMemory(t, s, account, memoryPreference(msg)))
	current := msg
	check := func(want int) {
		t.Helper()
		list, err := s.replyMemories(account, current, 8)
		if err != nil || len(list) != want {
			t.Fatalf("记忆隔离错误 want=%d got=%+v err=%v", want, list, err)
		}
	}
	check(1)
	current.Target = "fixture-group@chatroom"
	check(0)
	current = msg
	current.SenderID = "fixture-other"
	check(0)
	current = msg
	_ = s.store.change(func(st *State) error { st.Messages[0].Content = "原文已变化"; return nil })
	check(0)
	_ = s.store.change(func(st *State) error { st.Messages[0] = msg; st.Messages[0].Status = "revoked_by_other"; return nil })
	check(0)
	if _, err := s.memorySourceMessage(account, msg.Target, msg.ID); err == nil {
		t.Fatal("撤回来源仍可读")
	}
	_ = s.store.change(func(st *State) error { st.Messages[0] = msg; return nil })
	item.ExpiresAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if _, err := s.putMemory(account, item, "fixture-admin", item.Version); err != nil {
		t.Fatal(err)
	}
	check(0)
	_ = s.store.change(func(st *State) error { st.Account.ID = "fixture-other-account"; return nil })
	if _, err := s.memoryList(account, MemoryFilter{}); err == nil {
		t.Fatal("旧账号可访问记忆")
	}
	if _, err := s.putMemory(account, memoryPreference(msg), "fixture-admin", 0); err == nil {
		t.Fatal("旧账号任务仍可写")
	}
	list, err := s.memoryList("fixture-other-account", MemoryFilter{})
	if err != nil || len(list) != 0 {
		t.Fatal("跨账号数据泄露")
	}
}
func TestMemoryConflictVersionRejectionAndDeletion(t *testing.T) {
	s, account, msg := memoryFixture(t)
	a := confirmMemory(t, s, account, mustMemory(t, s, account, memoryPreference(msg)))
	if _, err := s.memoryAction(account, a.ID, "reject", a.Version-1, "fixture-admin"); err == nil {
		t.Fatal("接受旧版本")
	}
	other := memoryPreference(msg)
	other.Origin = "manual"
	other.Content = "称呼小张"
	b := mustMemory(t, s, account, other)
	if b.Status != "conflicted" {
		t.Fatal("冲突未标记")
	}
	list, _ := s.replyMemories(account, msg, 8)
	if len(list) != 0 {
		t.Fatal("冲突仍注入")
	}
	b = confirmMemory(t, s, account, b)
	all, _ := s.memoryList(account, MemoryFilter{})
	for _, m := range all {
		if m.ID == a.ID && m.Status != "superseded" {
			t.Fatal("冲突未裁决")
		}
	}
	if _, err := s.memoryAction(account, b.ID, "delete", b.Version, "fixture-admin"); err != nil {
		t.Fatal(err)
	}
	changes, err := s.memoryHistory(account, b.ID)
	if err != nil || len(changes) != 1 || changes[0].Before != nil || changes[0].After.Content != "" {
		t.Fatal("删除仍保留正文审计", changes, err)
	}
	if _, err := s.putMemory(account, other, "fixture-admin", 0); err == nil {
		t.Fatal("删除候选复活")
	}
	scan := memoryPreference(msg)
	scan.Key = "tone"
	scan.Content = "简洁沟通"
	r := mustMemory(t, s, account, scan)
	r, err = s.memoryAction(account, r.ID, "reject", r.Version, "fixture-admin")
	if err != nil {
		t.Fatal(err)
	}
	scan.Content = "喜欢简洁回复"
	again := mustMemory(t, s, account, scan)
	if again.ID != r.ID || again.Status != "rejected" {
		t.Fatal("拒绝项重复出现")
	}
}
func TestMemoryExplicitAndScanAuthority(t *testing.T) {
	s, account, msg := memoryFixture(t)
	scan := memoryPreference(msg)
	scan.Status = "confirmed"
	item := mustMemory(t, s, account, scan)
	if item.Status != "pending" {
		t.Fatal("扫描自行确认")
	}
	explicit := memoryPreference(msg)
	explicit.Key = "explicit-address"
	explicit.Origin = "explicit"
	explicit.Status = "confirmed"
	if got := mustMemory(t, s, account, explicit); got.Status != "confirmed" {
		t.Fatal("明确请求未生效")
	}
	for _, bad := range []MemoryItem{{Scope: "group", Subject: "fixture-group@chatroom", Target: "fixture-group@chatroom", Origin: "explicit", Category: "name", Key: "x", Content: "偏好", Sources: explicit.Sources}, {Scope: "bot", Origin: "scan", Category: "state", Key: "status", Content: "下班"}} {
		if _, err := s.putMemory(account, bad, "fixture-admin", 0); err == nil {
			t.Fatal("未授权直接确认")
		}
	}
	sensitive := memoryPreference(msg)
	sensitive.Key = "sensitive"
	sensitive.Origin = "manual"
	sensitive.Sensitivity = "sensitive"
	sensitive.Status = "confirmed"
	if _, err := s.putMemory(account, sensitive, "fixture-admin", 0); err == nil {
		t.Fatal("未同意敏感记忆被确认")
	}
	sensitive.Consent = true
	sens := mustMemory(t, s, account, sensitive)
	list, _ := s.replyMemories(account, msg, 8)
	for _, m := range list {
		if m.ID == sens.ID {
			t.Fatal("仅保存同意被视为模型披露同意")
		}
	}
	sens.AllowModel = true
	sens, err := s.putMemory(account, sens, "fixture-admin", sens.Version)
	if err != nil {
		t.Fatal(err)
	}
	list, _ = s.replyMemories(account, msg, 8)
	found := false
	for _, m := range list {
		if m.ID == sens.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("敏感记忆已授权模型披露但未注入")
	}
	scan2 := memoryPreference(msg)
	scan2.Key = "untrusted-model-consent"
	scan2.AllowModel = true
	untrusted := mustMemory(t, s, account, scan2)
	if untrusted.AllowModel {
		t.Fatal("扫描伪造模型披露授权")
	}

}
func TestMemoryAdminAPI(t *testing.T) {
	s, account, msg := memoryFixture(t)
	_ = account
	request := func(method, path string, body any, u User) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.memoryAPI(w, r, u)
		return w
	}
	for _, u := range []User{{}, {Role: "operator"}} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			if w := request(method, "/api/memory", nil, u); w.Code != 403 {
				t.Fatal("非管理员越权")
			}
		}
	}
	admin := User{Name: "fixture-admin", Role: "admin"}
	item := memoryPreference(msg)
	if w := request("POST", "/api/memory", map[string]any{"action": "save", "item": item}, admin); w.Code != 400 {
		t.Fatal("HTTP伪造扫描来源")
	}
	item.Origin = "manual"
	w := request("POST", "/api/memory", map[string]any{"action": "save", "item": item}, admin)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"item"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("GET", "/api/memory?target=fixture-person", nil, admin)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request("GET", "/api/memory/source?target=fixture-other&id=fixture-source", nil, admin); w.Code != 400 {
		t.Fatal("来源跨会话")
	}
}

func TestMemoryPreferenceRelevanceAndUntrustedSensitiveConsent(t *testing.T) {
	s, account, msg := memoryFixture(t)
	unrelated := MemoryItem{Scope: "person", Subject: msg.SenderID, Target: msg.Target, Category: "preference", Key: "饮食", Content: "喜欢番茄", Status: "confirmed", Origin: "manual"}
	item := mustMemory(t, s, account, unrelated)
	list, err := s.replyMemories(account, msg, 8)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range list {
		if m.ID == item.ID {
			t.Fatal("不相关饮食偏好被注入")
		}
	}
	msg.Content = "聊聊饮食吧"
	list, err = s.replyMemories(account, msg, 8)
	if err != nil || len(list) != 1 {
		t.Fatal("相关偏好未检索", err, list)
	}
	sensitive := memoryPreference(s.store.snapshot().Messages[0])
	sensitive.Sensitivity = "sensitive"
	sensitive.Consent = true
	sensitive.AllowModel = true
	if _, err = s.putMemory(account, sensitive, "scanner", 0); err == nil {
		t.Fatal("模型伪造敏感内容保存同意")
	}
}
