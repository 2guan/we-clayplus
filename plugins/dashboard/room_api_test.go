// 文件名：room_api_test.go
// 主要功能：验证群资料只读入口的目标校验与脱机响应。
// 作者：2guan
package dashboard

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestRoomAPI(t *testing.T) {
	s := newServer(t, true)
	id := ""
	for _, c := range s.store.snapshot().Contacts {
		if c.Kind == "group" {
			id = c.ID
			break
		}
	}
	for _, tc := range []struct {
		method, id string
		code       int
	}{{"GET", id, 200}, {"GET", "unknown", 404}, {"POST", id, 405}} {
		w := httptest.NewRecorder()
		s.roomAPI(w, httptest.NewRequest(tc.method, "/api/room?id="+tc.id, nil))
		if w.Code != tc.code {
			t.Fatalf("%s %s: %d", tc.method, tc.id, w.Code)
		}
		if w.Code == 200 {
			var r RoomInfo
			if json.Unmarshal(w.Body.Bytes(), &r) != nil || len(r.Members) == 0 || r.Announcement == "" {
				t.Fatal("群资料缺失")
			}
		}
	}
}
