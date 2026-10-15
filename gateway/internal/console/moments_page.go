// 文件名：moments_page.go
// 主要功能：适配个人朋友圈、未读动态详情和图文发布，校验微信回执并保留真实 ID。
// 作者：2guan
package console

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	contactapi "github.com/we-clay/we-clay/gateway/internal/api/contact"
	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
	"github.com/we-clay/we-clay/sdk/moments"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strconv"
	"strings"
	"time"
)

func (b *Bridge) momentPage(target, cursor string) (dashboard.MomentPage, error) {
	out := dashboard.MomentPage{Items: []dashboard.Moment{}}
	var id uint64
	var err error
	if cursor != "" {
		id, err = strconv.ParseUint(cursor, 10, 64)
		if err != nil {
			return out, fmt.Errorf("分页标识无效")
		}
	}
	if moments.Instance == nil {
		return out, fmt.Errorf("朋友圈尚未就绪")
	}
	if target == "" {
		r, e := moments.Instance.Timeline("", id)
		if e != nil {
			return out, e
		}
		if r == nil || r.GetBaseResult() == nil || r.GetBaseResult().GetCode() != 0 {
			return out, fmt.Errorf("朋友圈读取失败")
		}
		out.Items = mapMomentItems(r.GetObjectList())
		out.Unread = r.GetUnreadCount()
		out.NextCursor = momentCursor(r.GetObjectList(), 0, id)
		// 合并已读时间线供洞察和已有互动操作使用；个人主页保持独立。
		if b.Dashboard != nil {
			_ = b.Dashboard.SetMoments(out.Items)
		}
	} else {
		r, e := moments.Instance.UserPage(target, "", id)
		if e != nil {
			return out, e
		}
		if r == nil || r.GetBaseResult() == nil || (r.GetBaseResult().GetCode() != 0 && r.GetBaseResult().GetCode() != 207) {
			return out, fmt.Errorf("个人朋友圈读取失败")
		}
		out.Items = mapMomentItems(r.GetObjectList())
		if r.GetBaseResult().GetCode() != 207 {
			out.NextCursor = momentCursor(r.GetObjectList(), r.GetContinueId(), id)
		}
		out.Notice = r.GetReturnTips()
	}
	if cursor != "" && len(out.Items) > 0 && out.NextCursor == "" {
		if out.Notice == "" {
			out.Notice = "微信未返回更早的可见动态"
		}
	}
	return out, nil
}

func (b *Bridge) momentNotifications() (dashboard.MomentPage, error) {
	out := dashboard.MomentPage{Items: []dashboard.Moment{}, Notice: "展示未读提醒关联动态的现有点赞和评论；微信暂未提供独立标记已读接口。"}
	r, err := moments.Instance.Timeline("", 0)
	if err != nil {
		return out, err
	}
	if r == nil || r.GetBaseResult() == nil || r.GetBaseResult().GetCode() != 0 {
		return out, fmt.Errorf("互动提醒读取失败")
	}
	out.Unread = r.GetUnreadCount()
	seen := map[uint64]bool{}
	// 有限逐条读取，避免一次提醒请求拖垮微信接口；部分失败明确显示，不能当成无提醒。
	for _, id := range r.GetUnreadList() {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		if len(seen) > 30 {
			out.Notice += " 本次最多展示 30 条关联动态。"
			break
		}
		detail, e := moments.Instance.Detail(id)
		if e != nil || detail == nil || detail.GetBaseResult() == nil || detail.GetBaseResult().GetCode() != 0 || detail.GetObject() == nil {
			out.Notice += " 部分动态暂无法读取。"
			continue
		}
		out.Items = append(out.Items, mapMomentItems([]*moments.Moment{detail.GetObject()})...)
	}
	if out.Unread > 0 && len(r.GetUnreadList()) == 0 {
		out.Notice += " 微信返回了未读数量，但未提供关联动态。"
	}
	return out, nil
}

// 使用 XML 编码器转义用户文字；浏览器不能直接提交任意 TimelineObject。
type postObject struct {
	XMLName       xml.Name    `xml:"TimelineObject"`
	ID            string      `xml:"id"`
	Username      string      `xml:"username"`
	CreateTime    int64       `xml:"createTime"`
	Content       string      `xml:"contentDesc"`
	Private       int         `xml:"private"`
	ContentObject postContent `xml:"ContentObject"`
}
type postContent struct {
	Style       int         `xml:"contentStyle"`
	Title       string      `xml:"title"`
	Description string      `xml:"description"`
	URL         string      `xml:"contentUrl"`
	Media       []postMedia `xml:"mediaList>media"`
}
type postMedia struct {
	ID    string   `xml:"id"`
	Type  int      `xml:"type"`
	URL   postURL  `xml:"url"`
	Thumb postURL  `xml:"thumb"`
	Size  postSize `xml:"size"`
}
type postURL struct {
	Text string `xml:",chardata"`
	Type uint32 `xml:"type,attr"`
}
type postSize struct {
	Width  int `xml:"width,attr"`
	Height int `xml:"height,attr"`
	Total  int `xml:"totalSize,attr"`
}

func (b *Bridge) publishMoment(params []byte) (dashboard.Moment, error) {
	var q dashboard.MomentPost
	empty := dashboard.Moment{}
	if err := json.Unmarshal(params, &q); err != nil {
		return empty, fmt.Errorf("发布参数无效")
	}
	if len(q.Images) > 9 || len(q.Content) > 10000 || (strings.TrimSpace(q.Content) == "" && len(q.Images) == 0) {
		return empty, fmt.Errorf("朋友圈内容无效")
	}
	self := contact.Instance.GetSelf()
	if self == nil || self.Username == "" {
		return empty, fmt.Errorf("微信未登录")
	}
	obj := postObject{ID: "0", Username: self.Username, CreateTime: time.Now().Unix(), Content: q.Content, ContentObject: postContent{Style: 2}}
	// 先校验所有图片，再调用上传；失败不自动重复上传或发布。
	configs := make([]image.Config, len(q.Images))
	for i, data := range q.Images {
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || (format != "jpeg" && format != "png") || len(data) > 8<<20 || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 40000000 {
			return empty, fmt.Errorf("图片格式或尺寸无效")
		}
		configs[i] = cfg
	}
	for i, data := range q.Images {
		r, err := moments.Instance.Upload(data)
		if err != nil {
			return empty, fmt.Errorf("图片上传失败，尚未发布朋友圈：%w", err)
		}
		if r == nil || r.GetBaseResult() == nil || r.GetBaseResult().GetCode() != 0 || safeWebURL(r.GetBufferUrl().GetUrl()) == "" || r.GetId() == 0 {
			return empty, fmt.Errorf("图片上传回执不完整，尚未发布朋友圈")
		}
		u := r.GetBufferUrl()
		thumb := u
		if len(r.GetThumbUrlList()) > 0 {
			thumb = r.GetThumbUrlList()[0]
		}
		if safeWebURL(thumb.GetUrl()) == "" {
			return empty, fmt.Errorf("图片缩略图回执无效，尚未发布朋友圈")
		}
		obj.ContentObject.Media = append(obj.ContentObject.Media, postMedia{ID: strconv.FormatUint(r.GetId(), 10), Type: 2, URL: postURL{u.GetUrl(), u.GetType()}, Thumb: postURL{thumb.GetUrl(), thumb.GetType()}, Size: postSize{configs[i].Width, configs[i].Height, len(data)}})
	}
	if len(obj.ContentObject.Media) > 0 {
		obj.ContentObject.Style = 1
	}
	body, err := xml.Marshal(obj)
	if err != nil {
		return empty, err
	}
	r, err := moments.Instance.Post(string(body), q.Blacklist, q.WithUsers)
	if err != nil {
		return empty, fmt.Errorf("发布结果未确认，请先在微信核对，不要重复发布：%w", err)
	}
	if r == nil || r.GetBaseResult() == nil || r.GetBaseResult().GetCode() != 0 || r.GetObject().GetId() == 0 {
		return empty, fmt.Errorf("发布回执不完整，请先在微信核对，不要重复发布")
	}
	item := mapMomentItems([]*moments.Moment{r.GetObject()})[0]
	if b.Dashboard != nil {
		_ = b.Dashboard.SetMoments([]dashboard.Moment{item})
	}
	return item, nil
}

// personInfo 只向浏览器返回展示字段；好友关系以 SDK 已确认的关系为准，避免把群成员当作好友。
func (b *Bridge) personInfo(id string) (dashboard.Contact, error) {
	if id == "" || (strings.HasSuffix(id, "@chatroom") || strings.HasSuffix(id, "@im.chatroom")) {
		return dashboard.Contact{}, fmt.Errorf("人员标识无效")
	}
	out := dashboard.Contact{ID: id, Kind: "contact", Tags: []string{}}
	if c := contact.Instance.Get(id); c != nil {
		out.Name = c.GetNickname()
		out.Remark = c.GetRemark()
		out.Avatar = c.GetAvatar()
		out.Alias = c.GetAlias()
		if c.GetType() == contact.ContactType_CONTACT_TYPE_FRIEND {
			out.Kind = "friend"
		}
	}
	result, err := contactapi.Get().Detail([]string{id})
	if err != nil {
		if out.Name != "" || strings.HasSuffix(id, "@openim") {
			if strings.HasSuffix(id, "@openim") {
				out.ProfileNote = "当前 core 未返回企业联系人资料"
			}
			return out, nil
		}
		return out, err
	}
	for _, c := range result.GetContactList() {
		if c.GetUsername().GetValue() != id {
			continue
		}
		out.Name = c.GetNickname().GetValue()
		out.Alias = c.GetAlias()
		out.Signature = c.GetSignature()
		out.Region = strings.TrimSpace(c.GetProvince() + " " + c.GetCity())
		if c.GetRemark().GetValue() != "" {
			out.Remark = c.GetRemark().GetValue()
		}
		if c.GetBigAvatarUrl() != "" {
			out.Avatar = c.GetBigAvatarUrl()
		} else if c.GetSmallAvatarUrl() != "" {
			out.Avatar = c.GetSmallAvatarUrl()
		}
	}
	return out, nil
}

// 使用服务端 continuation 或本页最小 ID，不依赖动态排列顺序；重复游标停止自动翻页。
func momentCursor(items []*moments.Moment, continuation, previous uint64) string {
	if len(items) == 0 {
		return ""
	}
	next := continuation
	if next == 0 {
		for _, m := range items {
			if m.GetId() > 0 && (next == 0 || m.GetId() < next) {
				next = m.GetId()
			}
		}
	}
	if next == 0 || (previous > 0 && next >= previous) {
		return ""
	}
	return strconv.FormatUint(next, 10)
}

// interactMoment 使用真实详情校验作者、点赞状态和评论对象；不要求动态先出现在全局时间线。
func (b *Bridge) interactMoment(c control.Command) (dashboard.Moment, error) {
	empty := dashboard.Moment{}
	id, err := strconv.ParseUint(c.ID, 10, 64)
	if err != nil || id == 0 {
		return empty, fmt.Errorf("动态标识无效")
	}
	before, err := moments.Instance.Detail(id)
	if err != nil {
		return empty, err
	}
	if before == nil || before.GetBaseResult() == nil || before.GetBaseResult().GetCode() != 0 || before.GetObject().GetId() != id {
		return empty, fmt.Errorf("动态暂不可用")
	}
	obj := before.GetObject()
	if c.Target != "" && obj.GetUsername() != c.Target {
		return empty, fmt.Errorf("动态与当前个人主页不匹配")
	}
	var result *moments.Moment
	switch c.Operation {
	case "moments-like":
		if obj.GetLikeFlag() != 0 {
			r, e := moments.Instance.Unlike(id)
			if e != nil {
				return empty, e
			}
			if r == nil || r.GetBaseResult() == nil || r.GetBaseResult().GetCode() != 0 {
				return empty, fmt.Errorf("取消点赞结果未确认，请刷新核对")
			}
			obj.LikeFlag = 0
			if contact.Instance != nil {
				if self := contact.Instance.GetSelf(); self != nil {
					filtered := obj.LikeUserList[:0]
					for _, person := range obj.LikeUserList {
						if person.GetUsername() != self.Username {
							filtered = append(filtered, person)
						}
					}
					obj.LikeUserList = filtered
				}
			}
			if obj.LikeCount > 0 {
				obj.LikeCount--
			}
			result = obj
		} else {
			r, e := moments.Instance.Like(id)
			if e != nil {
				return empty, e
			}
			if r == nil || r.GetBaseResult() == nil || r.GetBaseResult().GetCode() != 0 || r.GetObject().GetId() != id {
				return empty, fmt.Errorf("点赞结果未确认，请刷新核对")
			}
			result = r.GetObject()
		}
	case "moments-comment":
		if strings.TrimSpace(c.Content) == "" || len(c.Content) > 6000 {
			return empty, fmt.Errorf("评论不能为空且不超过 6000 字节")
		}
		r, e := moments.Instance.Comment(id, c.Content, 2, 0)
		if e != nil {
			return empty, e
		}
		if r == nil || r.GetBaseResult() == nil || r.GetBaseResult().GetCode() != 0 || r.GetObject().GetId() != id {
			return empty, fmt.Errorf("评论结果未确认，请刷新核对")
		}
		result = r.GetObject()
	default:
		return empty, fmt.Errorf("不支持的互动操作")
	}
	return mapMomentItems([]*moments.Moment{result})[0], nil
}
