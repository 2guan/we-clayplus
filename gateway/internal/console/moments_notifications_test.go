package console

import (
	"context"
	"database/sql"
	momentsapi "github.com/we-clay/we-clay/gateway/internal/api/moments"
	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/message"
	"github.com/we-clay/we-clay/sdk/moments"
	"google.golang.org/protobuf/proto"
	"path/filepath"
	"testing"
)

func TestDeviceMomentReadStatusSavedBeforeChatFilter(t *testing.T) {
	dir := t.TempDir()
	s, err := dashboard.Start(context.Background(), control.Config{Disabled: true, DataDir: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SetAccount(dashboard.Account{ID: "fixture-self", Status: "online"}); err != nil {
		t.Fatal(err)
	}
	old := contact.Instance
	contact.Instance = postContacts{}
	defer func() { contact.Instance = old }()
	m := &message.Message{Type: message.TypeStatusNotify, Sender: &contact.Contact{Username: "fixture-self"}, Receiver: &contact.Contact{Username: "fixture-self"}, Content: `<msg><op id="7"><name>MomentsUnreadMsgStatus</name><arg>100</arg></op></msg>`}
	if err = (&Bridge{Dashboard: s}).Record(m); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "console.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM secrets WHERE id=?", "moments-notifications:fixture-self").Scan(&count); err != nil || count != 1 {
		t.Fatal("设备已读状态被过滤掉", count, err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM state_rows WHERE account=? AND kind='messages'", "fixture-self").Scan(&count); err != nil || count != 0 {
		t.Fatal("状态同步进入聊天归档", count, err)
	}
}

type notificationFixture struct {
	moments.Ability
	commands []*moments.Command
}

func (f notificationFixture) Sync([]byte) (*moments.Sync_Response, error) {
	return &moments.Sync_Response{BaseResult: &moments.BaseResult{}, Commands: f.commands, Key: &moments.Buffer{Data: []byte("fixture-key")}}, nil
}
func TestMomentNotificationSyncUsesActionsAndRecipient(t *testing.T) {
	oldM, oldC := moments.Instance, contact.Instance
	defer func() { moments.Instance, contact.Instance = oldM, oldC }()
	contact.Instance = postContacts{}
	commands := []*moments.Command{}
	for _, target := range []string{"fixture-self", "fixture-other"} {
		body, _ := proto.Marshal(&momentsapi.ActionNotification{Id: 18446744073709551614, CurrentAction: &momentsapi.NotificationAction{Sender: "fixture-peer", Receiver: target, SenderNickname: "回复者", CreateTime: 110, Type: 2, CommentId: 35, Content: "具体回复"}, ReferAction: &momentsapi.NotificationAction{Sender: target, Type: 2, Content: "本人原评论"}})
		commands = append(commands, &moments.Command{Id: 46, Buffer: &moments.Buffer{Data: body}})
	}
	moments.Instance = notificationFixture{commands: commands}
	p, err := (&Bridge{}).syncMomentNotifications(nil)
	if err != nil || len(p.Items) != 1 || p.Items[0].MomentID != "18446744073709551614" || p.Items[0].Actor != "回复者" || p.Items[0].Original != "本人原评论" || p.Items[0].Content != "具体回复" || p.Items[0].ID == "" || !p.Items[0].ReplyToSelf || p.Items[0].CommentID != 35 {
		t.Fatal(p, err)
	}
}
