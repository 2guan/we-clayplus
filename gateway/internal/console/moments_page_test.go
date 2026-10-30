// 文件名：moments_page_test.go
// 主要功能：离线验证个人分页、未读关联详情、图文 XML 及发布回执；不访问真实微信。
// 作者：2guan
package console

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/moments"
	"image"
	"image/png"
	"strings"
	"testing"
)

type pageFixture struct {
	moments.Ability
	username string
	cursor   uint64
	details  []uint64
}

func (f *pageFixture) UserPage(u, _ string, id uint64) (*moments.UserPage_Response, error) {
	f.username = u
	f.cursor = id
	return &moments.UserPage_Response{BaseResult: &moments.BaseResult{Code: 207}, ObjectList: []*moments.Moment{{Id: 99, Username: u, ObjectDescription: `<TimelineObject><contentDesc>个人动态</contentDesc></TimelineObject>`}}}, nil
}
func (f *pageFixture) Timeline(_ string, _ uint64) (*moments.Timeline_Response, error) {
	return &moments.Timeline_Response{BaseResult: &moments.BaseResult{}, UnreadCount: 2, UnreadList: []uint64{99, 99, 100}}, nil
}
func (f *pageFixture) Detail(id uint64) (*moments.Detail_Response, error) {
	f.details = append(f.details, id)
	return &moments.Detail_Response{BaseResult: &moments.BaseResult{}, Object: &moments.Moment{Id: id, ObjectDescription: `<TimelineObject><contentDesc>互动动态</contentDesc></TimelineObject>`, CommentUserList: []*moments.CommentInfo{{Nickname: "测试好友", Content: "评论"}}}}, nil
}
func TestPersonalPageRead(t *testing.T) {
	previous := moments.Instance
	f := &pageFixture{}
	moments.Instance = f
	defer func() { moments.Instance = previous }()
	b := &Bridge{}
	page, err := b.momentPage("fixture-member", "18446744073709551614")
	if err != nil || f.username != "fixture-member" || f.cursor != 18446744073709551614 || len(page.Items) != 1 || page.Items[0].AuthorID != "fixture-member" {
		t.Fatalf("个人分页失败: %v", err)
	}
	if _, err = b.momentPage("fixture", "bad"); err == nil {
		t.Fatal("接受非法游标")
	}

}
func TestPostXMLSafeTextAndMedia(t *testing.T) {
	obj := postObject{Username: "fixture", Content: `正文 </contentDesc><private>1</private> &`, ContentObject: postContent{Style: 1, Media: []postMedia{{ID: "18446744073709551614", Type: 2, URL: postURL{"https://mmsns.qpic.cn/a?x=1&y=2", 1}, Size: postSize{400, 300, 1234}}}}}
	b, err := xml.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	var result postObject
	if err = xml.Unmarshal(b, &result); err != nil {
		t.Fatal(err)
	}
	if result.Content != obj.Content || result.Private != 0 || result.ContentObject.Media[0].ID != "18446744073709551614" || !strings.Contains(string(b), "&amp;") {
		t.Fatal("XML 转义或大整数媒体标识丢失")
	}
	// base64 传输保持图片原字节，不把 URL 当作可上传的文件。
	raw, _ := json.Marshal(dashboard.MomentPost{Images: [][]byte{{1, 2, 3}}})
	var q dashboard.MomentPost
	if err = json.Unmarshal(raw, &q); err != nil || len(q.Images) != 1 || q.Images[0][2] != 3 {
		t.Fatal("媒体传输失败")
	}
}

type postContacts struct{ contact.Ability }

func (postContacts) GetSelf() *contact.SelfInfo  { return &contact.SelfInfo{Username: "fixture-self"} }
func (postContacts) Get(string) *contact.Contact { return nil }

type postFixture struct {
	moments.Ability
	content              string
	calls                int
	missing              bool
	blacklist, withUsers []string
}

func (f *postFixture) Post(content string, blacklist, withUsers []string) (*moments.Post_Response, error) {
	f.calls++
	f.content = content
	f.blacklist = blacklist
	f.withUsers = withUsers
	if f.missing {
		return &moments.Post_Response{}, nil
	}
	return &moments.Post_Response{BaseResult: &moments.BaseResult{}, Object: &moments.Moment{Id: 18446744073709551614, Username: "fixture-self", ObjectDescription: content}}, nil
}
func TestPublishValidatesReceipt(t *testing.T) {
	oldM, oldC := moments.Instance, contact.Instance
	defer func() { moments.Instance = oldM; contact.Instance = oldC }()
	f := &postFixture{}
	moments.Instance = f
	contact.Instance = postContacts{}
	q, _ := json.Marshal(dashboard.MomentPost{Content: "文字 & <标签>", Blacklist: []string{"fixture-hidden"}, WithUsers: []string{"fixture-mentioned"}})
	item, err := (&Bridge{}).publishMoment(q)
	if err != nil || item.ID != "18446744073709551614" || item.Content != "文字 & <标签>" || len(f.blacklist) != 1 || len(f.withUsers) != 1 {
		t.Fatalf("发布适配失败: %v", err)
	}
	f.missing = true
	if _, err = (&Bridge{}).publishMoment(q); err == nil || f.calls != 2 {
		t.Fatal("缺失回执被当作成功或自动重试")
	}
}

// 上传返回的地址、媒体 ID 和图片实际尺寸必须写入发布 XML，不能使用固定成功体替代此链路检查。
func (f *postFixture) Upload(data []byte) (*moments.Upload_Response, error) {
	return &moments.Upload_Response{BaseResult: &moments.BaseResult{}, Id: 18446744073709551612, BufferUrl: &moments.BufferUrl{Url: "https://mmsns.qpic.cn/fixture-original", Type: 1}, ThumbUrlList: []*moments.BufferUrl{{Url: "https://mmsns.qpic.cn/fixture-thumb", Type: 1}}}, nil
}
func TestPublishImageReceiptFields(t *testing.T) {
	oldM, oldC := moments.Instance, contact.Instance
	defer func() { moments.Instance = oldM; contact.Instance = oldC }()
	f := &postFixture{}
	moments.Instance = f
	contact.Instance = postContacts{}
	var file bytes.Buffer
	if err := png.Encode(&file, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	q, _ := json.Marshal(dashboard.MomentPost{Images: [][]byte{file.Bytes()}})
	if _, err := (&Bridge{}).publishMoment(q); err != nil {
		t.Fatal(err)
	}
	var obj postObject
	if err := xml.Unmarshal([]byte(f.content), &obj); err != nil {
		t.Fatal(err)
	}
	if obj.ContentObject.Style != 1 || len(obj.ContentObject.Media) != 1 {
		t.Fatal("图片内容类型错误")
	}
	m := obj.ContentObject.Media[0]
	if m.ID != "18446744073709551612" || m.URL.Text != "https://mmsns.qpic.cn/fixture-original" || m.Thumb.Text != "https://mmsns.qpic.cn/fixture-thumb" || m.Size.Width != 4 || m.Size.Height != 3 || m.Size.Total != file.Len() {
		t.Fatal("上传回执到发布 XML 的字段丢失")
	}
}

// ID 大于 JavaScript 安全整数范围，分页仍必须准确；回传顺序不决定游标。
func TestMomentCursorAndLinkCard(t *testing.T) {
	items := []*moments.Moment{{Id: 18446744073709551610}, {Id: 18446744073709551612}}
	if momentCursor(items, 0, 0) != "18446744073709551610" || momentCursor(items, 18446744073709551609, 18446744073709551610) != "18446744073709551609" || momentCursor(items, 0, 18446744073709551610) != "" || momentCursor(nil, 99, 100) != "" {
		t.Fatal("分页游标或终止条件错误")
	}
	row := mapMomentItems([]*moments.Moment{{ObjectDescription: `<TimelineObject><ContentObject><contentStyle>3</contentStyle><title>文章标题</title><contentUrl>https://example.com/article</contentUrl><mediaList><media><type>2</type><url>https://mmsns.qpic.cn/large</url><thumb>https://mmsns.qpic.cn/small</thumb></media></mediaList></ContentObject></TimelineObject>`}})[0]
	if len(row.Images) != 0 || row.Thumbnail != "https://mmsns.qpic.cn/small" || row.Title != "文章标题" {
		t.Fatal("链接缩略图混入照片列表")
	}
}

type interactionFixture struct {
	moments.Ability
	writes int
	wrong  bool
	liked  bool
}

func (f *interactionFixture) Detail(id uint64) (*moments.Detail_Response, error) {
	var flag uint32
	if f.liked {
		flag = 1
	}
	return &moments.Detail_Response{BaseResult: &moments.BaseResult{}, Object: &moments.Moment{Id: id, Username: "fixture-author", Nickname: "测试作者", CreateTime: 1700000000, LikeFlag: flag, LikeCount: flag, ObjectDescription: `<TimelineObject><contentDesc>测试动态正文</contentDesc><ContentObject><contentStyle>1</contentStyle><mediaList><media><type>2</type><url>https://mmsns.qpic.cn/original</url><thumb>https://mmsns.qpic.cn/thumb</thumb></media></mediaList></ContentObject></TimelineObject>`}}, nil
}
func (f *interactionFixture) Like(id uint64) (*moments.Like_Response, error) {
	f.writes++
	if f.wrong {
		id++
	}
	return &moments.Like_Response{BaseResult: &moments.BaseResult{}, Object: &moments.Moment{Id: id, Username: "fixture-author", LikeFlag: 1, LikeCount: 1}}, nil
}
func (f *interactionFixture) Comment(id uint64, text string, typ, reply int32) (*moments.Comment_Response, error) {
	f.writes++
	if typ != 2 || reply != 0 {
		panic("评论参数错误")
	}
	return &moments.Comment_Response{BaseResult: &moments.BaseResult{}, Object: &moments.Moment{Id: id, Username: "fixture-author", CommentUserList: []*moments.CommentInfo{{Content: text}}}}, nil
}
func (f *interactionFixture) Unlike(id uint64) (*moments.Unlike_Response, error) {
	f.writes++
	return &moments.Unlike_Response{BaseResult: &moments.BaseResult{}}, nil
}
func TestPersonalMomentInteraction(t *testing.T) {
	old := moments.Instance
	defer func() { moments.Instance = old }()
	f := &interactionFixture{}
	moments.Instance = f
	b := &Bridge{}
	checkContent := func(m dashboard.Moment) {
		t.Helper()
		if m.Content != "测试动态正文" || len(m.Images) != 1 || m.Images[0] != "https://mmsns.qpic.cn/thumb" || len(m.OriginalImages) != 1 || m.OriginalImages[0] != "https://mmsns.qpic.cn/original" || m.AuthorID != "fixture-author" || m.Author != "测试作者" || m.Time == "" || strings.HasPrefix(m.Time, "1970-") {
			t.Fatal("部分互动回执覆盖了动态正文、配图或作者时间")
		}
	}
	q := control.Command{Operation: "moments-like", ID: "123", Target: "wrong-author"}
	if _, err := b.interactMoment(q); err == nil || f.writes != 0 {
		t.Fatal("作者不匹配仍写入")
	}
	q.Target = "fixture-author"
	m, err := b.interactMoment(q)
	if err != nil || !m.Liked {
		t.Fatal("个人主页点赞未生效")
	}
	checkContent(m)
	q.Operation = "moments-comment"
	q.Content = "测试评论"
	m, err = b.interactMoment(q)
	if err != nil || len(m.Comments) != 1 || m.Comments[0].Content != q.Content {
		t.Fatal("评论回执映射失败")
	}
	checkContent(m)
	q.Operation = "moments-like"
	f.liked = true
	m, err = b.interactMoment(q)
	if err != nil || m.Liked || m.Likes != 0 {
		t.Fatal("取消点赞回执映射失败")
	}
	checkContent(m)
	f.liked = false
	q.Operation = "moments-like"
	f.wrong = true
	if _, err = b.interactMoment(q); err == nil {
		t.Fatal("接受不匹配回执")
	}
}

// 名单去重、缩略图和原图分别映射，避免列表阶段下载大图。
func TestMomentPeopleAndSmallImages(t *testing.T) {
	rows := mapMomentItems([]*moments.Moment{{LikeCount: 3, LikeUserList: []*moments.CommentInfo{{Username: "fixture-a", Nickname: "小明"}, {Username: "fixture-a", Nickname: "小明"}, {Username: "fixture-b", Nickname: "小红"}}, ObjectDescription: `<TimelineObject><ContentObject><contentStyle>1</contentStyle><mediaList><media><type>2</type><url>https://mmsns.qpic.cn/original</url><thumb>https://mmsns.qpic.cn/thumb</thumb></media></mediaList></ContentObject></TimelineObject>`}})
	m := rows[0]
	if len(m.LikeUsers) != 2 || m.LikeUsers[0].Name != "小明" || m.Images[0] != "https://mmsns.qpic.cn/thumb" || m.OriginalImages[0] != "https://mmsns.qpic.cn/original" {
		t.Fatal("名单或图片分流失败")
	}
}
