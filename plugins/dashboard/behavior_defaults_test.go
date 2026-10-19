// 文件名：behavior_defaults_test.go
// 主要功能：验证默认授权、提示词覆盖、旧配置保留及工具展示与执行使用相同权限。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/sdk/control"
)

func TestBehaviorCatalogDefaultsAndPreservation(t *testing.T) {
	s := newServer(t, true)
	raw, err := os.ReadFile("../../gateway/capabilities.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &s.cfg.Capabilities); err != nil {
		t.Fatal(err)
	}
	if err = s.initBehaviors(); err != nil {
		t.Fatal(err)
	}
	st := s.store.snapshot()
	enabled, total := 0, 0
	for domain, cap := range s.cfg.Capabilities {
		for _, name := range cap.Enabled {
			id := domain + "." + name
			total++
			if strings.TrimSpace(behaviorGuidance[id]) == "" {
				t.Errorf("缺少专属提示词：%s", id)
			}
			p := defaultBehavior(id)
			if p.Enabled {
				enabled++
			}
			if p.ListMode != "deny" || len(p.Targets) != 0 {
				t.Errorf("默认名单错误：%s", id)
			}
		}
	}
	if enabled <= total/2 || len(st.Policies) < total {
		t.Fatal("常用策略没有默认开启或目录不完整")
	}
	p := BehaviorPolicy{Operation: "contact.SetRemark", Enabled: false, Prompt: "自定义要求", Scope: "private", ListMode: "allow", Targets: []string{"fixture"}}
	if err = s.saveBehavior(&p); err != nil {
		t.Fatal(err)
	}
	if err = s.initBehaviors(); err != nil {
		t.Fatal(err)
	}
	for _, got := range s.store.snapshot().Policies {
		if got.Operation == p.Operation && (got.Enabled || got.Prompt != p.Prompt || got.ListMode != "allow" || !slices.Equal(got.Targets, p.Targets)) {
			t.Fatal("升级覆盖了管理员配置")
		}
	}
}

func TestBehaviorToolsRespectScopeAndLists(t *testing.T) {
	s := newServer(t, true)
	s.cfg.Capabilities = map[string]control.Capability{"contact": {Enabled: []string{"SetRemark"}}, "chatroom": {Enabled: []string{"SetName", "SetAnnouncement"}}, "message": {Enabled: []string{"Send"}}}
	if err := s.initBehaviors(); err != nil {
		t.Fatal(err)
	}
	tools := s.behaviorTools("mock-contact-1")
	if len(tools) != 1 || tools[0].Name != "contact__SetRemark" || !strings.Contains(tools[0].Description, "新的备注全文") {
		t.Fatalf("私聊工具或提示词不正确：%v", tools)
	}
	tools = s.behaviorTools("fixture@chatroom")
	if len(tools) != 2 {
		t.Fatalf("群聊工具应仅有群名和公告：%v", tools)
	}
	p := defaultBehavior("contact.SetRemark")
	p.Targets = []string{"mock-contact-1"}
	if err := s.saveBehavior(&p); err != nil {
		t.Fatal(err)
	}
	if len(s.behaviorTools("mock-contact-1")) != 0 {
		t.Fatal("黑名单工具仍暴露")
	}
	if _, err := s.ExecuteAI(context.Background(), p.Operation, control.Command{Target: "mock-contact-1", Content: "备注"}); err == nil {
		t.Fatal("黑名单仍可执行")
	}
	// 停用自动发送仅验证本地拒绝，不产生微信写操作。
	p = defaultBehavior("message.Send")
	p.Enabled = false
	_ = s.saveBehavior(&p)
	if err := s.deliverReply(context.Background(), "mock-contact-1", "测试"); err == nil || !strings.Contains(err.Error(), "授权") {
		t.Fatalf("发送策略未执行：%v", err)
	}
}

func TestLegacyEnabledEmptyPolicyMigration(t *testing.T) {
	s := newServer(t, true)
	s.cfg.Capabilities = map[string]control.Capability{"message": {Enabled: []string{"Send"}}}
	_ = s.store.change(func(st *State) error {
		st.Policies = []BehaviorPolicy{{Operation: "message.Send", Enabled: true, Scope: "all", ListMode: "allow", Targets: []string{}}}
		return nil
	})
	if err := s.initBehaviors(); err != nil {
		t.Fatal(err)
	}
	if err := behaviorAllowed(s.store.snapshot(), "message.Send", "fixture"); err != nil {
		t.Fatalf("旧空策略阻断自动回复：%v", err)
	}
}

func TestReplyBehaviorToolsRequireCurrentIntent(t *testing.T) {
	s := newServer(t, true)
	s.cfg.Capabilities = map[string]control.Capability{"chatroom": {Enabled: []string{"SetName", "SetAnnouncement"}}, "contact": {Enabled: []string{"SetRemark"}}}
	if err := s.initBehaviors(); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"你好", "你歇会儿", "群名怎么改", "不要把群名改成测试", "他说把群名改为测试", "如果把群名改成测试", "请问群名改成什么好"} {
		if len(s.replyBehaviorTools("fixture@chatroom", text)) > 0 {
			t.Error("非操作意图获得工具", text)
		}
	}
	for _, tc := range []struct{ target, text, name string }{{"fixture@chatroom", "@本人 请把群名改成离线测试群", "chatroom__SetName"}, {"fixture@chatroom", "请发布公告：离线示例", "chatroom__SetAnnouncement"}, {"mock-contact-1", "帮我把备注改成离线测试", "contact__SetRemark"}} {
		tools := s.replyBehaviorTools(tc.target, tc.text)
		if len(tools) != 1 || tools[0].Name != tc.name {
			t.Errorf("明确操作未得到对应工具：%s %+v", tc.text, tools)
		}
	}
	st := s.store.snapshot()
	rules := replyInstructions(st, "你好", "")
	if strings.Contains(rules, "<voice>") || strings.Contains(rules, "<image") || !strings.Contains(rules, "本轮只用文字") {
		t.Fatal("普通闲聊仍带媒体协议")
	}
}
