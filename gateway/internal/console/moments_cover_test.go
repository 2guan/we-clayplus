package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/we-clay/we-clay/gateway/internal/ability/moments"
	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
)

type coverContacts struct{ contact.Ability }

func (coverContacts) GetSelf() *contact.SelfInfo {
	return &contact.SelfInfo{Username: "fixture-cover-self"}
}
func (coverContacts) Get(string) *contact.Contact { return nil }

// 通过实际 corewire gRPC 与 DoProto 解码合成 JSON，不接触微信或账号目录。
func withMomentCoverCore(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := coretest.NewServer(t, handler)
	client, err := api.NewGRPCClient(context.Background(), server.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	oldCore, oldContacts := api.GetCore, contact.Instance
	api.GetCore = func() *api.Client { return client }
	contact.Instance = coverContacts{}
	t.Cleanup(func() {
		api.GetCore, contact.Instance = oldCore, oldContacts
		_ = client.Close()
	})
}

func writeCoverReply(w http.ResponseWriter, data map[string]any) {
	data["base_response"] = map[string]int{"code": 0}
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
}

func TestMomentCoverSelfProfileFallbackUsesRawField(t *testing.T) {
	const cover = "https://shmmsns.qpic.cn/fixture-profile-cover"
	var contactCalls, profileCalls atomic.Int32
	withMomentCoverCore(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/contacts/detail":
			contactCalls.Add(1)
			var names []string
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&names) != nil || len(names) != 1 || names[0] != "fixture-cover-self" {
				t.Error("自己的联系人详情请求不匹配")
			}
			// 自己在 getcontact 中没有背景，而 profile 有值。
			writeCoverReply(w, map[string]any{"contact_list": []any{map[string]any{"username": "fixture-cover-self"}}})
		case "/api/user/profile":
			profileCalls.Add(1)
			if r.Method != "GET" {
				t.Error("资料读取不是 GET")
			}
			writeCoverReply(w, map[string]any{"user_info": map[string]any{"user_name": "fixture-cover-self", "album_bg_image_id": cover}})
		default:
			t.Error("意外的封面请求")
			w.WriteHeader(404)
		}
	})
	bridge := &Bridge{}
	bridge.Ready.Store(true)
	result, err := bridge.Execute(context.Background(), control.Command{Operation: "moments-cover"})
	if err != nil || result.(map[string]string)["cover"] != cover || contactCalls.Load() != 1 || profileCalls.Load() != 1 {
		t.Fatalf("自己的 profile 背景未到达封面接口: result=%v err=%v", result, err)
	}
}

func TestMomentCoverPreservesContactSourcesAndMediaMetadata(t *testing.T) {
	const objectID = "18446744073709551612"
	for _, tc := range []struct {
		name   string
		fields map[string]any
		want   string
		detail bool
	}{
		{"album", map[string]any{"album_bg_image_id": "https://shmmsns.qpic.cn/fixture-album", "sns_user_info": map[string]any{"bg_image_id": "https://shmmsns.qpic.cn/fixture-later"}}, "https://shmmsns.qpic.cn/fixture-album", false},
		{"sns", map[string]any{"sns_user_info": map[string]any{"bg_image_id": "https://shmmsns.qpic.cn/fixture-sns"}}, "https://shmmsns.qpic.cn/fixture-sns", false},
		{"invalid-album-valid-sns", map[string]any{"album_bg_image_id": "opaque-background-id", "sns_user_info": map[string]any{"bg_image_id": "https://shmmsns.qpic.cn/fixture-sns"}}, "https://shmmsns.qpic.cn/fixture-sns", false},
		{"object", map[string]any{"sns_user_info": map[string]any{"bg_object_id": objectID}}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var detailCalls atomic.Int32
			withMomentCoverCore(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/contacts/detail":
					fields := map[string]any{"username": "fixture-cover-peer"}
					for k, value := range tc.fields {
						fields[k] = value
					}
					writeCoverReply(w, map[string]any{"contact_list": []any{fields}})
				case "/api/moments/" + objectID:
					detailCalls.Add(1)
					writeCoverReply(w, map[string]any{"object": map[string]any{"id": objectID, "object_description": `<TimelineObject><ContentObject><mediaList><media><type>2</type><url>https://shmmsns.qpic.cn/fixture-original</url><thumb key="18446744073709551613" token="fixture-cover-token" enc_idx="2">https://shmmsns.qpic.cn/fixture-thumb</thumb></media></mediaList></ContentObject></TimelineObject>`}})
				default:
					t.Error("联系人封面意外读取自己的 profile 或其他接口")
					w.WriteHeader(404)
				}
			})
			got, err := momentCover("fixture-cover-peer")
			if err != nil {
				t.Fatal(err)
			}
			if !tc.detail {
				if got != tc.want || detailCalls.Load() != 0 {
					t.Fatal("联系人背景来源优先级发生变化")
				}
				return
			}
			proxy, err := url.Parse(got)
			if err != nil || proxy.Path != "/api/wechat-image" || proxy.Query().Get("key") != "18446744073709551613" || detailCalls.Load() != 1 {
				t.Fatal("背景对象或大整数图片解密参数丢失")
			}
			remote, err := url.Parse(proxy.Query().Get("url"))
			if err != nil || remote.Query().Get("token") != "fixture-cover-token" || remote.Query().Get("idx") != "2" {
				t.Fatal("背景图片访问元数据丢失")
			}
		})
	}
}

func TestMomentCoverErrorsAreNotEmptySuccess(t *testing.T) {
	for _, tc := range []struct {
		name, target, failure, raw string
		want                       string
	}{
		{"contact-api", "fixture-cover-peer", "contact", "", "联系人资料失败"},
		{"profile-api", "fixture-cover-self", "profile", "", "自己的朋友圈封面资料失败"},
		{"unsafe-contact", "fixture-cover-peer", "", "javascript:alert(1)", "有效封面地址"},
		{"opaque-contact", "fixture-cover-peer", "", "not-a-cdn-address", "有效封面地址"},
		{"unsafe-profile", "fixture-cover-self", "unsafe-profile", "", "有效封面地址"},
		{"mismatched-profile", "fixture-cover-self", "mismatched-profile", "", "账号不匹配"},
		{"detail-api", "fixture-cover-peer", "detail", "", "背景动态失败"},
		{"detail-xml", "fixture-cover-peer", "detail-xml", "", "内容无法解析"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withMomentCoverCore(t, func(w http.ResponseWriter, r *http.Request) {
				fail := (tc.failure == "contact" && r.URL.Path == "/api/contacts/detail") || (tc.failure == "profile" && r.URL.Path == "/api/user/profile") || (tc.failure == "detail" && r.URL.Path == "/api/moments/99")
				if fail {
					w.WriteHeader(502)
					fmt.Fprint(w, `{"code":-1,"message":"fixture read unavailable"}`)
					return
				}
				switch r.URL.Path {
				case "/api/contacts/detail":
					item := map[string]any{"username": tc.target, "album_bg_image_id": tc.raw}
					if strings.HasPrefix(tc.failure, "detail") {
						item["sns_user_info"] = map[string]any{"bg_object_id": "99"}
					}
					writeCoverReply(w, map[string]any{"contact_list": []any{item}})
				case "/api/user/profile":
					info := map[string]any{"user_name": "fixture-cover-self"}
					if tc.failure == "unsafe-profile" {
						info["album_bg_image_id"] = "file:///fixture-cover.jpg"
					}
					if tc.failure == "mismatched-profile" {
						info["user_name"] = "fixture-other-account"
						info["album_bg_image_id"] = "https://shmmsns.qpic.cn/fixture-other-cover"
					}
					writeCoverReply(w, map[string]any{"user_info": info})
				case "/api/moments/99":
					writeCoverReply(w, map[string]any{"object": map[string]any{"id": "99", "object_description": "<broken"}})
				default:
					if strings.HasPrefix(r.URL.Path, "/api/moments/user/") {
						writeCoverReply(w, map[string]any{"object_count": 0})
						return
					}
					t.Error("意外的封面读取")
					w.WriteHeader(404)
				}
			})
			bridge := &Bridge{}
			bridge.Ready.Store(true)
			result, err := bridge.Execute(context.Background(), control.Command{Operation: "moments-cover", Target: tc.target})
			if err == nil || !strings.Contains(err.Error(), tc.want) || result != nil {
				t.Fatalf("读取失败变成空成功: result=%v err=%v", result, err)
			}
		})
	}
}

func TestMomentCoverMissingFieldsRemainNormalEmpty(t *testing.T) {
	withMomentCoverCore(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/contacts/detail":
			var names []string
			if json.NewDecoder(r.Body).Decode(&names) != nil || len(names) != 1 {
				t.Fatal("联系人请求不完整")
			}
			writeCoverReply(w, map[string]any{"contact_list": []any{map[string]any{"username": names[0]}}})
		case "/api/user/profile":
			writeCoverReply(w, map[string]any{"user_info": map[string]any{"user_name": "fixture-cover-self"}})
		default:
			if strings.HasPrefix(r.URL.Path, "/api/moments/user/") {
				writeCoverReply(w, map[string]any{"object_count": 0})
				return
			}
			t.Error("无背景时不应猜测 CDN 或背景对象")
			w.WriteHeader(404)
		}
	})
	for _, target := range []string{"", "fixture-cover-peer", "fixture-cover-self"} {
		if cover, err := momentCover(target); err != nil || cover != "" {
			t.Fatalf("正常缺省被当作失败: cover=%q err=%v", cover, err)
		}
	}
}

func TestMomentCoverUserPageHeaderWithoutAnyObjects(t *testing.T) {
	for _, code := range []int{0, 207} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			var pageCalls atomic.Int32
			const cover = "https://shmmsns.qpic.cn/fixture-userpage-cover"
			withMomentCoverCore(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/contacts/detail":
					writeCoverReply(w, map[string]any{"contact_list": []any{map[string]any{"username": "fixture-cover-self"}}})
				case "/api/user/profile":
					writeCoverReply(w, map[string]any{"user_info": map[string]any{"user_name": "fixture-cover-self"}})
				case "/api/moments/user/fixture-cover-self":
					pageCalls.Add(1)
					if r.Method != "GET" || r.URL.Query().Get("first_page_md5") != "" || r.URL.Query().Get("max_id") != "0" {
						t.Error("主页封面请求不匹配")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"base_response": map[string]int{"code": code}, "object_count": 0, "sns_user_info": []any{map[string]any{"bg_image_id": cover, "bg_object_id": "18446744073709551612"}}}})
				default:
					t.Error("主页已有URL时不应请求背景动态或猜测CDN")
					w.WriteHeader(404)
				}
			})
			got, err := momentCover("fixture-cover-self")
			if err != nil || got != cover || pageCalls.Load() != 1 {
				t.Fatalf("零动态主页未读取元信息封面: cover=%q err=%v", got, err)
			}
		})
	}
}

func coverDashboard(t *testing.T) (*dashboard.Server, func() string) {
	t.Helper()
	server, err := dashboard.Start(context.Background(), control.Config{Disabled: true, DataDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	w := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/auth/setup", strings.NewReader(`{"name":"fixture-cover-admin","password":"abc123"}`))
	request.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(w, request)
	if w.Code != 200 || len(w.Result().Cookies()) == 0 {
		t.Fatal("测试控制台初始化失败")
	}
	cookie := w.Result().Cookies()[0]
	return server, func() string {
		r := httptest.NewRequest("GET", "/api/state", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		var state struct{ Account dashboard.Account }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil {
			t.Fatal("读取合成控制台状态失败")
		}
		return state.Account.MomentCover
	}
}

func TestMomentCoverSelfCacheRejectsLateAccountResponses(t *testing.T) {
	for _, mode := range []string{"api", "sync"} {
		for _, transition := range []string{"reconnect", "a-to-b", "a-to-b-to-a"} {
			t.Run(mode+"/"+transition, func(t *testing.T) {
				server, cachedCover := coverDashboard(t)
				if err := server.SetAccount(dashboard.Account{ID: "fixture-cover-self", MomentCover: "https://shmmsns.qpic.cn/fixture-before"}); err != nil {
					t.Fatal(err)
				}
				entered, release := make(chan struct{}), make(chan struct{})
				defer func() {
					select {
					case <-release:
					default:
						close(release)
					}
				}()
				const responseCover = "https://shmmsns.qpic.cn/fixture-slow-profile"
				withMomentCoverCore(t, func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/contacts/detail":
						writeCoverReply(w, map[string]any{"contact_list": []any{map[string]any{"username": "fixture-cover-self"}}})
					case "/api/user/profile":
						close(entered)
						<-release
						writeCoverReply(w, map[string]any{"user_info": map[string]any{"user_name": "fixture-cover-self", "album_bg_image_id": responseCover}})
					default:
						t.Error("迟到响应测试发生意外读取")
						w.WriteHeader(404)
					}
				})
				bridge := &Bridge{Dashboard: server}
				bridge.Ready.Store(true)
				done := make(chan error, 1)
				go func() {
					if mode == "sync" {
						bridge.syncMomentCover()
						done <- nil
						return
					}
					_, err := bridge.Execute(context.Background(), control.Command{Operation: "moments-cover"})
					done <- err
				}()
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("合成资料请求未进入阻塞点")
				}
				want := responseCover
				if transition == "reconnect" {
					if err := server.SetAccount(dashboard.Account{ID: "fixture-cover-self", Status: "online"}); err != nil {
						t.Fatal(err)
					}
				} else {
					want = "https://shmmsns.qpic.cn/fixture-current-b"
					if err := server.SetAccount(dashboard.Account{ID: "fixture-cover-b", MomentCover: want}); err != nil {
						t.Fatal(err)
					}
					if transition == "a-to-b-to-a" {
						want = "https://shmmsns.qpic.cn/fixture-current-a"
						if err := server.SetAccount(dashboard.Account{ID: "fixture-cover-self", MomentCover: want}); err != nil {
							t.Fatal(err)
						}
					}
				}
				close(release)
				select {
				case err := <-done:
					if mode == "api" && ((transition == "reconnect" && err != nil) || (transition != "reconnect" && (err == nil || !strings.Contains(err.Error(), "已过期")))) {
						t.Fatalf("账号代次校验错误: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("合成封面请求未结束")
				}
				if got := cachedCover(); got != want {
					t.Fatalf("封面缓存被旧账号响应覆盖: got=%q want=%q", got, want)
				}
			})
		}
	}
}

func TestMomentCoverPeerResponseDoesNotReplaceSelfCache(t *testing.T) {
	server, cachedCover := coverDashboard(t)
	const ownCover = "https://shmmsns.qpic.cn/fixture-own-cover"
	if err := server.SetAccount(dashboard.Account{ID: "fixture-cover-self", MomentCover: ownCover}); err != nil {
		t.Fatal(err)
	}
	withMomentCoverCore(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/contacts/detail" {
			t.Error("已知联系人封面不应额外查询")
			w.WriteHeader(404)
			return
		}
		writeCoverReply(w, map[string]any{"contact_list": []any{map[string]any{"username": "fixture-cover-peer", "album_bg_image_id": "https://shmmsns.qpic.cn/fixture-peer-cover"}}})
	})
	bridge := &Bridge{Dashboard: server}
	bridge.Ready.Store(true)
	if _, err := bridge.Execute(context.Background(), control.Command{Operation: "moments-cover", Target: "fixture-cover-peer"}); err != nil || cachedCover() != ownCover {
		t.Fatalf("他人的封面污染了当前账号缓存: %v", err)
	}
}
