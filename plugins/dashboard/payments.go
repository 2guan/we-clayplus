// 文件名：payments.go
// 主要功能：从账号归档解析收款票据，提供人工确认和持久化去重，不向 AI 开放资金操作。
// 作者：2guan
package dashboard

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/we-clay/we-clay/sdk/control"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type paymentTicket struct {
	Account       string `json:"account"`
	Kind          string `json:"kind"`
	NativeURL     string `json:"native_url,omitempty"`
	InvalidTime   string `json:"invalid_time,omitempty"`
	TransferID    string `json:"transfer_id,omitempty"`
	TransactionID string `json:"transaction_id,omitempty"`
	InWay         int    `json:"in_way"`
}

func (s *Server) initPayments() error {
	_, e := s.store.db.Exec(`CREATE TABLE IF NOT EXISTS payment_receipts(account TEXT NOT NULL,id TEXT NOT NULL,status TEXT NOT NULL,updated TEXT NOT NULL,PRIMARY KEY(account,id));UPDATE payment_receipts SET status='unknown' WHERE status='processing'`)
	return e
}

// 浏览器只能选消息，所有可花费/领取的票据来自 SDK 已归档的原始协议。
func (s *Server) paymentTicket(target, id string) (Message, paymentTicket, string, error) {
	st := s.store.snapshot()
	var m Message
	found := false
	for _, item := range st.Messages {
		if item.ID == id && item.Target == target {
			m = item
			found = true
			break
		}
	}
	q := paymentTicket{Kind: m.Kind, Account: st.Account.ID}
	fail := func(reason string) (Message, paymentTicket, string, error) { return m, q, "", fmt.Errorf("%s", reason) }
	if !found || m.Self || strings.HasPrefix(m.Status, "revoked") || (m.Kind != "redpacket" && m.Kind != "transfer") {
		return fail("请选择收到的红包或转账消息")
	}
	if m.Kind == "transfer" && strings.HasSuffix(target, "@chatroom") {
		return fail("当前不支持群内转账收款")
	}
	if st.Mock {
		return m, q, "mock:" + target + ":" + id, nil
	}
	raw, e := s.store.getSecret("quote:" + s.messageMediaKey(m))
	if e != nil || raw == "" {
		return fail("该历史消息缺少原始支付票据，请在微信处理")
	}
	if strings.HasPrefix(strings.TrimSpace(raw), "<appmsg") {
		raw = "<msg>" + raw + "</msg>"
	}
	var v struct {
		App struct {
			Type int `xml:"type"`
			Pay  struct {
				NativeURL     string `xml:"nativeurl"`
				TransferID    string `xml:"transferid"`
				TransactionID string `xml:"transcationid"`
				InvalidTime   string `xml:"invalidtime"`
				Subtype       int    `xml:"paysubtype"`
			} `xml:"wcpayinfo"`
		} `xml:"appmsg"`
	}
	if xml.Unmarshal([]byte(raw), &v) != nil {
		return fail("支付消息格式无法识别")
	}
	identity := ""
	if m.Kind == "redpacket" {
		if v.App.Type != 2001 {
			return fail("红包协议类型不符")
		}
		u, e := url.Parse(v.App.Pay.NativeURL)
		if e != nil || u.Scheme != "wxpay" || u.Host != "c2cbizmessagehandler" {
			return fail("红包票据无效")
		}
		values, e := url.ParseQuery(u.RawQuery)
		if e != nil || values.Get("sendid") == "" || values.Get("channelid") == "" || values.Get("msgtype") == "" {
			return fail("红包票据缺失")
		}
		q.NativeURL = v.App.Pay.NativeURL
		q.InWay = 1
		identity = "redpacket:" + values.Get("sendid")
	} else {
		if v.App.Type != 2000 || v.App.Pay.Subtype != 1 {
			return fail("该消息不是待收款的转账")
		}
		q.TransferID = v.App.Pay.TransferID
		q.TransactionID = v.App.Pay.TransactionID
		q.InvalidTime = v.App.Pay.InvalidTime
		if q.TransferID == "" || q.TransactionID == "" || q.InvalidTime == "" {
			return fail("转账票据不完整，请在微信处理")
		}
		identity = "transfer:" + q.TransferID + ":" + q.TransactionID
	}
	return m, q, fmt.Sprintf("%x", sha256.Sum256([]byte(identity))), nil
}
func (s *Server) paymentAPI(w http.ResponseWriter, r *http.Request, u User) {
	if e := requireAdmin(u); e != nil {
		writeError(w, 403, e.Error())
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeError(w, 405, "请求方法不支持")
		return
	}
	var q struct {
		Target    string `json:"target"`
		ID        string `json:"id"`
		Confirmed bool   `json:"confirmed"`
	}
	if r.Method == http.MethodGet {
		q.Target = r.URL.Query().Get("target")
		q.ID = r.URL.Query().Get("id")
	} else if !decode(w, r, &q) {
		return
	}
	m, ticket, key, e := s.paymentTicket(q.Target, q.ID)
	if e != nil {
		writeError(w, 400, e.Error())
		return
	}
	st := s.store.snapshot()
	if st.Account.ID != ticket.Account {
		writeError(w, 409, "微信账号已变化，请重新打开消息")
		return
	}
	status := "ready"
	e = s.store.db.QueryRow("SELECT status FROM payment_receipts WHERE account=? AND id=?", st.Account.ID, key).Scan(&status)
	if e != nil && e != sql.ErrNoRows {
		writeError(w, 500, "读取支付记录失败")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, 200, map[string]any{"status": status, "kind": m.Kind, "sender": m.Sender, "amount": func() string {
			if m.Rich != nil {
				return m.Rich.Amount
			}
			return ""
		}(), "mock": st.Mock})
		return
	}
	if !q.Confirmed {
		writeError(w, 400, "请确认领取对象")
		return
	}
	if st.Account.Status != "online" || (!st.Mock && s.host == nil) {
		writeError(w, 400, "微信尚未就绪")
		return
	}
	// 按交易标识去重，不因换消息 ID、重复点击或服务重启再次执行资金请求。
	res, e := s.store.db.Exec("INSERT OR IGNORE INTO payment_receipts VALUES(?,?,?,?)", st.Account.ID, key, "processing", time.Now().Format(time.RFC3339))
	if e != nil {
		writeError(w, 500, "无法记录支付操作，未提交")
		return
	}
	count, e := res.RowsAffected()
	if e != nil || count != 1 {
		writeError(w, 409, "此交易已提交，请在微信核对，不重复操作")
		return
	}
	status = "confirmed"
	if !st.Mock {
		params, _ := json.Marshal(ticket)
		result, err := s.host.Execute(r.Context(), control.Command{Operation: "payment-claim", Target: q.Target, ID: q.ID, Params: params})
		receipt, ok := result.(map[string]bool)
		if err != nil || !ok || !receipt["confirmed"] {
			status = "unknown"
		}
	}
	_, e = s.store.db.Exec("UPDATE payment_receipts SET status=?,updated=? WHERE account=? AND id=?", status, time.Now().Format(time.RFC3339), st.Account.ID, key)
	if e != nil {
		writeError(w, 500, "支付请求已提交，记录更新失败，请在微信核对")
		return
	}
	if status == "unknown" {
		writeError(w, 502, "结果未确认，请在微信核对，系统不会自动重试")
		return
	}
	// 保留原始通知，领取回执单独显示，不伪造微信发来的消息或到账金额。
	writeJSON(w, 200, map[string]string{"status": status})
}
