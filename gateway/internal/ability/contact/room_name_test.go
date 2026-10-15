// 文件名：room_name_test.go
// 主要功能：验证未命名群的成员顺序、十五字截断及有群名时不覆盖。
// 作者：2guan
package contactability

import (
	contactapi "github.com/we-clay/we-clay/gateway/internal/api/contact"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"testing"
)

func TestMemberRoomName(t *testing.T) {
	for _, tt := range []struct {
		names []string
		want  string
	}{
		{[]string{"甲", "乙", "丙"}, "甲、乙、丙"},
		{[]string{"一二三四五六七八九十甲乙丙丁戊"}, "一二三四五六七八九十甲乙丙丁戊"},
		{[]string{"一二三四五六七八九十甲乙丙丁戊己"}, "一二三四五六七八九十甲乙丙丁戊..."},
		{[]string{" ", "乙"}, "乙"},
	} {
		if got := memberRoomName(tt.names); got != tt.want {
			t.Errorf("got %q want %q", got, tt.want)
		}
	}
	data := &contactapi.ModifyContact{Username: wrapperspb.String("fixture@chatroom"), Members: &contactapi.ModifyContact_ChatroomMemberData{List: []*contactapi.ModifyContact_ChatroomMemberData_MemberInfo{{Nickname: proto.String("原昵称"), DisplayName: proto.String("群昵称")}, {Nickname: proto.String("第二位")}}}}
	c, err := Build(data)
	if err != nil || c.Nickname != "群昵称、第二位" {
		t.Fatalf("成员回退失败：%v %v", c, err)
	}
	data.Nickname = wrapperspb.String("已有群名")
	c, err = Build(data)
	if err != nil || c.Nickname != "已有群名" {
		t.Fatal("覆盖真实群名")
	}
}
