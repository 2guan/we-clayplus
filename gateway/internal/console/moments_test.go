// 文件名：moments_test.go
// 主要功能：验证朋友圈分页参数、64 位标识与 XML 文字图片转换。
// 作者：2guan
package console

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/moments"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type momentFixture struct {
	moments.Ability
	cursor uint64
}

type momentCommentContacts struct{ contact.Ability }

func (momentCommentContacts) GetSelf() *contact.SelfInfo {
	return &contact.SelfInfo{Username: "fixture-self", Nickname: "本人昵称"}
}
func (momentCommentContacts) Get(id string) *contact.Contact {
	if id == "fixture-peer" {
		return &contact.Contact{Username: id, Nickname: "好友昵称", Remark: "好友备注"}
	}
	return nil
}

func TestMomentCommentReplyIdentity(t *testing.T) {
	previous := contact.Instance
	contact.Instance = momentCommentContacts{}
	defer func() { contact.Instance = previous }()
	items := mapMomentItems([]*moments.Moment{{Username: "fixture-author", Nickname: "动态作者", ObjectDescription: "<TimelineObject/>", CommentUserList: []*moments.CommentInfo{
		{Username: "fixture-self", Nickname: "旧昵称", CommentId: 7, CommentId2: 18446744073709551610, Content: "原评论"},
		{Username: "fixture-peer", Nickname: "回复者", ReplyUsername: "fixture-self", ReplyCommentId: 7, Content: "直接回复"},
		{Username: "fixture-self", Nickname: "本人昵称", ReplyCommentId: 9, Content: "按评论编号回复"},
		{Username: "fixture-peer", Nickname: "回复者", CommentId: 9, ReplyCommentId2: 18446744073709551610, Content: "按64位编号回复"},
		{Nickname: "第三位", ReplyUsername: "fixture-author", Content: "回复作者"},
		{Nickname: "第三位", ReplyCommentId: 12345, Content: "原评论已不可见"},
		nil,
	}}})
	cs := items[0].Comments
	if len(cs) != 6 || cs[0].ReplyTo != "" || cs[1].ReplyTo != "本人昵称" || cs[1].ReplyToID != "fixture-self" || cs[1].AuthorID != "fixture-peer" || cs[2].ReplyTo != "好友备注" || cs[3].ReplyTo != "本人昵称" || cs[4].ReplyTo != "动态作者" || cs[5].ReplyTo != "未知用户" {
		t.Fatalf("回复关系错误：%+v", cs)
	}
}

func (m *momentFixture) Timeline(_ string, id uint64) (*moments.Timeline_Response, error) {
	m.cursor = id
	return &moments.Timeline_Response{BaseResult: &moments.BaseResult{Code: 0}, ObjectList: []*moments.Moment{{Id: 18446744073709551613, Username: "fixture-author", Nickname: "测试作者", CreateTime: 1700000000, ObjectDescription: `<TimelineObject><contentDesc>测试文字 &amp; 内容</contentDesc><ContentObject><mediaList><media><type>2</type><url>https://mmsns.qpic.cn/fixture</url></media></mediaList></ContentObject></TimelineObject>`}}}, nil
}
func TestSyncMomentsReadAndMap(t *testing.T) {
	fixture := &momentFixture{}
	previous := moments.Instance
	moments.Instance = fixture
	defer func() { moments.Instance = previous }()
	server, err := dashboard.Start(context.Background(), control.Config{Disabled: true, DataDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	bridge := &Bridge{Dashboard: server}
	if err := bridge.SyncMoments("18446744073709551614"); err != nil {
		t.Fatal(err)
	}
	if fixture.cursor != 18446744073709551614 {
		t.Fatal("游标精度丢失")
	}
	setup := httptest.NewRequest("POST", "http://127.0.0.1/api/auth/setup", strings.NewReader(`{"name":"fixture-admin","password":"abc123"}`))
	setup.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, setup)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	req := httptest.NewRequest("GET", "http://127.0.0.1/api/state", nil)
	req.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	var state dashboard.State
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Moments) != 1 || state.Moments[0].ID != "18446744073709551613" || state.Moments[0].Content != "测试文字 & 内容" || len(state.Moments[0].Images) != 1 {
		t.Fatal("动态字段映射错误")
	}
}

// 大整数解密密钥始终是字符串，不能通过浮点数编码；不允许脚本链接。
func TestMomentImageMetadataAndLink(t *testing.T) {
	var description momentDescription
	raw := `<TimelineObject><ContentObject><title>测试文章</title><contentUrl>https://example.com/article</contentUrl><mediaList><media><type>2</type><url key="18446744073709551613" token="fixture-token" enc_idx="1">https://shmmsns.qpic.cn/fixture</url></media></mediaList></ContentObject></TimelineObject>`
	if err := xml.Unmarshal([]byte(raw), &description); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(description.Media[0].URL.source())
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/api/wechat-image" || u.Query().Get("key") != "18446744073709551613" || !strings.Contains(u.Query().Get("url"), "token=fixture-token") {
		t.Fatal("加密图片字段丢失")
	}
	if description.Title != "测试文章" || safeWebURL(description.Link) == "" || safeWebURL("javascript:alert(1)") != "" {
		t.Fatal("链接解析错误")
	}
}
