// 主要功能：回归验证回复上下文、真实模型请求及异步任务的身份和证据边界。
package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/plugins/contextcleaner"
	"github.com/we-clay/we-clay/sdk/control"
)

func replyRegressionCheck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func replyRegressionFixture(t *testing.T) (*Server, Message) {
	t.Helper()
	s := newServer(t, true)
	m := Message{ID: "reply-regression-current", Target: "reply-regression-person", SenderID: "reply-regression-person", Kind: "text", Content: "本轮问题", Time: time.Now().Format(time.RFC3339), Status: "sent", Source: "wechat"}
	if e := s.store.change(func(st *State) error {
		st.Account.ID = "reply-regression-account"
		st.Contacts = append(st.Contacts, Contact{ID: m.Target, Kind: "friend"})
		st.Settings["reply.enabled"] = false
		st.Messages = []Message{m}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	return s, m
}
func TestReplyRegressionDeletedCurrent(t *testing.T) {
	s, m := replyRegressionFixture(t)
	st := s.store.snapshot()
	conv, h := buildReplyContext(st, m, time.Now())
	q := ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Kind: "chat", Source: "wechat", Prompt: m.Content, Target: m.Target, Conversation: &conv, Context: h}
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages = nil; return nil }))
	_, err := s.refreshReplyContext(q)
	if err == nil {
		t.Fatal("已删除的当前消息仍通过调用前校验")
	}
}
func TestReplyRegressionRevokedQuote(t *testing.T) {
	s, m := replyRegressionFixture(t)
	original := m
	original.ID = "reply-regression-original"
	original.Content = "唯一撤回正文标记"
	original.Status = "revoked_by_other"
	m.Kind = "quote"
	m.Rich = &RichMessage{QuoteID: original.ID, Quote: original.Content}
	st := s.store.snapshot()
	st.Messages = []Message{original, m}
	conv, h := buildReplyContext(st, m, time.Now())
	q := ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Prompt: m.Content, Conversation: &conv, Context: h}
	for _, turn := range ai.ConversationMessages(q) {
		if strings.Contains(turn.Text, original.Content) {
			t.Fatal("已知撤回引用原文仍进入实际序列化消息")
		}
	}
}
func TestReplyRegressionRevokeBeforeOriginal(t *testing.T) {
	s, m := replyRegressionFixture(t)
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages = nil; return nil }))
	notice := Message{ID: "reply-regression-revoke", Target: m.Target, Kind: "system", Content: `<sysmsg type="revokemsg"><revokemsg><newmsgid>reply-regression-current</newmsgid><replacemsg>对方撤回了一条消息</replacemsg></revokemsg></sysmsg>`}
	if e := s.Record(notice); e != nil {
		t.Fatal(e)
	}
	if e := s.Record(m); e != nil {
		t.Fatal(e)
	}
	for _, v := range s.store.snapshot().Messages {
		if v.ID == m.ID && (contextAvailable(v) || v.Content != m.Content) {
			t.Fatal("先撤回后入库丢失原文或恢复为可注入状态")
		}
	}
}
func TestReplyRegressionHistoricalMessageBudget(t *testing.T) {
	s, m := replyRegressionFixture(t)
	st := s.store.snapshot()
	old := m
	old.ID = "large-old"
	old.Content = strings.Repeat("long history ", 300)
	st.Messages = []Message{old, m}
	conv, h := buildReplyContext(st, m, time.Now())
	q := ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Prompt: m.Content, Target: m.Target, Conversation: &conv, Context: h}
	out, _, err := materializeSafety(st, q, Model{Context: 10, ContextTokens: 2048}, 256)
	if err != nil {
		t.Fatal("本轮短消息可容纳但单条旧历史过大导致整个请求失败而非裁剪", err)
	}
	if len(out.Context) != 0 || out.Conversation.Current.ID != m.ID || out.Conversation.Current.Text != m.Content {
		t.Fatal("旧历史应裁空，并完整保留本轮消息", out)
	}
}
func TestReplyRegressionProviderFallbackBudget(t *testing.T) {
	s := newServer(t, false)
	var mu sync.Mutex
	counts := map[string]int{}
	currentCount := map[string]int{}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Model    string                           `json:"model"`
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
			http.Error(w, "无效请求", 400)
			return
		}
		mu.Lock()
		for _, turn := range b.Messages {
			if strings.Contains(turn.Content, "history-fixture-") {
				counts[b.Model] += strings.Count(turn.Content, "history-fixture-")
			}
			if strings.Contains(turn.Content, "current-unique-fixture") {
				currentCount[b.Model]++
			}
		}
		mu.Unlock()
		if b.Model == "first" {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":{"message":"retry"}}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer remote.Close()
	replyRegressionCheck(t, s.store.putSecret("probe-vendor", "fixture-probe-key"))
	m := Message{ID: "probe-current", Target: "probe-target", SenderID: "probe-target", Kind: "text", Content: "current-unique-fixture", Status: "sent", Time: time.Now().Format(time.RFC3339), Source: "wechat"}
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Account.ID = "probe-account"
		st.Models = []Model{{ID: "first", VendorID: "probe-vendor", Name: "first", Model: "first", Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}, Context: 1}, {ID: "second", VendorID: "probe-vendor", Name: "second", Model: "second", Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}, Context: 3}}
		st.Pipeline = []string{"first", "second"}
		st.Settings["routing.fallback"] = true
		st.Messages = nil
		for i := 0; i < 4; i++ {
			old := m
			old.ID = fmt.Sprintf("history-id-%d", i)
			old.Content = fmt.Sprintf("history-fixture-%d", i)
			st.Messages = append(st.Messages, old)
		}
		st.Messages = append(st.Messages, m)
		return nil
	}))
	st := s.store.snapshot()
	conv, h := buildReplyContext(st, m, time.Now())
	id, err := s.queue(ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Kind: "chat", Source: "dashboard", System: "reply", Prompt: m.Content, Target: m.Target, Conversation: &conv, Context: h})
	if err != nil {
		t.Fatal(err)
	}
	task := waitModelTask(t, s, id)
	if task.Status != "completed" {
		t.Fatal(task)
	}
	mu.Lock()
	defer mu.Unlock()
	if counts["first"] != 1 || counts["second"] != 3 || currentCount["first"] != 1 || currentCount["second"] != 1 {
		t.Fatalf("实际HTTP fallback预算或本轮去重错误 history=%v current=%v", counts, currentCount)
	}
	t.Logf("实际HTTP history=%v current=%v", counts, currentCount)
}
func TestReplyRegressionAccountRoundTripOldWorker(t *testing.T) {
	s := newServer(t, false)
	began := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		began <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"old worker result"}}]}`)
	}))
	defer func() { unblock(); remote.Close() }()
	replyRegressionCheck(t, s.store.putSecret("roundtrip-vendor", "fixture-probe-key"))
	m := Message{ID: "roundtrip-current", Target: "roundtrip-target", SenderID: "roundtrip-target", Kind: "text", Content: "roundtrip request", Status: "sent", Source: "wechat", Time: time.Now().Format(time.RFC3339)}
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Account.ID = "roundtrip-a"
		st.Models = []Model{{ID: "roundtrip-model", VendorID: "roundtrip-vendor", Name: "roundtrip-model", Model: "roundtrip-model", Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}}}
		st.Pipeline = []string{"roundtrip-model"}
		st.Messages = []Message{m}
		return nil
	}))
	st := s.store.snapshot()
	conv, h := buildReplyContext(st, m, time.Now())
	id, err := s.queue(ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Kind: "chat", Source: "dashboard", System: "reply", Prompt: m.Content, Target: m.Target, Conversation: &conv, Context: h})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-began:
	case <-time.After(3 * time.Second):
		t.Fatal("探针未进入实际HTTP")
	}
	if err = s.SetAccount(Account{ID: "roundtrip-b"}); err != nil {
		t.Fatal(err)
	}
	if err = s.SetAccount(Account{ID: "roundtrip-a"}); err != nil {
		t.Fatal(err)
	}
	unblock()
	task := waitModelTask(t, s, id)
	if task.Status == "completed" {
		t.Fatal("账号A→B→A后旧worker仍完成且结果被接受")
	}
}
func TestReplyRegressionScanCancelResumeWorkerFence(t *testing.T) {
	s := newServer(t, false)
	began := make(chan int, 4)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var mu sync.Mutex
	calls := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		began <- n
		select {
		case <-r.Context().Done():
			return
		case <-release:
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"items":[]}`}}}})
	}))
	defer func() { unblock(); remote.Close() }()
	replyRegressionCheck(t, s.store.putSecret("scan-probe-vendor", "fixture-probe-key"))
	m := Message{ID: "scan-race-source", Target: "scan-race-target", SenderID: "scan-race-target", Kind: "text", Content: "记住：叫我小林", Status: "sent", Source: "wechat", Time: time.Now().Format(time.RFC3339)}
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Account.ID = "scan-race-account"
		st.Models = []Model{{ID: "scan-probe-model", VendorID: "scan-probe-vendor", Name: "scan-probe-model", Model: "scan-probe-model", Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}}}
		st.Pipeline = []string{"scan-probe-model"}
		st.Settings["memory.scan_enabled"] = true
		st.Settings["reply.enabled"] = false
		st.Settings["memory.direct_enabled"] = false
		st.Contacts = append(st.Contacts, Contact{ID: m.Target, Kind: "friend"})
		st.Messages = []Message{m}
		return nil
	}))
	v, err := s.startMemoryScan("scan-race-account", memoryScanRequest{Target: m.Target})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-began:
	case <-time.After(3 * time.Second):
		t.Fatal("探针未启动")
	}
	if _, err = s.cancelMemoryScan("scan-race-account", v.ID, m.Target); err != nil {
		t.Fatal(err)
	}
	if _, err = s.startMemoryScan("scan-race-account", memoryScanRequest{Target: m.Target, ID: v.ID}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.store.mu.Lock()
		got, e := s.store.memoryScanLocked("scan-race-account", v.ID)
		s.store.mu.Unlock()
		if e != nil {
			t.Fatal(e)
		}
		if got.Status == "failed" {
			t.Fatal("被取消的旧scanner将刚续扫的新worker状态覆盖为failed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	unblock()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.store.mu.Lock()
		got, e := s.store.memoryScanLocked("scan-race-account", v.ID)
		s.store.mu.Unlock()
		replyRegressionCheck(t, e)
		if got.Status == "completed" {
			return
		}
		if got.Status == "failed" {
			t.Fatal("续扫未完成", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("续扫未在预期时间内完成")

}
func TestReplyRegressionQuoteBudgetPriority(t *testing.T) {
	s, m := replyRegressionFixture(t)
	prev := m
	prev.ID = "quote-neighbor"
	prev.Content = strings.Repeat("p", 900)
	original := m
	original.ID = "quote-needed"
	original.Content = "required-quote-unique" + strings.Repeat("q", 900)
	m.Kind = "quote"
	m.Rich = &RichMessage{QuoteID: original.ID, Quote: original.Content}
	st := s.store.snapshot()
	st.Messages = []Message{prev, original, m}
	conv, h := buildReplyContext(st, m, time.Now())
	q := ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Prompt: m.Content, Target: m.Target, Conversation: &conv, Context: h}
	out, _, err := materializeSafety(st, q, Model{ContextTokens: ai.ConversationSize(q) - 1}, 256)
	if err != nil {
		t.Fatal("邻近消息挤掉必要引用导致短本轮报错", err)
	}
	found := false
	for _, v := range out.Conversation.References {
		if v.ID == original.ID {
			found = true
		}
	}
	if !found || len(out.Conversation.References) != 1 {
		t.Fatal("预算裁剪应移除邻居并保留本轮必要的被引用原消息")
	}
}
func TestReplyRegressionHTTPInflightReferenceRevoke(t *testing.T) {
	s := newServer(t, false)
	began := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		began <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"stale reference result"}}]}`)
	}))
	defer func() { unblock(); remote.Close() }()
	replyRegressionCheck(t, s.store.putSecret("revoke-vendor", "fixture-probe-key"))
	m := Message{ID: "revoke-current", Target: "revoke-target", SenderID: "revoke-target", Kind: "quote", Content: "引用问题", Status: "sent", Source: "wechat", Time: time.Now().Format(time.RFC3339), Rich: &RichMessage{QuoteID: "revoke-original", Quote: "引用来源"}}
	original := m
	original.ID = "revoke-original"
	original.Kind = "text"
	original.Content = "引用来源"
	original.Rich = nil
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Account.ID = "revoke-account"
		st.Models = []Model{{ID: "revoke-model", VendorID: "revoke-vendor", Name: "revoke-model", Model: "revoke-model", Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}}}
		st.Pipeline = []string{"revoke-model"}
		st.Messages = []Message{original, m}
		return nil
	}))
	st := s.store.snapshot()
	conv, h := buildReplyContext(st, m, time.Now())
	id, err := s.queue(ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Kind: "chat", Source: "dashboard", System: "reply", Prompt: m.Content, Target: m.Target, Conversation: &conv, Context: h})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-began:
	case <-time.After(3 * time.Second):
		t.Fatal("探针未进入实际HTTP")
	}
	replyRegressionCheck(t, s.store.change(func(st *State) error { st.Messages[0].Status = "revoked_by_other"; return nil }))
	unblock()
	task := waitModelTask(t, s, id)
	if task.Status == "completed" {
		t.Fatal("推理期间引用撤回后仍接受结果")
	}
	t.Log("实际HTTP后已丢弃失效来源结果", task.Status, task.Error)
}
func TestReplyRegressionRecordAccountRace(t *testing.T) {
	s, m := replyRegressionFixture(t)
	m.ID = "race-incoming"
	m.Kind = "link"
	m.Articles = []MessageArticle{{Title: "fixture", URL: "https://example.com/fixture", ThumbID: "fixture-id", ThumbKey: "fixture-key"}}
	before := s.store.db.Stats().WaitCount
	tx, err := s.store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	recordDone := make(chan error, 1)
	go func() { recordDone <- s.Record(m) }()
	deadline := time.Now().Add(time.Second)
	for s.store.db.Stats().WaitCount <= before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.store.db.Stats().WaitCount <= before {
		tx.Rollback()
		t.Fatal("探针未阻塞入站缩略图存储")
	}
	switchDone := make(chan error, 1)
	go func() { switchDone <- s.SetAccount(Account{ID: "race-account-b"}) }()
	for s.store.db.Stats().WaitCount <= before+1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	tx.Rollback()
	if err = <-switchDone; err != nil {
		t.Fatal(err)
	}
	<-recordDone
	st := s.store.snapshot()
	for _, v := range st.Messages {
		if v.ID == m.ID {
			t.Fatal("账号切换期间旧账号入站消息已写入新账号归档")
		}
	}
}

func TestReplyRegressionInteractionAndSpeakerMetadata(t *testing.T) {
	s, base := replyRegressionFixture(t)
	st := s.store.snapshot()
	group := "fixture-regression-group@chatroom"
	st.Account.ID = "fixture-regression-self"
	st.Contacts = append(st.Contacts, Contact{ID: group, Kind: "group"})
	memberA := base
	memberA.ID = "fixture-group-member-a-message"
	memberA.Target = group
	memberA.SenderID = "fixture-member-a"
	memberA.Sender = "同名成员"
	memberA.Content = "相同正文也应保留不同消息"
	memberB := memberA
	memberB.ID = "fixture-group-member-b-message"
	memberB.SenderID = "fixture-member-b"
	manual := memberA
	manual.ID = "fixture-manual-message"
	manual.Self = true
	manual.Source = "dashboard"
	manual.SenderID = st.Account.ID
	manual.Content = "fixture-manual-unique"
	pat := memberA
	pat.ID = "fixture-pat-event"
	pat.Kind = "system"
	pat.Content = "fixture-pat-unique"
	pat.Interaction = &contextcleaner.Interaction{Kind: "pat", ActorID: memberA.SenderID, TargetID: st.Account.ID, Verified: true}
	system := memberA
	system.ID = "fixture-system-event"
	system.Kind = "system"
	system.Content = "fixture-internal-unique"
	revoked := memberA
	revoked.ID = "fixture-revoked-message"
	revoked.Status = "revoked_by_other"
	revoked.Content = "fixture-revoked-unique"
	current := memberB
	current.ID = "fixture-current-metadata"
	current.Content = "fixture-current-metadata-unique"
	current.Mentions = []string{st.Account.ID, memberA.SenderID}
	current.MentionAll = true
	st.Messages = []Message{memberA, memberB, manual, pat, system, revoked, current, current}
	conv, history := buildReplyContext(st, current, time.Now())
	q := ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Kind: "chat", Prompt: current.Content, Target: group, Conversation: &conv, Context: history}
	out, _, err := materializeSafety(st, q, Model{Context: 20, ContextTokens: 16000}, 256)
	replyRegressionCheck(t, err)
	if out.Conversation.Scene != "group" || out.Conversation.Current.SenderID != memberB.SenderID || !out.Conversation.Current.MentionAll || len(out.Conversation.Current.Mentions) != 2 || out.Conversation.Current.Mentions[0] != st.Account.ID || out.Conversation.Current.Mentions[1] != memberA.SenderID {
		t.Fatal("本轮群身份或@关系丢失", out.Conversation)
	}
	seen := map[string]contextcleaner.Message{}
	for _, v := range out.Context {
		seen[v.ID] = v
	}
	if seen[memberA.ID].SenderID != memberA.SenderID || seen[memberB.ID].SenderID != memberB.SenderID || seen[memberA.ID].Sender != seen[memberB.ID].Sender {
		t.Fatal("同名成员被合并或身份串用", seen)
	}
	if seen[manual.ID].Role != "assistant" || seen[manual.ID].ActorType != "self_operator" || seen[manual.ID].SenderID != st.Account.ID {
		t.Fatal("人工发送未标为自身助手角色", seen[manual.ID])
	}
	interaction := seen[pat.ID].Interaction
	if interaction == nil || interaction.Kind != "pat" || !interaction.Verified || interaction.ActorID != memberA.SenderID || interaction.TargetID != st.Account.ID {
		t.Fatal("拍一拍参与者丢失", interaction)
	}
	if _, ok := seen[system.ID]; ok {
		t.Fatal("内部系统通知进入近期上下文")
	}
	if _, ok := seen[revoked.ID]; ok {
		t.Fatal("撤回消息进入近期上下文")
	}
	currentOccurrences := 0
	for _, turn := range ai.ConversationMessages(out) {
		currentOccurrences += strings.Count(turn.Text, current.Content)
		if strings.Contains(turn.Text, system.Content) || strings.Contains(turn.Text, revoked.Content) {
			t.Fatal("序列化请求包含内部通知或撤回原文")
		}
	}
	if currentOccurrences != 1 {
		t.Fatalf("本轮消息应只发送一次，实际=%d", currentOccurrences)
	}
}

func TestReplyRegressionDeletedSourceReplay(t *testing.T) {
	s, msg := replyRegressionFixture(t)
	account := s.store.snapshot().Account.ID
	item, err := s.putMemory(account, MemoryItem{Scope: "person", Subject: msg.SenderID, Target: msg.Target, Category: "name", Key: "fixture-deleted-source", Content: "fixture-memory-from-deleted-source", Status: "confirmed", Origin: "manual", Sources: []MemorySource{memorySource(msg)}}, "fixture-admin", 0)
	replyRegressionCheck(t, err)
	initial, err := s.replyMemories(account, msg, 8)
	replyRegressionCheck(t, err)
	if len(initial) != 1 || initial[0].ID != item.ID {
		t.Fatal("来源删除前，探针记忆应有效", initial)
	}
	w := call(t, s, "/api/action", actionRequest{Kind: "delete-message", Target: msg.Target, ID: msg.ID}, nil, "")
	if w.Code != 200 {
		t.Fatal("来源删除失败", w.Code, w.Body.String())
	}
	check := func(server *Server) {
		t.Helper()
		replyRegressionCheck(t, server.Record(msg))
		for _, m := range server.store.snapshot().Messages {
			if m.ID == msg.ID && m.Target == msg.Target {
				t.Fatal("已删除来源通过事件重放复活", m)
			}
		}

		late := msg
		late.ID = "fixture-late-deleted-quote"
		late.Kind = "quote"
		late.Content = "fixture-late-quote-current"
		late.Rich = &RichMessage{QuoteID: msg.ID, Quote: msg.Content}
		replyRegressionCheck(t, server.Record(late))
		st := server.store.snapshot()
		quoteFound := false
		for _, m := range st.Messages {
			if m.ID == late.ID && m.Target == late.Target {
				quoteFound = true
				if m.Rich == nil || !m.Rich.QuoteUnavailable || m.Rich.Quote != "" {
					t.Fatal("晚到引用保留了已删除来源正文", m)
				}
				conv, history := buildReplyContext(st, m, time.Now())
				q := ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Prompt: m.Content, Target: m.Target, Conversation: &conv, Context: history}
				for _, turn := range ai.ConversationMessages(q) {
					if strings.Contains(turn.Text, msg.Content) {
						t.Fatal("晚到引用仍将已删除正文发送给模型")
					}
				}
			}
		}
		if !quoteFound {
			t.Fatal("晚到引用本身不应被删除")
		}
		items, e := server.replyMemories(account, msg, 8)
		replyRegressionCheck(t, e)
		for _, m := range items {
			if m.ID == item.ID {
				t.Fatal("已删除来源使记忆重新注入", m)
			}
		}
	}
	check(s)
	restarted, err := Start(context.Background(), control.Config{Address: "127.0.0.1:0", DataDir: s.cfg.DataDir, Mock: true, Disabled: true}, forbiddenHost{t})
	replyRegressionCheck(t, err)
	t.Cleanup(restarted.Close)
	check(restarted)
	other := msg
	other.Target = "fixture-other-target"
	other.SenderID = other.Target
	replyRegressionCheck(t, restarted.Record(other))
	found := false
	for _, m := range restarted.store.snapshot().Messages {
		if m.ID == other.ID && m.Target == other.Target {
			found = true
		}
	}
	if !found {
		t.Fatal("来源删除墓碑错误阻止了另一会话的同ID消息")
	}
}

type replyRegressionHost func(context.Context, control.Command) (any, error)

func (h replyRegressionHost) Execute(ctx context.Context, c control.Command) (any, error) {
	return h(ctx, c)
}
func TestReplyRegressionSplitReplySourceRevoke(t *testing.T) {
	s := newServer(t, false)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "first part\n\nsecond part"}}}})
	}))
	defer remote.Close()
	replyRegressionCheck(t, s.store.putSecret("audit-provider", "fixture-probe-key"))
	m := Message{ID: "audit-current", Target: "audit-target", SenderID: "audit-target", Kind: "text", Content: "audit question", Status: "sent", Source: "wechat", Time: time.Now().Format(time.RFC3339)}
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Account.ID = "audit-account"
		st.Account.Status = "online"
		st.Models = []Model{{ID: "audit-model", VendorID: "audit-provider", Name: "audit-model", Model: "audit-model", Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}}}
		st.Pipeline = []string{"audit-model"}
		st.Messages = []Message{m}
		st.Contacts = []Contact{{ID: m.Target, Kind: "friend", Mode: "auto"}}
		st.Settings["reply.enabled"] = true
		st.Settings["reply.targets"] = m.Target
		st.Settings["reply.interval_ms"] = float64(0)
		return nil
	}))
	var sends atomic.Int32
	s.host = replyRegressionHost(func(ctx context.Context, c control.Command) (any, error) {
		if c.Operation != "send" {
			return nil, fmt.Errorf("探针不允许其他操作")
		}
		n := sends.Add(1)
		if n == 1 {
			if err := s.store.change(func(st *State) error {
				for i := range st.Messages {
					if st.Messages[i].ID == m.ID {
						st.Messages[i].Status = "revoked_by_other"
					}
				}
				return nil
			}); err != nil {
				return nil, err
			}
		}
		return map[string]string{"id": fmt.Sprintf("audit-send-%d", n)}, nil
	})
	st := s.store.snapshot()
	conv, h := buildReplyContext(st, m, time.Now())
	id, e := s.queue(ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Kind: "chat", Source: "wechat", System: "reply", Prompt: m.Content, Target: m.Target, Conversation: &conv, Context: h})
	if e != nil {
		t.Fatal(e)
	}
	task := waitModelTask(t, s, id)
	if sends.Load() > 1 {
		t.Fatal("分段回复第一条发送后来源撤回，第二条仍然外发", sends.Load(), task.Status)
	}
	if task.Status != "failed" {
		t.Fatal("来源失效后任务应标为失败", task)
	}
	if sends.Load() != 1 {
		t.Fatal("探针未达到首次发送", task)
	}
}

func TestReplyRegressionUnverifiedQuoteRevokedDuringInference(t *testing.T) {
	s := newServer(t, false)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	quoteBody := "fixture-unverified-quote-body"
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			http.Error(w, "无法读取请求", 400)
			return
		}
		if !strings.Contains(string(raw), quoteBody) {
			t.Error("探针调用前应包含未验证的内嵌引用正文")
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "fixture-result-from-unverified-quote"}}}})
	}))
	defer func() { unblock(); remote.Close() }()
	replyRegressionCheck(t, s.store.putSecret("fixture-unverified-quote-vendor", "fixture-probe-key"))
	current := Message{ID: "fixture-unverified-quote-current", Target: "fixture-unverified-quote-target", SenderID: "fixture-unverified-quote-target", Kind: "quote", Content: "请看这个引用", Status: "sent", Source: "wechat", Time: time.Now().Format(time.RFC3339), Rich: &RichMessage{QuoteID: "fixture-unarchived-original", Quote: quoteBody}}
	replyRegressionCheck(t, s.store.change(func(st *State) error {
		st.Account.ID = "fixture-unverified-quote-account"
		st.Messages = []Message{current}
		st.Contacts = []Contact{{ID: current.Target, Kind: "friend"}}
		st.Models = []Model{{ID: "fixture-unverified-quote-model", VendorID: "fixture-unverified-quote-vendor", Name: "fixture-unverified-quote-model", Model: "fixture-unverified-quote-model", Provider: "openai", Endpoint: remote.URL, Enabled: true, Capabilities: []string{"chat"}}}
		st.Pipeline = []string{"fixture-unverified-quote-model"}
		st.Settings["reply.enabled"] = false
		return nil
	}))
	st := s.store.snapshot()
	conv, history := buildReplyContext(st, current, time.Now())
	if conv.Current.Quote == nil || conv.Current.Quote.Verified || len(conv.References) != 0 {
		t.Fatal("探针必须使用未归档原文的未验证引用", conv)
	}
	id, err := s.queue(ai.Request{AccountID: st.Account.ID, AccountEpoch: st.AccountEpoch, Kind: "chat", Source: "dashboard", System: "reply", Prompt: current.Content, Target: current.Target, Conversation: &conv, Context: history})
	replyRegressionCheck(t, err)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("引用请求未进入模型HTTP")
	}
	notice := Message{ID: "fixture-unverified-quote-revoke", Target: current.Target, Kind: "system", Content: `<sysmsg type="revokemsg"><revokemsg><newmsgid>fixture-unarchived-original</newmsgid><replacemsg>对方撤回了一条消息</replacemsg></revokemsg></sysmsg>`}
	replyRegressionCheck(t, s.Record(notice))
	for _, m := range s.store.snapshot().Messages {
		if m.ID == current.ID && (m.Rich == nil || !m.Rich.QuoteUnavailable || m.Rich.Quote != "") {
			t.Fatal("撤回通知必须先清除内嵌引用", m)
		}
	}
	unblock()
	task := waitModelTask(t, s, id)
	if task.Status != "failed" {
		t.Fatal("推理期间未验证引用被撤回后仍接受结果", task)
	}
}

func TestReplyRegressionCompactBudgetUsesWireText(t *testing.T) {
	s, m := replyRegressionFixture(t)
	st := s.store.snapshot()
	st.Messages = nil
	for i := 0; i < 25; i++ {
		old := m
		old.ID = fmt.Sprintf("%s%d", strings.Repeat("fixture-long-archive-id-", 8), i)
		old.Content = fmt.Sprintf("short-body-%d", i)
		old.Sender = "成员"
		st.Messages = append(st.Messages, old)
	}
	st.Messages = append(st.Messages, m)
	conv, h := buildReplyContext(st, m, time.Now())
	q := ai.Request{Prompt: m.Content, Target: m.Target, Conversation: &conv, Context: h}
	out, _, err := materializeSafety(st, q, Model{Context: 25, ContextTokens: 2048}, 256)
	if err != nil || len(out.Context) != 25 {
		t.Fatalf("仍按JSON元数据长度提前裁剪短文本：%d %v", len(out.Context), err)
	}
}
