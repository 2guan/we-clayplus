// 文件名：moments.go
// 主要功能：读取 core 的朋友圈时间线并转换为控制台展示结构，不触发发布或互动。
// 作者：2guan
package console

import (
	"encoding/xml"
	"errors"
	"fmt"
	contactapi "github.com/we-clay/we-clay/gateway/internal/api/contact"
	userapi "github.com/we-clay/we-clay/gateway/internal/api/user"
	"github.com/we-clay/we-clay/sdk/contact"
	"log/slog"
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
		commentNames := map[string]string{item.AuthorID: item.Author}
		commentIDs, commentIDs2 := map[int32]string{}, map[uint64]string{}
		for _, c := range object.GetCommentUserList() {
			if c.GetUsername() != "" && c.GetNickname() != "" {
				commentNames[c.GetUsername()] = c.GetNickname()
			}
			if c.GetCommentId() != 0 {
				commentIDs[c.GetCommentId()] = c.GetUsername()
			}
			if c.GetCommentId2() != 0 {
				commentIDs2[c.GetCommentId2()] = c.GetUsername()
			}
		}
		personName := func(id string) string {
			if contact.Instance != nil {
				if self := contact.Instance.GetSelf(); self != nil && self.Username == id && self.Nickname != "" {
					return self.Nickname
				}
				if c := contact.Instance.Get(id); id != "" && c != nil {
					if c.Remark != "" {
						return c.Remark
					}
					if c.Nickname != "" {
						return c.Nickname
					}
				}
			}
			if name := commentNames[id]; name != "" {
				return name
			}
			return "未知用户"
		}
		for _, c := range object.GetCommentUserList() {
			if c == nil || c.GetDeleteFlag() != 0 {
				continue
			}
			replyID := c.GetReplyUsername()
			if replyID == "" && c.GetReplyCommentId2() != 0 {
				replyID = commentIDs2[c.GetReplyCommentId2()]
			}
			if replyID == "" && c.GetReplyCommentId() != 0 {
				replyID = commentIDs[c.GetReplyCommentId()]
			}
			comment := dashboard.Comment{AuthorID: c.GetUsername(), Author: c.GetNickname(), ReplyToID: replyID, Content: c.GetContent()}
			if c.GetCommentId() > 0 {
				comment.ID = strconv.FormatInt(int64(c.GetCommentId()), 10)
			}
			if comment.Author == "" {
				comment.Author = personName(comment.AuthorID)
			}
			if replyID != "" || c.GetReplyCommentId() != 0 || c.GetReplyCommentId2() != 0 {
				comment.ReplyTo = personName(replyID)
			}
			item.Comments = append(item.Comments, comment)
		}
		items = append(items, item)
	}
	return items
}

// momentCover 保留联系人三种来源，再读取自己的 profile 和主页元信息。
// 没有背景与读取失败不同：前者返回空值，后者交给调用方诊断。
func momentCover(username string) (string, error) {
	if username == "" {
		return "", nil
	}
	cover, contactErr := contactMomentCover(username)
	if cover != "" {
		return cover, nil
	}
	var profileErr error
	if contact.Instance != nil && contact.Instance.GetSelf().GetUsername() == username {
		cover, profileErr = selfProfileMomentCover(username)
		if cover != "" {
			return cover, nil
		}
	}
	cover, pageErr := userPageMomentCover(username)
	if cover != "" {
		return cover, nil
	}
	return cover, errors.Join(contactErr, profileErr, pageErr)
}

func selfProfileMomentCover(username string) (string, error) {
	profile, err := userapi.Get().GetProfile()
	if err != nil {
		return "", fmt.Errorf("读取自己的朋友圈封面资料失败: %w", err)
	}
	if profile == nil || profile.GetUserInfo() == nil || profile.GetUserInfo().GetUserName().GetValue() != username || contact.Instance == nil || contact.Instance.GetSelf().GetUsername() != username {
		return "", errors.New("自己的朋友圈封面资料缺失或账号不匹配")
	}
	if raw := profile.GetUserInfo().GetAlbumBgImageId(); strings.TrimSpace(raw) != "" {
		if cover := safeWebURL(raw); cover != "" {
			return cover, nil
		}
		return "", errors.New("自己的朋友圈资料未返回有效封面地址")
	}
	return "", nil
}

func contactMomentCover(username string) (string, error) {
	detail, err := contactapi.Get().Detail([]string{username})
	if err != nil {
		return "", fmt.Errorf("读取朋友圈联系人资料失败: %w", err)
	}
	if detail == nil {
		return "", errors.New("朋友圈联系人资料回执缺失")
	}
	var invalidAddress bool
	for _, c := range detail.GetContactList() {
		if c.GetUsername().GetValue() != username {
			continue
		}
		for _, raw := range []string{c.GetAlbumBgImageId(), c.GetSnsUserInfo().GetBgImageId()} {
			if cover := safeWebURL(raw); cover != "" {
				return cover, nil
			}
			invalidAddress = invalidAddress || strings.TrimSpace(raw) != ""
		}
		if id := c.GetSnsUserInfo().GetBgObjectId(); id != 0 {
			return momentObjectCover(id)
		}
	}
	if invalidAddress {
		return "", errors.New("朋友圈联系人资料未返回有效封面地址")
	}
	return "", nil
}

func userPageMomentCover(username string) (string, error) {
	if moments.Instance == nil {
		return "", errors.New("朋友圈主页服务尚未就绪")
	}
	page, err := moments.Instance.UserPage(username, "", 0)
	if err != nil {
		return "", fmt.Errorf("读取朋友圈主页封面失败: %w", err)
	}
	if page == nil || page.GetBaseResult() == nil || (page.GetBaseResult().GetCode() != 0 && page.GetBaseResult().GetCode() != 207) {
		return "", errors.New("朋友圈主页封面回执不完整")
	}
	var failures []error
	// 元信息与 object_list 独立：没有任何动态、或分页结束 207 时仍可能有封面。
	for _, info := range page.GetSnsUserInfo() {
		if raw := info.GetBgImageId(); strings.TrimSpace(raw) != "" {
			if cover := safeWebURL(raw); cover != "" {
				return cover, nil
			}
			failures = append(failures, errors.New("朋友圈主页未返回有效封面地址"))
		}
		if id := info.GetBgObjectId(); id != 0 {
			cover, err := momentObjectCover(id)
			if cover != "" {
				return cover, nil
			}
			failures = append(failures, err)
		}
	}
	return "", errors.Join(failures...)
}

func momentObjectCover(id uint64) (string, error) {
	if moments.Instance == nil {
		return "", errors.New("朋友圈封面背景动态服务尚未就绪")
	}
	r, err := moments.Instance.Detail(id)
	if err != nil {
		return "", fmt.Errorf("读取朋友圈封面背景动态失败: %w", err)
	}
	if r == nil || r.GetBaseResult() == nil || r.GetBaseResult().GetCode() != 0 || r.GetObject() == nil || r.GetObject().GetId() != id {
		return "", errors.New("朋友圈封面背景动态回执不完整或不匹配")
	}
	var description momentDescription
	if xml.Unmarshal([]byte(r.GetObject().GetObjectDescription()), &description) != nil {
		return "", errors.New("朋友圈封面背景动态内容无法解析")
	}
	for _, cover := range mapMomentItems([]*moments.Moment{r.GetObject()})[0].Images {
		if cover != "" {
			return cover, nil
		}
	}
	return "", errors.New("朋友圈封面背景动态未返回可用图片")
}

// loadMomentCover 在网络前记住账号代次，拒绝迟到的旧账号结果（包括 A→B→A）。
func (b *Bridge) loadMomentCover(username string) (string, error) {
	var account string
	var epoch uint64
	if b.Dashboard != nil && contact.Instance != nil && contact.Instance.GetSelf().GetUsername() == username {
		account, epoch = b.Dashboard.AccountIdentity()
		if account == "" || account != username {
			return "", errors.New("朋友圈封面读取已过期：控制台账号与当前资料不匹配")
		}
	}
	cover, err := momentCover(username)
	if err != nil {
		return "", err
	}
	if account != "" {
		if err := b.Dashboard.SetMomentCoverForAccount(account, epoch, cover); err != nil {
			return "", err
		}
	}
	return cover, nil
}

func (b *Bridge) syncMomentCover() {
	if b.Dashboard != nil && contact.Instance != nil {
		if self := contact.Instance.GetSelf(); self != nil {
			_, err := b.loadMomentCover(self.Username)
			if err != nil {
				slog.Warn("朋友圈封面读取未完成", "error_type", fmt.Sprintf("%T", err))
				return
			}
		}
	}
}
