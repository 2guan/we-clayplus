// 文件名：messages_test.go
// 主要功能：验证 SDK 只保留标题时，控制台仍从 AppData 归档原始文章链接。
// 作者：2guan
package console

import (
	"context"
	"encoding/json"
	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/message"
	"net/http/httptest"
	"strings"
	"testing"
)

type messageContactFixture struct{ contact.Ability }

func (messageContactFixture) GetSelf() *contact.SelfInfo {
	return &contact.SelfInfo{Username: "fixture-self"}
}
func TestRecordPreservesArticleAppData(t *testing.T) {
	old := contact.Instance
	contact.Instance = messageContactFixture{}
	defer func() { contact.Instance = old }()
	s, err := dashboard.Start(context.Background(), control.Config{Disabled: true, DataDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b := &Bridge{Dashboard: s}
	err = b.Record(&message.Message{Id: 1, Type: message.TypeAppLink, Sender: &contact.Contact{Username: "gh_fixture"}, Receiver: &contact.Contact{Username: "fixture-self"}, Content: "只有标题", Data: &message.Message_App{App: &message.AppData{SubType: 5, Title: "只有标题", Url: "https://example.com/article", Xml: `<msg><appmsg><title>只有标题</title><type>5</type><url>https://example.com/article</url></appmsg></msg>`}}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "http://local/api/auth/setup", strings.NewReader(`{"name":"fixture-admin","password":"abc123"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	req = httptest.NewRequest("GET", "http://local/api/state", nil)
	req.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	var state dashboard.State
	if err = json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 1 || len(state.Messages[0].Articles) != 1 || state.Messages[0].Articles[0].URL != "https://example.com/article" {
		t.Fatal("文章被降级为纯标题")
	}
}
