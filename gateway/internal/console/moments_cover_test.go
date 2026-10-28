package console

import (
	"context"
	"net/http"
	"testing"

	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/moments"
)

type coverFixture struct {
	moments.Ability
	image  string
	object uint64
	pages  []string
}

func (f *coverFixture) UserPage(username, _ string, _ uint64) (*moments.UserPage_Response, error) {
	f.pages = append(f.pages, username)
	return &moments.UserPage_Response{BaseResult: &moments.BaseResult{Code: 207}, BgImageId: f.image, BgObjectId: f.object}, nil
}

func (f *coverFixture) Detail(id uint64) (*moments.Detail_Response, error) {
	return &moments.Detail_Response{BaseResult: &moments.BaseResult{}, Object: &moments.Moment{Id: id, ObjectDescription: `<TimelineObject><ContentObject><mediaList><media><thumb>https://shmmsns.qpic.cn/fixture-background</thumb></media></mediaList></ContentObject></TimelineObject>`}}, nil
}

func TestOwnMomentCoverFromUserPage(t *testing.T) {
	// 与本次现场一致：个人联系人详情不含 SNS 资料，个人页以 207 返回封面。
	core := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/contacts/detail" {
			t.Error("不是联系人只读查询")
			w.WriteHeader(400)
			return
		}
		w.Write([]byte(`{"code":0,"data":{"base_response":{"code":0}}}`))
	}))
	defer core.Close()
	client, err := api.NewGRPCClient(context.Background(), core.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	oldCore, oldContacts, oldMoments := api.GetCore, contact.Instance, moments.Instance
	defer func() { api.GetCore, contact.Instance, moments.Instance = oldCore, oldContacts, oldMoments }()
	api.GetCore = func() *api.Client { return client }
	contact.Instance = postContacts{}
	f := &coverFixture{image: "https://shmmsns.qpic.cn/fixture-cover"}
	moments.Instance = f
	if got := momentCover("fixture-self"); got != f.image {
		t.Fatal("本人封面未从个人页补齐")
	}
	if got := momentCover("fixture-other"); got != "" || len(f.pages) != 1 {
		t.Fatal("把本人回退路径用于其他联系人")
	}
	page, err := (&Bridge{}).momentPage("fixture-self", "")
	if err != nil || page.Cover != f.image {
		t.Fatal("个人页未返回封面", err)
	}
	f.image = "javascript:alert(1)"
	if got := momentCover("fixture-self"); got != "" {
		t.Fatal("接受不安全的封面地址")
	}
	f.image, f.object = "opaque-background-id", 123
	if got := momentCover("fixture-self"); got != "https://shmmsns.qpic.cn/fixture-background" {
		t.Fatal("未使用明确指定的背景动态")
	}
	f.image, f.object = "", 0
	if got := momentCover("fixture-self"); got != "" {
		t.Fatal("没有封面字段时冒用普通动态图片")
	}
}
