// 文件名：revoke_persistence_test.go
// 主要功能：验证发送回执落盘后仍完整，并从归档向撤回入口传递大整数原值。
// 作者：2guan
package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/we-clay/we-clay/sdk/control"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

type persistedReceiptHost struct {
	account, created string
	original         string
	got              control.Command
}

func (h *persistedReceiptHost) Execute(_ context.Context, c control.Command) (any, error) {
	if c.Operation == "send" {
		result := map[string]string{"id": "9223372036854770001", "client_id": "9007199254740993", "create_time": h.created, "account": h.account, "original_client_id": h.original}
		if h.original != "" {
			delete(result, "client_id")
		}
		return result, nil
	}
	h.got = c
	return nil, nil
}
func TestRevokeUsesPersistedReceipt(t *testing.T) {
	for _, original := range []string{"", "synthetic-peer_1234", "1234"} {
		t.Run("original="+original, func(t *testing.T) {
			s := newServer(t, true)
			st := s.store.snapshot()
			target := st.Contacts[0].ID
			h := &persistedReceiptHost{account: st.Account.ID, original: original, created: strconv.FormatInt(time.Now().Unix(), 10)}
			s.host = h
			s.cfg.Mock = false
			if err := s.send(context.Background(), actionRequest{Target: target, Content: "测试"}, "manual"); err != nil {
				t.Fatal(err)
			}
			var raw []byte
			if err := s.store.db.QueryRow("SELECT body FROM documents WHERE id='state'").Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var loaded State
			if err := json.Unmarshal(raw, &loaded); err != nil {
				t.Fatal(err)
			}
			if err := s.store.loadCollections(s.store.db, &loaded); err != nil {
				t.Fatal(err)
			}
			// 丢弃内存状态，模拟后台重新读取持久化归档。
			s.store.mu.Lock()
			s.store.state = loaded
			s.store.mu.Unlock()
			body, _ := json.Marshal(actionRequest{Kind: "revoke", Target: target, ID: "9223372036854770001"})
			w := httptest.NewRecorder()
			r := httptest.NewRequest("POST", "/api/action", bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			s.action(w, r, User{Role: "admin"})
			var got control.RevokeReceipt
			_ = json.Unmarshal(h.got.Params, &got)
			if w.Code != 200 || ((original == "" && got.ClientID != "9007199254740993") || (original != "" && (got.ClientID != "" || got.OriginalClientID != original))) || got.Created != h.created || got.Account != h.account {
				t.Fatalf("原始回执未完整传递：status=%d", w.Code)
			}
		})
	}
}
