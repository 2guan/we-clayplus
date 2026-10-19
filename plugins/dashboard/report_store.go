// 文件名：report_store.go
// 主要功能：加密保存洞察资料缓存与不可变快照，独立于全局页面状态。
// 作者：2guan
package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/plugins/contextcleaner"
)

type ReportRange struct {
	Mode  string `json:"mode"`
	Count int    `json:"count"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}
type reportSnapshot struct {
	ID        string                   `json:"id"`
	Account   string                   `json:"account"`
	Target    string                   `json:"target"`
	Type      string                   `json:"type"`
	Captured  string                   `json:"captured"`
	Range     ReportRange              `json:"range"`
	Documents []contextcleaner.Message `json:"documents"`
	Images    []ai.InputImage          `json:"images,omitempty"`
	Prompt    string                   `json:"prompt"`
	Model     string                   `json:"model,omitempty"`
}
type momentAnalysisCache struct {
	Account  string   `json:"account"`
	Target   string   `json:"target"`
	Captured string   `json:"captured"`
	Items    []Moment `json:"items"`
	Cursor   string   `json:"cursor"`
	Complete bool     `json:"complete"`
}

func reportKey(prefix, account, id string) string {
	sum := sha256.Sum256([]byte(account + "\x00" + id))
	return prefix + ":" + hex.EncodeToString(sum[:])
}
func (s *store) saveReportValue(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.putSecret(key, string(raw))
}
func (s *store) loadReportValue(key string, value any) error {
	raw, err := s.getSecret(key)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), value)
}
func (s *Server) loadReportSnapshot(account, id string) (reportSnapshot, error) {
	var snap reportSnapshot
	err := s.store.loadReportValue(reportKey("report-source", account, id), &snap)
	if err != nil || snap.ID != id || snap.Account != account {
		return snap, fmt.Errorf("原始资料快照不可用，请补选范围后重新分析")
	}
	return snap, nil
}
func validateReportRange(kind string, r ReportRange) error {
	if r.Mode == "count" && r.Count > 0 && r.Count <= 9007199254740991 {
		return nil
	}
	if kind != "moments" && r.Mode == "time" {
		start, e1 := time.Parse(time.RFC3339, r.Start)
		end, e2 := time.Parse(time.RFC3339, r.End)
		if e1 == nil && e2 == nil && start.Before(end) {
			return nil
		}
	}
	return fmt.Errorf("请选择有效的分析条数或起止时间")
}
