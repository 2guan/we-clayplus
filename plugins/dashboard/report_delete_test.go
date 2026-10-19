package dashboard

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
)

func TestDeleteReport(t *testing.T) {
	s := newServer(t, true)
	today := time.Now().Format(time.RFC3339)
	if err := s.store.change(func(v *State) error {
		v.Tasks = []Task{}
		for _, status := range []string{"completed", "failed", "cancelled", "interrupted", "queued", "running"} {
			v.Tasks = append(v.Tasks, Task{Insight: &InsightInfo{Type: "conversation", Prompt: "待删除提示词", TargetName: "待删除对象"}, ID: status, Kind: "insight", Status: status, Created: today, Input: "待删除输入", Result: "待删除正文", Target: "fixture-contact"})
		}
		v.Tasks = append(v.Tasks, Task{ID: "chat", Kind: "chat", Status: "completed", Created: today})
		v.Settings["routing.daily_calls"] = float64(len(v.Tasks))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"queued", "running", "chat", "missing"} {
		if w := call(t, s, "/api/action", actionRequest{Kind: "delete-report", ID: id}, nil, ""); w.Code != 400 {
			t.Fatalf("不应删除 %s: %d", id, w.Code)
		}
	}
	s.startTaskStream("completed")
	s.appendTaskStream("completed", "待删除流式正文")
	s.finishTaskStream("completed")
	for _, id := range []string{"completed", "failed", "cancelled", "interrupted"} {
		if w := call(t, s, "/api/action", actionRequest{Kind: "delete-report", ID: id}, nil, ""); w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		s.updateTask(id, func(task *Task) { task.Result = "迟到的结果"; task.Status = "completed" })
		if _, err := s.Task(id); err == nil {
			t.Fatal("删除的报告仍可读取")
		}
	}
	if w := call(t, s, "/api/task-stream?id=completed", nil, nil, ""); w.Code != 404 {
		t.Fatal("已删除报告仍有流式正文")
	}
	var visible State
	if err := json.Unmarshal(call(t, s, "/api/state", nil, nil, "").Body.Bytes(), &visible); err != nil {
		t.Fatal(err)
	}
	if len(visible.Tasks) != 3 {
		t.Fatalf("删除报告未从列表隐藏: %d", len(visible.Tasks))
	}
	if _, err := s.Submit(ai.Request{Kind: "chat", Prompt: "测试每日限额"}); err == nil {
		t.Fatal("删除报告绕过每日限额")
	}
	reopened, err := openStore(s.cfg.DataDir, true)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.db.Close()
	for _, task := range reopened.snapshot().Tasks {
		if task.Status == "deleted" && (task.Insight != nil || task.Input != "" || task.Result != "" || task.Target != "" || task.Created != today) {
			t.Fatal("删除内容未持久化或调用日期丢失")
		}
	}
}
