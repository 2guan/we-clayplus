// 文件名：reply_list_test.go
// 主要功能：验证好友、群聊的默认黑名单和显式白名单，以及发送阶段同步阻止。
// 作者：2guan
package dashboard

import "testing"

func TestReplyTargetLists(t *testing.T) {
	st := State{Settings: map[string]any{"reply.targets": "friend"}, Contacts: []Contact{{ID: "friend", Kind: "friend"}, {ID: "group", Kind: "group"}, {ID: "official", Kind: "official"}}}
	if !replyTargetAllowed(st, "friend") || !replyTargetAllowed(st, "group") {
		t.Fatal("默认空黑名单未放行好友和群")
	}
	if replyTargetAllowed(st, "official") || replyTargetAllowed(st, "unknown") || replyTargetAllowed(st, "") {
		t.Fatal("错误放行非好友/群目标")
	}
	st.Settings["reply.blacklist"] = " friend , group "
	if replyTargetAllowed(st, "friend") || replyTargetAllowed(st, "group") {
		t.Fatal("黑名单未生效")
	}
	st.Settings["reply.list_mode"] = "白名单"
	if !replyTargetAllowed(st, "friend") || replyTargetAllowed(st, "group") {
		t.Fatal("显式白名单或名单隔离错误")
	}
}
func TestBlacklistStopsDelivery(t *testing.T) {
	s, target := replyServer(t)
	if !s.replyAllowed(target) {
		t.Fatal("自动会话未放行")
	}
	if err := s.store.change(func(v *State) error { v.Settings["reply.blacklist"] = target; return nil }); err != nil {
		t.Fatal(err)
	}
	if s.replyAllowed(target) {
		t.Fatal("加入黑名单后仍允许发送")
	}
}
