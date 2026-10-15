// 文件名：message_delete_test.go
// 主要功能：验证后台消息删除绑定会话、保护发送中回执且不操作真实微信。
// 作者：2guan
package dashboard

import "testing"

func TestDeleteArchivedMessage(t *testing.T) {
	s := newServer(t, true)
	s.store.change(func(st *State) error {
		st.Messages = []Message{{ID: "same", Target: "a", Content: "保留", Status: "sent"}, {ID: "same", Target: "b", Content: "删除", Status: "sent"}, {ID: "pending", Target: "b", Status: "sending"}}
		return nil
	})
	request := func(target, id string) int {
		return call(t, s, "/api/action", map[string]string{"kind": "delete-message", "target": target, "id": id}, nil, "http://127.0.0.1").Code
	}
	if request("b", "pending") == 200 || request("c", "same") == 200 {
		t.Fatal("允许删除发送中或其他会话的消息")
	}
	if request("b", "same") != 200 {
		t.Fatal("删除失败")
	}
	messages := s.store.snapshot().Messages
	if len(messages) != 2 || messages[0].Target != "a" || messages[1].ID != "pending" {
		t.Fatal("删除范围错误")
	}
}
