package dashboard

import (
	"encoding/json"
	"testing"
	"time"
)

func TestReportMetadata(t *testing.T) {
	s := newServer(t, true)
	target := s.store.snapshot().Contacts[0]
	if err := s.store.change(func(v *State) error {
		v.Settings["routing.concurrency"] = float64(10)
		v.Contacts[0].Remark = "生成时的备注"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, kind := range []string{"conversation", "group", "moments"} {
		prompt := "完整的原始提示词\n保留第二行"
		q := map[string]any{"kind": "insight", "prompt": prompt + "\n分析对象：补充信息", "target": target.ID, "insight": InsightInfo{Type: kind, Prompt: prompt, TargetName: "不可信的客户端名称"}}
		w := call(t, s, "/api/tasks", q, nil, "")
		if w.Code != 202 {
			t.Fatal(w.Body.String())
		}
		var result map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, result["id"])
		task, err := s.Task(result["id"])
		if err != nil || task.Insight == nil || task.Insight.Type != kind || task.Insight.Prompt != prompt || task.Insight.TargetName != "生成时的备注" {
			t.Fatalf("报告信息错误: %+v, %v", task.Insight, err)
		}
	}
	if w := call(t, s, "/api/tasks", map[string]any{"kind": "insight", "prompt": "示例", "insight": InsightInfo{Type: "invalid", Prompt: "示例"}}, nil, ""); w.Code != 400 {
		t.Fatal("接受了未知报告类型")
	}
	if err := s.store.change(func(v *State) error { v.Contacts[0].Remark = "修改后的备注"; return nil }); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for _, id := range ids {
		for {
			task, _ := s.Task(id)
			if task.Status == "completed" {
				if task.Model == "" {
					t.Fatal("未记录实际使用模型")
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("报告未完成")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	reopened, err := openStore(s.cfg.DataDir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.db.Close()
	found := 0
	for _, task := range reopened.snapshot().Tasks {
		if task.Insight == nil {
			continue
		}
		found++
		if task.Insight.TargetName != "生成时的备注" || task.Insight.Prompt != "完整的原始提示词\n保留第二行" {
			t.Fatal("报告元信息未持久化")
		}
	}
	if found != 3 {
		t.Fatalf("报告数量错误: %d", found)
	}
}
