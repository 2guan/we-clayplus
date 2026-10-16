package dashboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

func TestStateStorageMigrationIncrementalAndRollback(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	legacy := initialState(false)
	legacy.Account.ID = "storage-account"
	legacy.Messages = []Message{{ID: "original", Target: "peer", Content: "fixture-original", Mentions: []string{"member"}, RevokeReceipt: &control.RevokeReceipt{ClientID: "9007199254740993"}}}
	legacy.Logs = []Record{{ID: "large-log", Time: time.Now().Format(time.RFC3339), Level: "DEBUG", Component: "debug", Message: strings.Repeat("fixture-large-log ", 2000)}}
	raw, _ := json.Marshal(legacy)
	if _, err = s.db.Exec("UPDATE documents SET body=? WHERE id='state'", raw); err != nil {
		t.Fatal(err)
	}
	s.db.Close()
	s, err = openStore(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	var doc []byte
	if err = s.db.QueryRow("SELECT body FROM documents WHERE id='state'").Scan(&doc); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(doc, []byte("fixture-original")) || bytes.Contains(doc, []byte("fixture-large-log")) || storageVersion(doc) != stateStorageVersion {
		t.Fatal("归档原文仍在全量配置文档")
	}
	// 追加消息不能重写已有记录；SQL 触发器使任何无关 UPDATE 直接失败。
	if _, err = s.db.Exec("CREATE TRIGGER unchanged_message BEFORE UPDATE ON state_rows WHEN OLD.kind='messages' AND OLD.position=0 BEGIN SELECT RAISE(ABORT,'unchanged row rewritten'); END"); err != nil {
		t.Fatal(err)
	}
	if err = s.change(func(v *State) error {
		v.Messages = append(v.Messages, Message{ID: "appended", Content: "fixture-new"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	copy := s.snapshot()
	copy.Messages[0].Mentions[0] = "mutated"
	copy.Messages[0].RevokeReceipt.ClientID = "mutated"
	if s.snapshot().Messages[0].Mentions[0] != "member" || s.snapshot().Messages[0].RevokeReceipt.ClientID != "9007199254740993" {
		t.Fatal("快照共享可变引用")
	}
	if err = s.change(func(v *State) error { v.Messages[0].Content = "forbidden"; return nil }); err == nil {
		t.Fatal("失败事务未回滚")
	}
	if s.snapshot().Messages[0].Content != "fixture-original" {
		t.Fatal("事务失败后内存被污染")
	}
	if _, err = s.db.Exec("DROP TRIGGER unchanged_message"); err != nil {
		t.Fatal(err)
	}
	// 多次重开不能把数据库中的日志原文覆盖成内存摘要。
	for i := 0; i < 2; i++ {
		next, e := openStore(dir, false)
		if e != nil {
			t.Fatal(e)
		}
		if len(next.snapshot().Messages) != 2 {
			t.Fatal("消息迁移或追加丢失")
		}
		var body string
		if e = next.db.QueryRow("SELECT message FROM state_logs WHERE account=? AND id='large-log'", legacy.Account.ID).Scan(&body); e != nil || body != legacy.Logs[0].Message {
			t.Fatal("原始日志重启后丢失", e)
		}
		if len(next.snapshot().Logs[0].Message) > 2048 {
			t.Fatal("完整日志再次进入内存热路径")
		}
		next.db.Close()
	}
}

func TestStateStorageAccountRoundTrip(t *testing.T) {
	s, err := openStore(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	if err = s.switchAccount(Account{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err = s.change(func(v *State) error {
		v.Messages = []Message{{ID: "a-msg", Content: "fixture-a"}}
		addLog(v, "INFO", "test", "fixture-a-log")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.switchAccount(Account{ID: "b"}); err != nil {
		t.Fatal(err)
	}
	if len(s.snapshot().Messages) != 0 || len(s.snapshot().Logs) != 0 {
		t.Fatal("跨账号泄露归档")
	}
	if err = s.change(func(v *State) error { v.Messages = []Message{{ID: "b-msg", Content: "fixture-b"}}; return nil }); err != nil {
		t.Fatal(err)
	}
	if err = s.switchAccount(Account{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if s.snapshot().Messages[0].Content != "fixture-a" || s.snapshot().Logs[0].Message != "fixture-a-log" {
		t.Fatal("账号归档未恢复")
	}
}

func storageBenchmarkState() State {
	v := initialState(false)
	for i := 0; i < 8000; i++ {
		v.Messages = append(v.Messages, Message{ID: fmt.Sprint(i), Target: "fixture-peer", Content: strings.Repeat("fixture message ", 30)})
	}
	for i := 0; i < 1000; i++ {
		v.Logs = append(v.Logs, Record{ID: fmt.Sprint(i), Message: strings.Repeat("fixture log ", 1600)})
	}
	return v
}

func BenchmarkStateCopy(b *testing.B) {
	v := storageBenchmarkState()
	b.Run("legacy-json", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			raw, _ := json.Marshal(v)
			var out State
			_ = json.Unmarshal(raw, &out)
		}
	})
	compactLogs(&v)
	b.Run("containers-and-summaries", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			copy := cloneState(v)
			if !reflect.DeepEqual(copy.Account, v.Account) {
				b.Fatal("copy")
			}
		}
	})
}
