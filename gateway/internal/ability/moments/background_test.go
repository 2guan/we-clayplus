package momentsability

import (
	"fmt"
	"net/http"
	"testing"
)

func TestUserPageBackgroundDirectAndPlugin(t *testing.T) {
	const objectID uint64 = 18446744073709551613
	clients := commentClients(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/moments/user/fixture-self" {
			t.Error("个人页读取路由不匹配")
			w.WriteHeader(400)
			return
		}
		fmt.Fprintf(w, `{"code":0,"data":{"base_response":{"code":207},"sns_user_info":{"bg_image_id":"https://shmmsns.qpic.cn/fixture-cover","bg_object_id":"%d"}}}`, objectID)
	})
	for name, client := range clients {
		r, err := client.UserPage("fixture-self", "", 0)
		if err != nil || r.GetBgImageId() != "https://shmmsns.qpic.cn/fixture-cover" || r.GetBgObjectId() != objectID || r.GetBaseResult().GetCode() != 207 {
			t.Fatalf("%s 丢失封面字段、64位标识或分页状态：%v", name, err)
		}
	}
}
