package chatroomability

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/we-clay/we-clay/gateway/internal/api"
	chatroomapi "github.com/we-clay/we-clay/gateway/internal/api/chatroom"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	sdk "github.com/we-clay/we-clay/sdk/chatroom"
	"github.com/we-clay/we-clay/sdk/contact"
)

type cachedRoom struct{ contact.Ability }

func (cachedRoom) Get(name string) *contact.Contact {
	return &contact.Contact{Username: name, Nickname: "synthetic", Data: &contact.Contact_Chatroom{Chatroom: &contact.Chatroom{Owner: "test-owner", MemberCount: 3}}}
}

func TestGroupAdaptersAndCache(t *testing.T) {
	var bad atomic.Bool
	var count atomic.Int32
	server := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if bad.Load() {
			io.WriteString(w, `{"code":0,"data":{"code":-1,"base_response":{"code":-1}}}`)
			return
		}
		switch r.URL.Path {
		case "/api/chatroom":
			if r.Method != "POST" {
				t.Error("create method")
			}
			io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0},"chatroom":"test-room@chatroom","member_list":[{"username":{"value":"test-a"}},{"username":{"value":"test-b"}}]}}`)
		case "/api/chatroom/name/test-room@chatroom":
			if r.Method != "PUT" || r.URL.Query().Get("name") != "测试群" {
				t.Error("name contract")
			}
			io.WriteString(w, `{"code":0,"data":{"code":0,"result":{"code":0}}}`)
		case "/api/chatroom/remark/test-room@chatroom":
			if r.Method != "PUT" || r.URL.Query().Get("remark") != "测试备注" {
				t.Error("remark contract")
			}
			io.WriteString(w, `{"code":0,"data":{"code":0,"result":{"code":0}}}`)
		case "/api/chatroom/contact-list/test-room@chatroom":
			if r.Method != "PUT" || r.URL.Query().Get("save") != "true" {
				t.Error("save contract")
			}
			io.WriteString(w, `{"code":0,"data":{"code":0,"result":{"code":0}}}`)
		case "/api/chatroom/announcement/test-room@chatroom":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if r.Method != "PUT" || body["content"] != "测试公告" {
				t.Error("announcement contract")
			}
			io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0}}}`)
		case "/api/chatroom/admins/test-room@chatroom", "/api/chatroom/invite/test-room@chatroom":
			var body struct{ Members []string }
			json.NewDecoder(r.Body).Decode(&body)
			if r.Method != "POST" || len(body.Members) != 1 || body.Members[0] != "test-c" {
				t.Error("members contract")
			}
			io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0},"member_count":1,"member_list":[{"username":{"value":"test-c"}}]}}`)
		case "/api/chatroom/info/test-room@chatroom":
			if r.Method != "GET" {
				t.Error("info method")
			}
			io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0},"announcement":"测试公告"}}`)
		case "/api/chatroom/members/detail/test-room@chatroom":
			var body []string
			json.NewDecoder(r.Body).Decode(&body)
			if r.Method != "POST" || len(body) != 1 || body[0] != "test-a" {
				t.Error("detail contract")
			}
			io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0},"contact_list":[{"username":{"value":"test-a"},"nickname":{"value":"updated"}}]}}`)
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	conn, err := api.NewGRPCClient(context.Background(), server.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	oldCore, oldContact := api.GetCore, contact.Instance
	api.GetCore = func() *api.Client { return conn }
	contact.Instance = cachedRoom{}
	defer func() { api.GetCore = oldCore; contact.Instance = oldContact }()
	a := &ability{api: chatroomapi.Get(), cache: map[string]map[string]*sdk.Member{}}
	const room = "test-room@chatroom"
	created, err := a.Create([]string{"test-a", "test-b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.ListMembers(room)) != 2 || len(created.GetMembers()) != 2 {
		t.Fatal("create lost a member")
	}
	created.Members[0].Nickname = "caller edit"
	if a.ListMembers(room)[0].Nickname == "caller edit" || a.ListMembers(room)[1].Nickname == "caller edit" {
		t.Fatal("cache aliased")
	}
	if _, err = a.SetName(room, "测试群"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.SetRemark(room, "测试备注"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Save(room, true); err != nil {
		t.Fatal(err)
	}
	if _, err = a.SetAnnouncement(room, "测试公告"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.SetAdmin(room, []string{"test-c"}); err != nil {
		t.Fatal(err)
	}
	invited, err := a.InviteMember(room, []string{"test-c"})
	if err != nil || invited.GetMembers()[0].Username != "test-c" {
		t.Fatal("invitation", err)
	}
	if len(a.ListMembers(room)) != 2 {
		t.Fatal("invitation treated as joined")
	}
	info, err := a.GetInfo(room)
	if err != nil || info.GetAnnouncement() != "测试公告" || info.GetOwner() != "test-owner" {
		t.Fatal("info", err)
	}
	members := a.GetMembersDetail(room, []string{"test-a"})
	if len(members) != 1 || members[0].Nickname != "updated" {
		t.Fatal("detail mapping")
	}
	members[0].Nickname = "caller edit"
	if a.GetMember(room, "test-a").Nickname != "updated" {
		t.Fatal("detail aliases cache")
	}
	bad.Store(true)
	if _, err = a.SetName(room, "测试群"); err == nil {
		t.Fatal("negative receipt accepted")
	}
	if _, err = a.SetAdmin(room, []string{"test-c"}); err == nil {
		t.Fatal("negative admin receipt accepted")
	}
	before := count.Load()
	if _, err = a.RemoveMember(room, []string{"test-a"}); err == nil || count.Load() != before {
		t.Fatal("placeholder sent request")
	}
}
