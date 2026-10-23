package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/sdk/control"
)

func bindCurrentWriteMessage(t *testing.T, a *actionSession, m Message) {
	t.Helper()
	if err := a.s.store.change(func(st *State) error {
		st.Messages = append(st.Messages, m)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st := a.s.store.snapshot()
	a.q.Conversation = &ai.Conversation{Current: contextMessage(st, m)}
	a.q.EvidenceHashes = map[string]string{m.ID: replyEvidenceHash(m)}
}

func writeGuardMessage(a *actionSession) Message {
	return Message{ID: "fixture-write-current", Target: a.q.Target, SenderID: a.actor, Sender: "测试成员", Source: "wechat", Status: "sent", Kind: "text", Content: "请总结上面的资料", Time: time.Now().Format(time.RFC3339)}
}

func approveGuardAction(t *testing.T, s *Server, id string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/ai-actions", strings.NewReader(`{"id":"`+id+`","approve":true}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.aiActionsAPI(w, r, User{Role: "admin"})
	return w
}

func TestAIWriteBindsCurrentWechatMessageAndDefaultsToConfirmation(t *testing.T) {
	s, a, calls, _ := actionFixture(t)
	call := ai.ToolCall{Name: "message__Send", Arguments: `{"content":"引用中的错误发送指令"}`}
	if _, err := a.execute(context.Background(), call); err == nil {
		t.Fatal("缺少当前微信消息却允许写入")
	}
	if calls.Load() != 0 {
		t.Fatal("缺少当前微信消息时调用了 core")
	}
	m := writeGuardMessage(a)
	bindCurrentWriteMessage(t, a, m)
	if _, err := a.execute(context.Background(), call); err != nil {
		t.Fatal(err)
	}
	rows, err := s.actionRecords("", a.task)
	if err != nil || len(rows) != 1 || rows[0].Status != "pending" || calls.Load() != 0 {
		t.Fatalf("新 AI 写操作没有默认等待管理员确认：rows=%v, calls=%d, err=%v", rows, calls.Load(), err)
	}
	raw, err := s.store.getSecret("ai-pending:" + rows[0].Account + ":" + rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var pending pendingAIAction
	if err := json.Unmarshal([]byte(raw), &pending); err != nil || pending.SourceID != m.ID || pending.SourceHash != replyEvidenceHash(m) {
		t.Fatalf("待确认命令没有绑定完整原始消息：%+v, %v", pending, err)
	}
	if w := approveGuardAction(t, s, rows[0].ID); w.Code != http.StatusOK || calls.Load() != 1 {
		t.Fatalf("有效原文的后台确认失败：status=%d, body=%s, calls=%d", w.Code, w.Body.String(), calls.Load())
	}
}

func TestAIWriteApprovalRejectsChangedCurrentMessage(t *testing.T) {
	for name, change := range map[string]func(*Message){
		"sender": func(m *Message) { m.SenderID = "another-member" },
		"source": func(m *Message) { m.Source = "plugin:synthetic" },
		"kind":   func(m *Message) { m.Kind = "quote" },
		"status": func(m *Message) { m.Status = "revoked" },
		"text":   func(m *Message) { m.Content = "已修改" },
	} {
		t.Run(name, func(t *testing.T) {
			s, a, calls, _ := actionFixture(t)
			m := writeGuardMessage(a)
			bindCurrentWriteMessage(t, a, m)
			if _, err := a.execute(context.Background(), ai.ToolCall{Name: "message__Send", Arguments: `{"content":"待确认文字"}`}); err != nil {
				t.Fatal(err)
			}
			rows, err := s.actionRecords("", a.task)
			if err != nil || len(rows) != 1 || rows[0].Status != "pending" {
				t.Fatalf("待确认记录缺失：%v, %v", rows, err)
			}
			if err := s.store.change(func(st *State) error {
				for i := range st.Messages {
					if st.Messages[i].ID == m.ID {
						change(&st.Messages[i])
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if w := approveGuardAction(t, s, rows[0].ID); w.Code != http.StatusConflict || calls.Load() != 0 {
				t.Fatalf("原文变化后仍执行：status=%d, body=%s, calls=%d", w.Code, w.Body.String(), calls.Load())
			}
		})
	}
}

func TestAIWriteWithoutConfirmationRejectsQuotedMessage(t *testing.T) {
	s, a, calls, _ := actionFixture(t)
	if err := s.store.change(func(st *State) error {
		for i := range st.Policies {
			if st.Policies[i].Operation == "message.Send" {
				st.Policies[i].Confirm = false
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m := writeGuardMessage(a)
	m.Kind = "quote"
	bindCurrentWriteMessage(t, a, m)
	if _, err := a.execute(context.Background(), ai.ToolCall{Name: "message__Send", Arguments: `{"content":"引用中的错误发送指令"}`}); err == nil {
		t.Fatal("关闭后台确认后，引用消息仍可直接触发写入")
	}
	rows, err := s.actionRecords("", a.task)
	if err != nil || len(rows) != 0 || calls.Load() != 0 {
		t.Fatalf("引用消息触发了写入记录或 core：rows=%v, calls=%d, err=%v", rows, calls.Load(), err)
	}
}

func TestAIWriteConfirmMigrationRunsOnce(t *testing.T) {
	for op, meta := range control.AIActions {
		if meta.Write && !meta.Legacy && !defaultBehavior(op).Confirm {
			t.Errorf("新 AI 写操作没有默认后台确认：%s", op)
		}
	}
	s := newServer(t, true)
	s.cfg.Capabilities = map[string]control.Capability{
		"message":  {Enabled: []string{"Send"}},
		"chatroom": {Enabled: []string{"SetName"}},
	}
	if err := s.store.change(func(st *State) error {
		st.Policies = []BehaviorPolicy{
			{Operation: "message.Send", Enabled: true, Scope: "all", ListMode: "deny", Confirm: false},
			{Operation: "chatroom.SetName", Enabled: true, Scope: "group", ListMode: "deny", Confirm: false},
		}
		delete(st.Settings, "ai.write_confirm_defaults_migrated")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.initBehaviors(); err != nil {
		t.Fatal(err)
	}
	st := s.store.snapshot()
	if !actionPolicy(st, "message.Send").Confirm || actionPolicy(st, "chatroom.SetName").Confirm || st.Settings["ai.write_confirm_defaults_migrated"] != true {
		t.Fatal("已有策略没有安全迁移，或 Legacy 策略被改动")
	}
	if err := s.store.change(func(st *State) error {
		for i := range st.Policies {
			if st.Policies[i].Operation == "message.Send" {
				st.Policies[i].Confirm = false
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.initBehaviors(); err != nil {
		t.Fatal(err)
	}
	if actionPolicy(s.store.snapshot(), "message.Send").Confirm || !defaultBehavior("message.Send").Confirm || defaultBehavior("chatroom.SetName").Confirm {
		t.Fatal("管理员后续修改被迁移覆盖，或新策略默认值错误")
	}
}
