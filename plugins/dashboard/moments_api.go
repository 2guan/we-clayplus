// 文件名：moments_api.go
// 主要功能：提供独立朋友圈浏览、互动提醒和人工图文发布接口，避免个人主页覆盖时间线。
// 作者：2guan
package dashboard

import (
	"encoding/json"
	"fmt"
	"github.com/we-clay/we-clay/sdk/control"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MomentPage 的未读数按真实互动通知及已读进度计算，不把历史评论冒充新提醒。
type MomentPage struct {
	Cover         string               `json:"cover,omitempty"`
	NextCursor    string               `json:"next_cursor"`
	Items         []Moment             `json:"items"`
	Unread        uint32               `json:"unread"`
	Notice        string               `json:"notice,omitempty"`
	Notifications []MomentNotification `json:"notifications,omitempty"`
}
type MomentPost struct {
	Content   string   `json:"content"`
	Images    [][]byte `json:"images"` // JSON 自动按 base64 解码；总请求不超过 24 MB。
	Blacklist []string `json:"blacklist"`
	WithUsers []string `json:"with_users"`
}

// MomentReply 只引用当前动态内的原评论，昵称与正文均由服务端核实。
type MomentReply struct {
	CommentID int32  `json:"reply_comment_id,omitempty"`
	AuthorID  string `json:"reply_author_id,omitempty"`
}

func (q MomentReply) valid() bool {
	return q.CommentID == 0 && q.AuthorID == "" || q.CommentID > 0 && q.AuthorID != ""
}

func momentReplyTarget(m Moment, q MomentReply) (Comment, error) {
	if !q.valid() || q.CommentID <= 0 {
		return Comment{}, fmt.Errorf("回复评论编号或作者无效")
	}
	for _, comment := range m.Comments {
		if comment.ID == fmt.Sprint(q.CommentID) && comment.AuthorID == q.AuthorID {
			return comment, nil
		}
	}
	return Comment{}, fmt.Errorf("所选评论已不可用或作者不匹配，请刷新后重新选择；未改为普通留言")
}

func (s *Server) momentsAPI(w http.ResponseWriter, r *http.Request, u User) {
	if r.URL.Path == "/api/moments/unread" || r.URL.Path == "/api/moments/notifications" {
		s.momentNotificationsAPI(w, r)
		return
	}
	// 真实只读请求无需复制整份消息归档，减少首屏与预加载的内存开销。
	var state State
	if s.cfg.Mock || r.Method != http.MethodGet {
		state = s.store.snapshot()
	}
	if r.Method == http.MethodGet {
		target, cursor := r.URL.Query().Get("target"), r.URL.Query().Get("cursor")
		operation := "moments-page"
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/api/moments/unread" {
			if s.cfg.Mock {
				writeJSON(w, 200, map[string]uint32{"unread": 0})
				return
			}
			operation, target, cursor = "moments-unread", "", ""
		}
		if r.URL.Path == "/api/moments/cover" {
			if s.cfg.Mock {
				writeJSON(w, 200, map[string]string{"cover": state.Account.MomentCover})
				return
			}
			operation = "moments-cover"
		}
		if r.URL.Path == "/api/moments/notifications" {
			operation = "moments-notifications"
		}
		if s.cfg.Mock {
			items := []Moment{}
			for _, m := range state.Moments {
				if target == "" || m.AuthorID == target {
					items = append(items, m)
				}
			}
			notice := ""
			if operation == "moments-notifications" {
				items = []Moment{}
				notice = "脱机模式下暂无微信互动提醒"
			}
			writeJSON(w, 200, MomentPage{Items: items, Notice: notice})
			return
		}
		if s.host == nil {
			writeError(w, 503, "微信尚未就绪")
			return
		}
		result, err := s.host.Execute(r.Context(), control.Command{Operation: operation, Target: target, ID: cursor})
		if err != nil {
			writeError(w, 502, err.Error())
			return
		}
		if page, ok := result.(MomentPage); ok && target == "" {
			account, _ := s.momentNotificationAccount()
			s.momentNotificationMu.Lock()
			v, e := s.loadMomentNotifications(account)
			s.momentNotificationMu.Unlock()
			if e == nil {
				page.Unread = notificationUnread(v)
			}
			result = page
		}
		writeJSON(w, 200, result)
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/api/moments" {
		writeError(w, 405, "请求方法不支持")
		return
	}
	if requireAdmin(u) != nil {
		writeError(w, 403, "仅管理员可以发布朋友圈")
		return
	}
	if !s.momentPublishMu.TryLock() {
		writeError(w, 409, "已有朋友圈正在发布，请勿重复提交")
		return
	}
	defer s.momentPublishMu.Unlock()
	var q MomentPost
	if !decode(w, r, &q) {
		return
	}
	if (strings.TrimSpace(q.Content) == "" && len(q.Images) == 0) || len(q.Content) > 10000 || len(q.Images) > 9 {
		writeError(w, 400, "请输入内容或选择图片，最多 9 张图片、10000 字节文字")
		return
	}
	// 选择器只接受已确认好友，不能通过浏览器篡改范围向任意帐号发送提醒。
	for _, list := range [][]string{q.Blacklist, q.WithUsers} {
		if len(list) > 100 {
			writeError(w, 400, "一次最多选择 100 位好友")
			return
		}
		for _, id := range list {
			valid := false
			for _, c := range state.Contacts {
				if c.ID == id && c.Kind == "friend" {
					valid = true
					break
				}
			}
			if !valid {
				writeError(w, 400, "范围包含非好友，请刷新通讯录")
				return
			}
		}
	}
	for _, image := range q.Images {
		mime := http.DetectContentType(image)
		if len(image) == 0 || len(image) > 8<<20 || (mime != "image/jpeg" && mime != "image/png") {
			writeError(w, 400, "请选择 8 MB 以内的 JPEG 或 PNG 图片")
			return
		}
	}
	if s.cfg.Mock {
		images := []string{}
		for _, image := range q.Images {
			path, err := s.saveMedia(image, http.DetectContentType(image))
			if err != nil {
				writeError(w, 500, "图片保存失败")
				return
			}
			images = append(images, path)
		}
		m := Moment{ID: randomID(), AuthorID: state.Account.ID, Author: state.Account.Name, Avatar: state.Account.Photo, Content: q.Content, Images: images, Comments: []Comment{}, Time: time.Now().Format(time.RFC3339)}
		if err := s.SetMoments([]Moment{m}); err != nil {
			writeError(w, 500, "保存失败")
			return
		}
		writeJSON(w, 200, m)
		return
	}
	if s.host == nil {
		writeError(w, 503, "微信尚未就绪")
		return
	}
	params, _ := json.Marshal(q)
	result, err := s.host.Execute(r.Context(), control.Command{Operation: "moments-publish", Params: params})
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, result)
}

// personAPI 仅允许读取通讯录或归档消息中出现过的人员，防止任意标识探测。
func (s *Server) personAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "请求方法不支持")
		return
	}
	id := r.URL.Query().Get("id")
	state := s.store.snapshot()
	known := id != "" && id == state.Account.ID
	person := Contact{ID: id, Tags: []string{}}
	for _, c := range state.Contacts {
		if c.ID == id && c.Kind != "group" {
			known = true
			person = c
			break
		}
	}
	if !known {
		for _, m := range state.Messages {
			if m.SenderID == id && id != "" {
				known = true
				person.Name = m.Sender
				break
			}
		}
	}
	if !known {
		writeError(w, 404, "人员尚未归档")
		return
	}
	if s.cfg.Mock {
		writeJSON(w, 200, person)
		return
	}
	if s.host == nil {
		writeError(w, 503, "微信尚未就绪")
		return
	}
	result, err := s.host.Execute(r.Context(), control.Command{Operation: "person-info", Target: id})
	if err != nil {
		writeError(w, 502, "人员资料暂无法获取")
		return
	}
	if info, ok := result.(Contact); ok {
		classifyEnterpriseContact(&info)
		if info.Enterprise && info.ProfileNote != "" {
			info.ProfileNote = "当前 core 未返回企业联系人昵称及企业资料"
		}
		result = info
	}
	writeJSON(w, 200, result)
}

// momentInteractionAPI 为时间线和个人主页提供同一套互动接口；请求不能携带任意宿主命令。
func (s *Server) momentInteractionAPI(w http.ResponseWriter, r *http.Request) {
	s.momentInteractionMu.Lock()
	defer s.momentInteractionMu.Unlock()
	if r.Method != http.MethodPost {
		writeError(w, 405, "请求方法不支持")
		return
	}
	var q struct {
		MomentReply
		ID      string `json:"id"`
		Target  string `json:"target"`
		Kind    string `json:"kind"`
		Content string `json:"content"`
	}
	if !decode(w, r, &q) {
		return
	}
	if q.Kind != "moment-like" && q.Kind != "moment-comment" {
		writeError(w, 400, "不支持的互动操作")
		return
	}
	if !q.MomentReply.valid() || q.Kind != "moment-comment" && q.CommentID != 0 {
		writeError(w, 400, "回复评论编号或作者无效")
		return
	}
	if q.Kind == "moment-comment" && (strings.TrimSpace(q.Content) == "" || len(q.Content) > 6000) {
		writeError(w, 400, "评论不能为空且不超过 6000 字节")
		return
	}
	if s.cfg.Mock {
		var item Moment
		err := s.store.change(func(st *State) error {
			for i := range st.Moments {
				m := &st.Moments[i]
				if m.ID == q.ID && (q.Target == "" || q.Target == m.AuthorID) {
					if q.Kind == "moment-like" {
						m.Liked = !m.Liked
						if m.Liked {
							m.LikeUsers = append(m.LikeUsers, MomentLike{ID: st.Account.ID, Name: st.Account.Name})
							m.Likes++
						} else if m.Likes > 0 {
							m.Likes--
							people := m.LikeUsers[:0]
							for _, person := range m.LikeUsers {
								if person.ID != st.Account.ID {
									people = append(people, person)
								}
							}
							m.LikeUsers = people
						}
					} else {
						nextID := int64(1)
						for _, original := range m.Comments {
							if id, err := strconv.ParseInt(original.ID, 10, 32); err == nil && id >= nextID {
								nextID = id + 1
							}
						}
						if nextID > 2147483647 {
							return fmt.Errorf("脱机评论编号已用尽")
						}
						comment := Comment{ID: fmt.Sprint(nextID), AuthorID: st.Account.ID, Author: st.Account.Name, Content: q.Content}
						if q.CommentID > 0 {
							original, err := momentReplyTarget(*m, q.MomentReply)
							if err != nil {
								return err
							}
							comment.ReplyToID, comment.ReplyTo = original.AuthorID, original.Author
						}
						m.Comments = append(m.Comments, comment)
					}
					item = *m
					return nil
				}
			}
			return fmt.Errorf("动态不存在")
		})
		if err != nil {
			writeError(w, 404, err.Error())
			return
		}
		s.notify()
		writeJSON(w, 200, item)
		return
	}
	if s.host == nil {
		writeError(w, 503, "微信尚未就绪")
		return
	}
	operation := "moments-like"
	if q.Kind == "moment-comment" {
		operation = "moments-comment"
	}
	params, _ := json.Marshal(q.MomentReply)
	result, err := s.host.Execute(r.Context(), control.Command{Operation: operation, Target: q.Target, ID: q.ID, Content: q.Content, Params: params})
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, result)
}
