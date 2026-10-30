package dashboard

import (
	"bytes"
	"strings"
	"testing"
)

func TestAccountMomentCoverReconnectAndIsolation(t *testing.T) {
	for _, entry := range []string{"store", "server"} {
		t.Run(entry, func(t *testing.T) {
			var setAccount func(Account) error
			var setCover func(string) error
			var snapshot func() State
			if entry == "server" {
				s := newServer(t, false)
				setAccount, setCover, snapshot = s.SetAccount, s.SetMomentCover, s.store.snapshot
			} else {
				s, err := openStore(t.TempDir(), false)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = s.db.Close() })
				setAccount, snapshot = s.switchAccount, s.snapshot
				setCover = func(cover string) error {
					return s.change(func(v *State) error { v.Account.MomentCover = cover; return nil })
				}
			}
			if err := setAccount(Account{ID: "fixture-cover-a", Name: "首次连接"}); err != nil {
				t.Fatal(err)
			}
			// 模拟资料同步后单独取得封面；随后重连的账号资料不包含此字段。
			const firstCover = "https://example.invalid/cover-a.jpg"
			if err := setCover(firstCover); err != nil {
				t.Fatal(err)
			}
			steps := []struct {
				name string
				in   Account
				want Account
			}{
				{"同账号重连", Account{ID: "fixture-cover-a", Name: "重连资料", Status: "online"}, Account{ID: "fixture-cover-a", Name: "重连资料", Status: "online", MomentCover: firstCover}},
				{"显式新封面", Account{ID: "fixture-cover-a", MomentCover: "https://example.invalid/cover-a-new.jpg"}, Account{ID: "fixture-cover-a", MomentCover: "https://example.invalid/cover-a-new.jpg"}},
				{"切到另一账号", Account{ID: "fixture-cover-b", Status: "online"}, Account{ID: "fixture-cover-b", Status: "online"}},
				{"另一账号新封面", Account{ID: "fixture-cover-b", MomentCover: "https://example.invalid/cover-b.jpg"}, Account{ID: "fixture-cover-b", MomentCover: "https://example.invalid/cover-b.jpg"}},
				{"切回不继承前一账号封面", Account{ID: "fixture-cover-a"}, Account{ID: "fixture-cover-a"}},
				{"已知账号再次取得封面", Account{ID: "fixture-cover-a", MomentCover: firstCover}, Account{ID: "fixture-cover-a", MomentCover: firstCover}},
				{"空ID不能继承已知账号封面", Account{Status: "offline"}, Account{Status: "offline"}},
				{"空ID显式输入", Account{MomentCover: "https://example.invalid/unidentified.jpg"}, Account{MomentCover: "https://example.invalid/unidentified.jpg"}},
				{"已知账号不能继承空ID封面", Account{ID: "fixture-cover-b"}, Account{ID: "fixture-cover-b"}},
				{"再次空ID显式输入", Account{MomentCover: "https://example.invalid/unidentified.jpg"}, Account{MomentCover: "https://example.invalid/unidentified.jpg"}},
				{"相同空ID也不继承封面", Account{}, Account{}},
			}
			for _, step := range steps {
				if err := setAccount(step.in); err != nil {
					t.Fatalf("%s: %v", step.name, err)
				}
				if got := snapshot().Account; got != step.want {
					t.Fatalf("%s: got %#v, want %#v", step.name, got, step.want)
				}
			}
		})
	}
}

func TestStoreAccountMomentCoverReconnectAfterReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	const cover = "https://example.invalid/persisted-cover.jpg"
	if err = s.switchAccount(Account{ID: "fixture-persisted-cover", MomentCover: cover}); err != nil {
		_ = s.db.Close()
		t.Fatal(err)
	}
	if err = s.db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openStore(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	if err = s.switchAccount(Account{ID: "fixture-persisted-cover", Status: "online"}); err != nil {
		t.Fatal(err)
	}
	if got := s.snapshot().Account.MomentCover; got != cover {
		t.Fatalf("重启后同账号重连丢失已持久化封面: %q", got)
	}
}

func TestMomentCoverForAccountRejectsStaleResponse(t *testing.T) {
	s := newServer(t, false)
	if err := s.SetAccount(Account{ID: "fixture-guard-a"}); err != nil {
		t.Fatal(err)
	}
	accountA, epochA := s.AccountIdentity()
	if accountA != "fixture-guard-a" || epochA != s.store.snapshot().AccountEpoch {
		t.Fatal("账号身份快照不匹配")
	}
	wake := make(chan struct{}, 1)
	s.eventMu.Lock()
	s.subscribers[wake] = struct{}{}
	s.eventMu.Unlock()
	const cover = "https://example.invalid/guard-cover-a.jpg"
	if err := s.SetMomentCoverForAccount(accountA, epochA, cover); err != nil {
		t.Fatal(err)
	}
	if s.store.snapshot().Account.MomentCover != cover {
		t.Fatal("当前账号的封面未保存")
	}
	select {
	case <-wake:
	default:
		t.Fatal("封面更新未通知页面")
	}
	if err := s.SetMomentCoverForAccount(accountA, epochA, ""); err != nil || s.store.snapshot().Account.MomentCover != cover {
		t.Fatal("正常缺省清除了已知封面", err)
	}
	select {
	case <-wake:
		t.Fatal("空封面不应触发更新通知")
	default:
	}
	readDocument := func() []byte {
		t.Helper()
		var body []byte
		if err := s.store.db.QueryRow("SELECT body FROM documents WHERE id='state'").Scan(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	reject := func(name, account string, epoch uint64) {
		t.Helper()
		before, raw := s.store.snapshot(), readDocument()
		err := s.SetMomentCoverForAccount(account, epoch, "https://example.invalid/stale-cover.jpg")
		if err == nil || !strings.Contains(err.Error(), "已过期") {
			t.Fatalf("%s: 迟到响应未被拒绝: %v", name, err)
		}
		after := s.store.snapshot()
		if before.Account != after.Account || before.AccountEpoch != after.AccountEpoch || before.Revision != after.Revision || !bytes.Equal(raw, readDocument()) {
			t.Fatalf("%s: 拒绝响应后仍修改了内存或数据库", name)
		}
	}
	if err := s.SetAccount(Account{ID: "fixture-guard-b", MomentCover: "https://example.invalid/guard-cover-b.jpg"}); err != nil {
		t.Fatal(err)
	}
	reject("A到B", accountA, epochA)
	if err := s.SetAccount(Account{ID: accountA, MomentCover: cover}); err != nil {
		t.Fatal(err)
	}
	_, currentEpoch := s.AccountIdentity()
	if currentEpoch == epochA {
		t.Fatal("切回同账号未更新代次")
	}
	reject("A到B再到A", accountA, epochA)
	reject("输入空账号ID", "", currentEpoch)
	if err := s.SetAccount(Account{}); err != nil {
		t.Fatal(err)
	}
	_, emptyEpoch := s.AccountIdentity()
	reject("当前和输入账号ID均为空", "", emptyEpoch)
}
