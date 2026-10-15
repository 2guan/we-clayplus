// 文件名：enterprise_test.go
// 主要功能：验证企业账号分类、历史数据兼容和真实资料保留。
// 作者：2guan
package dashboard

import "testing"

func TestEnterpriseClassification(t *testing.T) {
	for _, tc := range []struct {
		id, kind   string
		enterprise bool
	}{{"fixture@openim", "enterprise", true}, {"fixture@im.chatroom", "group", true}, {"gh_fixture", "official", false}, {"fixture@chatroom", "group", false}} {
		c := Contact{ID: tc.id, Name: tc.id, Kind: tc.kind}
		classifyEnterpriseContact(&c)
		if c.Kind != tc.kind || c.Enterprise != tc.enterprise {
			t.Fatal(c)
		}
	}
	c := Contact{ID: "fixture@openim", Name: "真实昵称", Remark: "已有备注", Avatar: "avatar", Kind: "friend"}
	classifyEnterpriseContact(&c)
	if c.Kind != "enterprise" || c.Name != "真实昵称" || c.Remark != "已有备注" || c.Avatar != "avatar" {
		t.Fatal("覆盖了已获取资料")
	}
	v := State{}
	ensureConversation(&v, Message{Target: "fixture@im.chatroom"})
	if v.Contacts[0].Kind != "group" || !v.Contacts[0].Enterprise {
		t.Fatal("新企业群归类错误")
	}
}
