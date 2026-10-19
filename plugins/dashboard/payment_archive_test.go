// 文件名：payment_archive_test.go
// 主要功能：可选只读检查本机支付归档是否有完整票据，只输出统计，不执行领取。
// 作者：2guan
package dashboard

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPaymentArchiveReadOnly(t *testing.T) {
	dir := os.Getenv("WE_CLAY_AUDIT_PAYMENT_DIR")
	if dir == "" {
		t.Skip("仅显式指定目录时进行只读归档检查")
	}
	db, e := sql.Open("sqlite", "file:"+filepath.Join(dir, "console.db")+"?mode=ro")
	if e != nil {
		t.Fatal("无法只读打开归档")
	}
	defer db.Close()
	key, e := os.ReadFile(filepath.Join(dir, "credential.key"))
	if e != nil || len(key) != 32 {
		t.Fatal("无法读取归档密钥")
	}
	st := &store{db: db, key: key}
	var raw []byte
	if e = db.QueryRow("SELECT body FROM documents WHERE id='state'").Scan(&raw); e != nil {
		t.Fatal("无法读取状态")
	}
	if json.Unmarshal(raw, &st.state) != nil {
		t.Fatal("状态格式错误")
	}
	if storageVersion(raw) == stateStorageVersion {
		if e = st.loadCollections(db, &st.state); e != nil {
			t.Fatal("无法读取分表归档")
		}
	}
	s := &Server{store: st}
	total, complete := 0, 0
	for _, m := range st.state.Messages {
		if !m.Self && (m.Kind == "transfer" || m.Kind == "redpacket") {
			total++
			if _, _, _, e = s.paymentTicket(m.Target, m.ID); e == nil {
				complete++
			}
		}
	}
	t.Logf("收到的支付通知=%d，具有可解析领取票据=%d；未发送任何请求", total, complete)
}
