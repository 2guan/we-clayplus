// 文件名：refresh_test.go
// 主要功能：验证单个失效联系人不阻断整批同步、失败刷新不清空已有缓存。
// 作者：2guan
package contactability

import (
	"encoding/json"
	"errors"
	api "github.com/we-clay/we-clay/gateway/internal/api/contact"
	sdk "github.com/we-clay/we-clay/sdk/contact"
	"google.golang.org/protobuf/types/known/wrapperspb"
	"os"
	"path/filepath"
	"testing"
)

type detailFixture struct {
	api.ContactService
	failList bool
}

func (f detailFixture) List() ([]string, error) {
	if f.failList {
		return nil, errors.New("network failure")
	}
	return []string{"fixture-good", "fixture-invalid", "fixture-group@chatroom"}, nil
}
func (f detailFixture) Detail(ids []string) (*api.GetContactDetailResponse, error) {
	for _, id := range ids {
		if id == "fixture-invalid" {
			return nil, errors.New("contact detail item returned code -1")
		}
	}
	r := &api.GetContactDetailResponse{}
	for _, id := range ids {
		r.ContactList = append(r.ContactList, &api.ModifyContact{Username: wrapperspb.String(id), Nickname: wrapperspb.String("测试"), SmallAvatarUrl: ptr("https://wx.qlogo.cn/fixture")})
	}
	return r, nil
}
func ptr(v string) *string { return &v }
func TestRefreshPreservesCacheAndSplitsInvalidContacts(t *testing.T) {
	oldAPI, oldCache, oldDir := instance.api, instance.cache, cacheDir
	defer func() { instance.api = oldAPI; instance.cache = oldCache; cacheDir = oldDir }()
	instance.cache = map[string]*sdk.Contact{"fixture-existing": {Username: "fixture-existing"}}
	cacheDir = t.TempDir()
	instance.api = detailFixture{failList: true}
	if Refresh() == nil || len(instance.cache) != 1 {
		t.Fatal("失败刷新清空缓存")
	}
	instance.api = detailFixture{}
	if err := Refresh(); err != nil {
		t.Fatal(err)
	}
	if len(instance.cache) != 3 || instance.cache["fixture-group@chatroom"].Type != sdk.ContactType_CONTACT_TYPE_CHATROOM || instance.cache["fixture-good"].Avatar == "" {
		t.Fatal("有效联系人、群类型或小头像丢失")
	}
}

// 模拟实际 SDK oneof 缓存，确保重启不会丢掉头像与联系人关系。
func TestContactCacheRoundTrip(t *testing.T) {
	oldCache, oldDir := instance.cache, cacheDir
	defer func() { instance.cache = oldCache; cacheDir = oldDir }()
	cacheDir = t.TempDir()
	instance.cache = map[string]*sdk.Contact{"fixture-peer": {Username: "fixture-peer", Avatar: "https://wx.qlogo.cn/fixture", Type: sdk.ContactType_CONTACT_TYPE_FRIEND, Data: &sdk.Contact_Friend{Friend: &sdk.Friend{City: "测试城市"}}}}
	// 先验证旧版 encoding/json 数据的兼容恢复。
	legacy, _ := json.Marshal(instance.cache)
	if err := os.WriteFile(filepath.Join(cacheDir, "contact.json"), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	LoadCache(cacheDir)
	if len(instance.cache) != 1 || instance.cache["fixture-peer"].GetFriend().GetCity() != "测试城市" {
		t.Fatal("旧缓存迁移丢失")
	}
	Destroy()
	LoadCache(cacheDir)
	if len(instance.cache) != 1 || instance.cache["fixture-peer"].Avatar == "" || instance.cache["fixture-peer"].GetFriend().GetCity() != "测试城市" {
		t.Fatal("重启丢失联系人资料")
	}
}

// 重连只补齐缺失项，显式全量刷新仍可读取已有联系人。
type countingDetailFixture struct {
	detailFixture
	calls int
}

func (f *countingDetailFixture) Detail(ids []string) (*api.GetContactDetailResponse, error) {
	f.calls++
	return f.detailFixture.Detail(ids)
}
func TestReconnectSkipsKnownContacts(t *testing.T) {
	oldAPI, oldCache, oldDir := instance.api, instance.cache, cacheDir
	defer func() { instance.api = oldAPI; instance.cache = oldCache; cacheDir = oldDir }()
	cacheDir = t.TempDir()
	instance.cache = map[string]*sdk.Contact{}
	for _, id := range []string{"fixture-good", "fixture-invalid", "fixture-group@chatroom"} {
		instance.cache[id] = &sdk.Contact{Username: id, Nickname: "已同步", Avatar: "https://wx.qlogo.cn/fixture"}
	}
	f := &countingDetailFixture{}
	instance.api = f
	if err := RefreshMissingWithProgress(nil); err != nil {
		t.Fatal(err)
	}
	if f.calls != 0 {
		t.Fatalf("重连重复扫描已有联系人：%d", f.calls)
	}
	instance.cache["fixture-group@chatroom"].Avatar = ""
	if err := RefreshMissingWithProgress(nil); err != nil || f.calls != 1 || instance.cache["fixture-group@chatroom"].Avatar == "" {
		t.Fatal("有群名但缺头像的群未补齐", err)
	}
	instance.cache["fixture-good"].Avatar = ""
	if err := RefreshMissingWithProgress(nil); err != nil || f.calls != 2 || instance.cache["fixture-good"].Avatar == "" {
		t.Fatal("有昵称但缺头像的好友未补齐", err)
	}
	f.calls = 0
	if err := RefreshWithProgress(nil); err != nil {
		t.Fatal(err)
	}
	if f.calls == 0 {
		t.Fatal("显式刷新未读取资料")
	}
}

func TestContactListOrderIsStable(t *testing.T) {
	a := &ability{cache: map[string]*sdk.Contact{"fixture-b": {Username: "fixture-b"}, "fixture-a": {Username: "fixture-a"}, "fixture-c": {Username: "fixture-c"}}}
	for i := 0; i < 20; i++ {
		items := a.List()
		if len(items) != 3 || items[0].Username != "fixture-a" || items[1].Username != "fixture-b" || items[2].Username != "fixture-c" {
			t.Fatal("缓存遍历改变联系人顺序")
		}
	}
}
