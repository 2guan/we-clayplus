package momentsability

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"

	momentsapi "github.com/we-clay/we-clay/gateway/internal/api/moments"
	sdk "github.com/we-clay/we-clay/sdk/moments"
)

func TestUserPageCoverMetadataDirectAndPluginWithoutObjects(t *testing.T) {
	const objectID uint64 = 18446744073709551612
	const cover = "https://shmmsns.qpic.cn/fixture-sdk-cover"
	var code atomic.Int32
	clients := commentClients(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/moments/user/fixture-sdk-self" || r.URL.Query().Get("max_id") != "0" {
			t.Error("主页元信息请求不匹配")
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"base_response": map[string]int32{"code": code.Load()}, "object_count": 0, "new_request_time": 123,
			"sns_user_info": []any{map[string]any{"flag": 9, "bg_image_id": cover, "bg_object_id": strconv.FormatUint(objectID, 10), "flag_extend": 7}},
		}})
	})
	// raw core field6 与 SDK additive field12 不能混用，SDK旧field6仍是时间。
	if (&momentsapi.UserPageResponse{}).ProtoReflect().Descriptor().Fields().ByName("sns_user_info").Number() != 6 || (&sdk.UserPage_Response{}).ProtoReflect().Descriptor().Fields().ByName("sns_user_info").Number() != 12 || (&sdk.UserPage_Response{}).ProtoReflect().Descriptor().Fields().ByName("new_request_time").Number() != 6 {
		t.Fatal("主页协议字段编号被改写")
	}
	for name, client := range clients {
		for _, status := range []int32{0, 207} {
			t.Run(fmt.Sprintf("%s/code-%d", name, status), func(t *testing.T) {
				code.Store(status)
				page, err := client.UserPage("fixture-sdk-self", "", 0)
				if err != nil || page == nil || page.GetBaseResult().GetCode() != status || page.GetObjectCount() != 0 || len(page.GetObjectList()) != 0 || page.GetNewRequestTime() != 123 || len(page.GetSnsUserInfo()) != 1 {
					t.Fatalf("零动态主页元信息丢失: %v", err)
				}
				info := page.GetSnsUserInfo()[0]
				if info.GetFlag() != 9 || info.GetFlagExtend() != 7 || info.GetBgImageId() != cover || info.GetBgObjectId() != objectID {
					t.Fatal("背景元信息或uint64对象ID丢失")
				}
			})
		}
	}
}
