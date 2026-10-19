// 文件名：target_settings_test.go
// 主要功能：验证中文长人设的会话保存、继承默认以及类型和容量校验。
// 作者：2guan
package dashboard

import (
	"strings"
	"testing"
)

func TestTargetLongChinesePrompt(t *testing.T) {
	s := newServer(t, true)
	target := s.store.snapshot().Contacts[0].ID
	// 复现从人设库载入约六千中文字符的提示词，不能在保存时截断。
	prompt := strings.Repeat("中文人设", 1600)
	save := func(value any) int {
		return call(t, s, "/api/action", actionRequest{Kind: "target", Target: target, Values: map[string]any{"prompt": value, "model": nil}}, nil, "").Code
	}
	if code := save(prompt); code != 200 {
		t.Fatalf("中文长人设保存失败：%d", code)
	}
	if got := s.store.snapshot().Overrides[target]["prompt"]; got != prompt {
		t.Fatal("提示词被截断或未保存")
	}
	// 无效输入必须拒绝且保留上一次保存的提示词。
	for _, invalid := range []any{true, []string{"提示词"}, strings.Repeat("a", (64<<10)+1)} {
		if save(invalid) != 400 {
			t.Fatal("未拒绝无效提示词")
		}
		if s.store.snapshot().Overrides[target]["prompt"] != prompt {
			t.Fatal("校验失败仍修改了设置")
		}
	}
	if save(strings.Repeat("a", 64<<10)) != 200 {
		t.Fatal("边界容量应可保存")
	}
	if save(nil) != 200 || s.store.snapshot().Overrides[target]["prompt"] != nil {
		t.Fatal("无法恢复全局人设")
	}
}

// 群概率需要验证请求边界、零值覆写及恢复全局，不能只测试表单保存。
func TestTargetGroupProbability(t *testing.T) {
	s := newServer(t, true)
	target := ""
	friend := ""
	for _, c := range s.store.snapshot().Contacts {
		if c.Kind == "group" {
			target = c.ID
		}
		if c.Kind == "friend" {
			friend = c.ID
		}
	}
	save := func(id string, value any) int {
		return call(t, s, "/api/action", actionRequest{Kind: "target", Target: id, Values: map[string]any{"probability": value}}, nil, "").Code
	}
	if err := s.store.change(func(v *State) error { v.Settings["reply.probability"] = float64(42); return nil }); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []any{-1, 101, 2.5, "50"} {
		if save(target, bad) == 200 {
			t.Fatalf("接受无效值 %v", bad)
		}
	}
	if save(friend, 50) == 200 {
		t.Fatal("私聊接受了群概率")
	}
	for _, n := range []float64{0, 100, 25} {
		if save(target, n) != 200 || groupReplyProbability(s.store.snapshot(), target) != n {
			t.Fatalf("未应用覆写 %v", n)
		}
	}
	if save(target, nil) != 200 || groupReplyProbability(s.store.snapshot(), target) != 42 {
		t.Fatal("未恢复全局概率")
	}
}
