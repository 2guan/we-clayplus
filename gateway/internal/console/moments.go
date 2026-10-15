// 文件名：moments.go
// 主要功能：读取 core 的朋友圈时间线并转换为控制台展示结构，不触发发布或互动。
// 作者：2guan
package console

import (
	"encoding/xml"
	"fmt"
	contactapi "github.com/we-clay/we-clay/gateway/internal/api/contact"
	"github.com/we-clay/we-clay/sdk/contact"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/moments"
)

func (b *Bridge) SyncMoments(cursor string) error {
	var maxID uint64
	var err error
	if cursor != "" {
		maxID, err = strconv.ParseUint(cursor, 10, 64)
		if err != nil {
			return fmt.Errorf("朋友圈分页标识无效")
		}
	}
	result, err := moments.Instance.Timeline("", maxID)
	if err != nil {
		return err
	}
	if result == nil || result.GetBaseResult() == nil || result.GetBaseResult().GetCode() != 0 {
		return fmt.Errorf("朋友圈读取未成功，请稍后重试")
	}
	items := mapMomentItems(result.GetObjectList())
	if cursor == "" {
		b.syncMomentCover()
	}
	return b.Dashboard.SetMoments(items)
}

// XML 媒体属性必须一同保留，token 决定可访问性，key 决定能否解密。
type momentImage struct {
	Text  string `xml:",chardata"`
	Key   string `xml:"key,attr"`
	Token string `xml:"token,attr"`
	Index string `xml:"enc_idx,attr"`
}

func (m momentImage) source() string {
	raw := safeWebURL(m.Text)
	if raw == "" {
		return ""
	}
	if m.Token == "" && m.Key == "" {
		return raw
	}
	u, _ := url.Parse(raw)
	q := u.Query()
	if m.Token != "" {
		q.Set("token", m.Token)
		q.Set("idx", m.Index)
	}
	u.RawQuery = q.Encode()
	local := url.Values{"url": {u.String()}}
	if m.Key != "" {
		local.Set("key", m.Key)
	}
	return "/api/wechat-image?" + local.Encode()
}
func safeWebURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.String()
}

type momentDescription struct {
	Content string `xml:"contentDesc"`
	Type    string `xml:"ContentObject>contentStyle"`
	Title   string `xml:"ContentObject>title"`
	Link    string `xml:"ContentObject>contentUrl"`
	Media   []struct {
		Type  string      `xml:"type"`
		URL   momentImage `xml:"url"`
		Thumb momentImage `xml:"thumb"`
	} `xml:"ContentObject>mediaList>media"`
	Finder struct {
		Description string `xml:"desc"`
		Media       []struct {
			Cover string `xml:"coverUrl"`
		} `xml:"mediaList>media"`
	} `xml:"ContentObject>finderFeed"`
}

// mapMomentItems 统一时间线与个人主页的展示解析。
func mapMomentItems(objects []*moments.Moment) []dashboard.Moment {
	items := []dashboard.Moment{}
	for _, object := range objects {
		if object == nil {
			continue
		}
		// 仅提取约定的文字及图片节点，不把微信 XML 当作 HTML 执行。
		var description momentDescription
		if err := xml.Unmarshal([]byte(object.GetObjectDescription()), &description); err != nil {
			description.Content = "[暂无法解析的朋友圈动态]"
		}
		item := dashboard.Moment{ID: strconv.FormatUint(object.GetId(), 10), AuthorID: object.GetUsername(), Author: object.GetNickname(), Content: description.Content, Images: []string{}, Comments: []dashboard.Comment{}, Liked: object.GetLikeFlag() != 0, Likes: int(object.GetLikeCount()), Time: time.Unix(int64(object.GetCreateTime()), 0).Format(time.RFC3339)}
		if item.Author == "" {
			item.Author = item.AuthorID
		}
		// 头像来自已同步的联系人；不把名字的首字冒充头像。
		if contact.Instance != nil {
			if c := contact.Instance.Get(item.AuthorID); c != nil {
				item.Avatar = c.GetAvatar()
			}
		}
		item.Link = safeWebURL(description.Link)
		item.Title = description.Title
		item.Kind = description.Type
		for _, media := range description.Media {
			image := media.Thumb
			if item.Link != "" {
				// 分享卡片优先使用 thumb，不能把文章封面当作照片网格。
				if image.Text == "" {
					image = media.URL
				}
				if item.Thumbnail == "" {
					item.Thumbnail = image.source()
				}
				continue
			}
			// 列表下载小图；原图地址只在点击预览时请求。
			original := image
			if media.Type == "2" {
				original = media.URL
			}
			if image.Text == "" {
				image = original
			}
			if image.Text != "" {
				item.Images = append(item.Images, image.source())
				if original.Text == "" {
					original = image
				}
				item.OriginalImages = append(item.OriginalImages, original.source())
			}
		}
		for _, media := range description.Finder.Media {
			if source := safeWebURL(media.Cover); source != "" {
				item.Images = append(item.Images, source)
				item.OriginalImages = append(item.OriginalImages, source)
			}
		}
		if item.Title == "" {
			item.Title = description.Finder.Description
		}
		// 只用微信返回的点赞名单；备注从本地通讯录补全，不逐个访问微信。
		seen := map[string]bool{}
		for _, like := range object.GetLikeUserList() {
			if like == nil {
				continue
			}
			id, name := like.GetUsername(), like.GetNickname()
			if id != "" && seen[id] {
				continue
			}
			if id != "" {
				seen[id] = true
			}
			if contact.Instance != nil && id != "" {
				if c := contact.Instance.Get(id); c != nil {
					if c.Remark != "" {
						name = c.Remark
					} else if name == "" {
						name = c.Nickname
					}
				}
			}
			if name == "" {
				name = id
			}
			if name == "" {
				continue
			}
			item.LikeUsers = append(item.LikeUsers, dashboard.MomentLike{ID: id, Name: name})
		}
		for _, c := range object.GetCommentUserList() {
			item.Comments = append(item.Comments, dashboard.Comment{Author: c.GetNickname(), Content: c.GetContent()})
		}
		items = append(items, item)
	}
	return items
}

// momentCover 同时兼容旧相册字段、新版 SNS 字段及背景对象引用。
func momentCover(username string) string {
	if username == "" {
		return ""
	}
	detail, err := contactapi.Get().Detail([]string{username})
	if err != nil || detail == nil {
		return ""
	}
	for _, c := range detail.GetContactList() {
		if c.GetUsername().GetValue() != username {
			continue
		}
		for _, raw := range []string{c.GetAlbumBgImageId(), c.GetSnsUserInfo().GetBgImageId()} {
			if cover := safeWebURL(raw); cover != "" {
				return cover
			}
		}
		if id := c.GetSnsUserInfo().GetBgObjectId(); id != 0 && moments.Instance != nil {
			r, e := moments.Instance.Detail(id)
			if e == nil && r != nil && r.GetBaseResult() != nil && r.GetBaseResult().GetCode() == 0 && r.GetObject() != nil {
				items := mapMomentItems([]*moments.Moment{r.GetObject()})
				if len(items[0].Images) > 0 {
					return items[0].Images[0]
				}
			}
		}
	}
	return ""
}
func (b *Bridge) syncMomentCover() {
	if b.Dashboard != nil && contact.Instance != nil {
		if self := contact.Instance.GetSelf(); self != nil {
			if cover := momentCover(self.Username); cover != "" {
				_ = b.Dashboard.SetMomentCover(cover)
			}
		}
	}
}
