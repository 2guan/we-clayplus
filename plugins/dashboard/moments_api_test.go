// 文件名：moments_api_test.go
// 主要功能：验证朋友圈发布的角色、好友范围及媒体限制；全部使用脱机环境。
// 作者：2guan
package dashboard

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMomentPublishValidation(t *testing.T) {
	s := newServer(t, true)
	for _, tc := range []struct {
		body, role string
		code       int
	}{
		{`{"content":"测试"}`, "viewer", 403},
		{`{"content":""}`, "admin", 400},
		{`{"content":"测试","blacklist":["not-a-friend"]}`, "admin", 400},
		{`{"content":"测试","images":["YWJj"]}`, "admin", 400},
	} {
		r := httptest.NewRequest("POST", "/api/moments", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.momentsAPI(w, r, User{Role: tc.role})
		if w.Code != tc.code {
			t.Fatalf("预期 %d，实际 %d", tc.code, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/person?id=unknown", nil)
	w := httptest.NewRecorder()
	s.personAPI(w, r)
	if w.Code != 404 {
		t.Fatal("接受未归档人员")
	}
}
