package console

import (
	"context"
	"encoding/json"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/sdk/message"
)

func TestFriendRequestParserPreservesAllPublicFields(t *testing.T) {
	m := &message.Message{Id: 17, Timestamp: 1700000000, Content: `<msg fromusername="fixture-peer" fromnickname="申请昵称" encryptusername="fixture-v1" ticket="fixture-ticket" content="第一行&#10;第二行：请加我" scene="3" alias="申请微信号" city="测试城市" bigheadimgurl="https://example.com/avatar.jpg"><sourcenickname>共同好友</sourcenickname><custom_info title="额外资料">完整附加内容</custom_info><auth_token>secret-token</auth_token><antispamticket>secret-spam</antispamticket></msg>`}
	q, err := parseFriendRequest(m)
	if err != nil || q.V1 != "fixture-v1" || q.V2 != "fixture-ticket" || q.Comment != "第一行\n第二行：请加我" || q.Nickname != "申请昵称" || q.Avatar != "https://example.com/avatar.jpg" || q.Created == "" {
		t.Fatal(q, err)
	}
	raw, _ := json.Marshal(q.Details)
	for _, v := range []string{"fixture-v1", "fixture-ticket", "secret-token", "secret-spam"} {
		if strings.Contains(string(raw), v) {
			t.Fatal("验证凭据进入展示字段")
		}
	}
	for _, v := range []string{"额外资料", "完整附加内容", "共同好友", "申请微信号", "测试城市"} {
		if !strings.Contains(string(raw), v) {
			t.Fatal("遗漏申请内容", v)
		}
	}
}

func TestFriendVerifyAccountFence(t *testing.T) {
	old := contact.Instance
	contact.Instance = postContacts{}
	defer func() { contact.Instance = old }()
	b := &Bridge{}
	b.Ready.Store(true)
	if _, err := b.Execute(context.Background(), control.Command{Operation: "friend-verify", Params: json.RawMessage(`{"v1":"fixture-v1","v2":"fixture-ticket","scene":3,"account":"fixture-other"}`)}); err == nil {
		t.Fatal("允许跨账号通过")
	}
}
