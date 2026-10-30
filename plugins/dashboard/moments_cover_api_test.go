package dashboard

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/sdk/control"
)

func TestMomentCoverAPIDistinguishesReadFailureFromMissingCover(t *testing.T) {
	for _, failed := range []bool{false, true} {
		s := newServer(t, false)
		s.host = replyRegressionHost(func(_ context.Context, c control.Command) (any, error) {
			if c.Operation != "moments-cover" || c.Target != "fixture-cover-self" {
				t.Fatal("封面接口调用了错误的宿主操作")
			}
			if failed {
				return nil, errors.New("fixture cover read unavailable")
			}
			return map[string]string{"cover": ""}, nil
		})
		w := httptest.NewRecorder()
		s.momentsAPI(w, httptest.NewRequest("GET", "/api/moments/cover?target=fixture-cover-self", nil), User{Role: "viewer"})
		if failed {
			if w.Code != 502 || !strings.Contains(w.Body.String(), "fixture cover read unavailable") {
				t.Fatal("封面读取错误被当作空成功")
			}
		} else if w.Code != 200 || !strings.Contains(w.Body.String(), `"cover":""`) {
			t.Fatal("正常没有封面不应返回错误")
		}
	}
}
