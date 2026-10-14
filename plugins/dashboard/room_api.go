// 文件名：room_api.go
// 主要功能：提供已归档群聊资料的只读入口及独立脱机演示数据。
// 作者：2guan
package dashboard

import (
	"github.com/we-clay/we-clay/sdk/control"
	"net/http"
)

type RoomMember struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Avatar      string `json:"avatar"`
	DisplayName string `json:"display_name"`
}
type RoomInfo struct {
	Name         string       `json:"name"`
	Announcement string       `json:"announcement"`
	Notice       string       `json:"notice,omitempty"`
	SelfName     string       `json:"self_name"`
	Members      []RoomMember `json:"members"`
}

func (s *Server) roomAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "请求方法不支持")
		return
	}
	id := r.URL.Query().Get("id")
	st := s.store.snapshot()
	var room *Contact
	for _, c := range st.Contacts {
		if c.ID == id && c.Kind == "group" {
			copy := c
			room = &copy
			break
		}
	}
	if room == nil {
		writeError(w, 404, "群聊尚未归档")
		return
	}
	if s.cfg.Mock {
		out := RoomInfo{Name: room.Name, Announcement: "这是一条脱机演示群公告。", SelfName: st.Account.Name, Members: []RoomMember{}}
		for _, c := range st.Contacts {
			if c.Kind == "friend" {
				out.Members = append(out.Members, RoomMember{ID: c.ID, Name: c.Name, Avatar: c.Avatar, DisplayName: c.Remark})
			}
		}
		writeJSON(w, 200, out)
		return
	}
	if s.host == nil {
		writeError(w, 503, "微信尚未就绪")
		return
	}
	out, err := s.host.Execute(r.Context(), control.Command{Operation: "room-info", Target: id})
	if err != nil {
		writeError(w, 502, "群资料暂无法获取，请重试")
		return
	}
	writeJSON(w, 200, out)
}
