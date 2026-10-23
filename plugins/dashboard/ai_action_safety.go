package dashboard

import (
	"encoding/json"
	"fmt"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/plugins/promptguard"
)

// 工具返回的昵称、消息、动态等仍是不可信资料。检查解码后的字符串，不让JSON转义绕过防护。
func (s *Server) checkActionData(task string, q ai.Request, raw []byte) error {
	guard, _, e := safetyConfig(s.store.snapshot(), q.Source, q.Target)
	if e != nil {
		return e
	}
	var data any
	if e = json.Unmarshal(raw, &data); e != nil {
		return e
	}
	report := safetyReport{}
	var visit func(any) error
	visit = func(value any) error {
		switch v := value.(type) {
		case string:
			hits, e := promptguard.Check(guard, v, "document", false)
			if e != nil {
				return e
			}
			report.Hits = append(report.Hits, hits...)
		case []any:
			for _, x := range v {
				if e := visit(x); e != nil {
					return e
				}
			}
		case map[string]any:
			for _, x := range v {
				if e := visit(x); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if e = visit(data); e != nil {
		return e
	}
	report.Blocked = promptguard.Blocked(report.Hits)
	if len(report.Hits) > 0 {
		if e = s.recordSafety(task, q.Source, "tool-data", report); e != nil {
			return e
		}
	}
	if report.Blocked {
		return fmt.Errorf("工具返回的参考资料触发提示词防护，后续操作已停止；已完成步骤保留")
	}
	return nil
}
