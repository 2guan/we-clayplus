// 文件名：reply_modes_test.go
// 主要功能：验证默认回复模式、旧状态兼容和专属策略优先级。
// 作者：2guan
package dashboard

import "testing"

func TestReplyModeDefaults(t *testing.T) {
	v := State{Settings: map[string]any{"reply.default_friend": "自动", "reply.default_group": "人工"}, Overrides: map[string]map[string]any{"paused": {"mode": "paused"}}, Contacts: []Contact{
		{ID: "new-friend", Kind: "friend", Mode: "paused"},
		{ID: "new-group", Kind: "group", Mode: "paused"},
		{ID: "manual", Kind: "friend", Mode: "manual"},
		{ID: "auto", Kind: "group", Mode: "auto"},
		{ID: "paused", Kind: "friend", Mode: "paused"},
		{ID: "official", Kind: "official", Mode: "paused"},
	}}
	resolveReplyModes(&v)
	for i, want := range []string{"auto", "manual", "manual", "auto", "manual", "manual"} {
		if v.Contacts[i].Mode != want {
			t.Fatalf("case %d: %s", i, v.Contacts[i].Mode)
		}
	}
	if !v.Contacts[0].ModeInherited || !v.Contacts[1].ModeInherited || v.Contacts[2].ModeInherited || v.Contacts[4].ModeInherited {
		t.Fatal("默认与专属模式标识错误")
	}
}

// 解析后的模式不能污染持久化状态，否则下一次修改默认值将无法继续继承。
func TestReplyModeInheritancePersists(t *testing.T) {
	dir := t.TempDir()
	s, err := openStore(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.change(func(v *State) error {
		v.Contacts = []Contact{{ID: "fixture", Kind: "friend", Mode: "paused"}}
		v.Settings["reply.default_friend"] = "自动"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if s.snapshot().Contacts[0].Mode != "auto" {
		t.Fatal("默认自动未生效")
	}
	if s.state.Contacts[0].Mode != "paused" {
		t.Fatal("读取污染存储")
	}
	s.db.Close()
	s, err = openStore(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.db.Close()
	if !s.snapshot().Contacts[0].ModeInherited {
		t.Fatal("重启丢失继承关系")
	}
	if err = s.change(func(v *State) error { v.Settings["reply.default_friend"] = "人工"; return nil }); err != nil {
		t.Fatal(err)
	}
	if s.snapshot().Contacts[0].Mode != "manual" {
		t.Fatal("修改默认值未生效")
	}
}
