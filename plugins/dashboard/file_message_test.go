package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/protobuf/encoding/protojson"
)

type fileHost struct {
	t     *testing.T
	calls int
}

func (h *fileHost) Execute(_ context.Context, c control.Command) (any, error) {
	var m message.Message
	if c.Operation != "message-media" || c.Target != "fixture-peer" || c.ID != "9007199254740993" || protojson.Unmarshal(c.Params, &m) != nil || m.GetType().GetCode() != 4906 || m.GetSender().GetUsername() != c.Target || !strings.Contains(m.GetApp().GetXml(), "fixture-attachment") {
		h.t.Fatal("lost original attachment or conversation identity")
	}
	h.calls++
	return []byte("<html>fixture attachment</html>"), nil
}
func TestHistoricalFileDownload(t *testing.T) {
	s := newServer(t, false)
	h := &fileHost{t: t}
	s.host = h
	raw := `<msg><appmsg><type>6</type><title>测试文件.html</title><appattach><attachid>fixture-attachment</attachid><totallen>31</totallen><overwrite_newmsgid>9007199254740992</overwrite_newmsgid></appattach></appmsg></msg>`
	m := Message{ID: "9007199254740993", Target: "fixture-peer", Kind: "app", Content: "旧标题", Source: "wechat"}
	if e := s.store.change(func(v *State) error { v.Account.ID = "fixture-self"; v.Messages = []Message{m}; return nil }); e != nil {
		t.Fatal(e)
	}
	if e := s.RememberQuoteSource(m, raw); e != nil {
		t.Fatal(e)
	}
	if e := s.restoreFileArchive(); e != nil {
		t.Fatal(e)
	}
	m = s.store.snapshot().Messages[0]
	if m.Kind != "file" || m.Content != "测试文件.html" || m.Rich == nil || m.Rich.FileNoticeID != "9007199254740992" {
		t.Fatal("history not repaired")
	}
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), "fixture-attachment") {
		t.Fatal("ticket exposed")
	}
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		s.messageMedia(w, httptest.NewRequest("GET", "/api/message-media?target=fixture-peer&id="+m.ID, nil))
		_, params, e := mime.ParseMediaType(w.Header().Get("Content-Disposition"))
		if e != nil || w.Code != 200 || params["filename"] != m.Content || w.Header().Get("Content-Type") != "application/octet-stream" || w.Body.String() != "<html>fixture attachment</html>" {
			t.Fatalf("download failed %d %v", w.Code, e)
		}
	}
	if h.calls != 1 {
		t.Fatal("cached attachment fetched again")
	}
	// 已识别的历史文件也补回关联字段，保留下载缓存和撤回状态。
	if e := s.store.change(func(v *State) error {
		v.Messages[0].Rich = nil
		v.Messages[0].Status = "revoked"
		v.Messages[0].Content = "消息已撤回"
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e := s.restoreFileArchive(); e != nil {
		t.Fatal(e)
	}
	saved := s.store.snapshot().Messages[0]
	if saved.Rich == nil || saved.Rich.FileNoticeID != "9007199254740992" || saved.Media == "" || saved.Content != "消息已撤回" || saved.Status != "revoked" {
		t.Fatal("file association repair lost existing state")
	}
	w := httptest.NewRecorder()
	s.messageMedia(w, httptest.NewRequest("GET", "/api/message-media?target=other&id="+m.ID, nil))
	if w.Code != 404 {
		t.Fatal("cross conversation download accepted")
	}
	if w = call(t, s, "/api/message-media?target=fixture-peer&id="+m.ID, nil, nil, ""); w.Code != 401 {
		t.Fatal("unauthenticated file access")
	}
	for _, typ := range []int{6, 74} {
		r := strings.Replace(raw, "<type>6</type>", fmt.Sprintf("<type>%d</type>", typ), 1)
		next, _ := ParseWechatMessage(Message{Kind: "app"}, r)
		if next.Kind != "file" || (typ == 74 && (next.Rich == nil || next.Rich.Status != "uploading")) {
			t.Fatal("file notice not classified")
		}
		text, _ := ParseWechatMessage(Message{Kind: "text", Content: r}, r)
		if text.Kind != "text" {
			t.Fatal("plain XML became attachment")
		}
	}
	if !strings.Contains(attachmentDisposition("../report.txt"), "report.txt") || strings.Contains(attachmentDisposition("bad\r\nname"), "\r\n") {
		t.Fatal("unsafe download header")
	}
}
