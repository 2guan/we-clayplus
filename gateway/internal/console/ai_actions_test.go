package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	_ "github.com/we-clay/we-clay/gateway/internal/ability"
	chatroomability "github.com/we-clay/we-clay/gateway/internal/ability/chatroom"
	contactability "github.com/we-clay/we-clay/gateway/internal/ability/contact"
	messageability "github.com/we-clay/we-clay/gateway/internal/ability/message"
	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/control"
)

// Stateful fixture 校验建群→改名→读回、邀请未等于入群、私聊→保留原回执→撤回及真实拒绝传播。
// 不调用线上微信。参数对照已有 core 契约和本仓库历史 Mac 验收记录。
func TestAIAdaptersStatefulGroupAndSendReceiptContracts(t *testing.T) {
	const group = "fixture-action-room@chatroom"
	var mu sync.Mutex
	created := false
	name := "初始群名"
	sends, revokes, invites := 0, 0, 0
	reject := false
	sentID := "9007199254740997"
	clientID := "9007199254740999"
	createdAt := strconv.FormatInt(time.Now().Unix()-1, 10)
	core := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/chatroom":
			var q struct{ Members []string }
			_ = json.NewDecoder(r.Body).Decode(&q)
			if len(q.Members) != 2 || q.Members[0] != "fixture-a" || q.Members[1] != "fixture-b" {
				t.Error("建群成员契约不符")
			}
			created = true
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"base_response": map[string]int{"code": 0}, "chatroom": group, "topic": name}})
		case "/api/chatroom/name/" + group:
			if !created {
				t.Error("没有建群就改名")
			}
			name = r.URL.Query().Get("name")
			fmt.Fprint(w, `{"code":0,"data":{"code":0,"result":{"count":1}}}`)
		case "/api/contacts/detail":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"base_response": map[string]int{"code": 0}, "contact_list": []any{map[string]any{"username": map[string]string{"value": group}, "nickname": map[string]string{"value": name}}}}})
		case "/api/chatroom/invite/" + group:
			if reject {
				w.WriteHeader(400)
				fmt.Fprint(w, `{"code":-1,"message":"fixture rejected invite"}`)
				return
			}
			invites++
			var q struct{ Members []string }
			_ = json.NewDecoder(r.Body).Decode(&q)
			if len(q.Members) != 1 || q.Members[0] != "fixture-c" {
				t.Error("邀请成员丢失")
			}
			fmt.Fprint(w, `{"code":0,"data":{"base_response":{"code":0},"member_count":1}}`)
		case "/api/message/text":
			var q struct{ Receiver, Content string }
			_ = json.NewDecoder(r.Body).Decode(&q)
			if q.Receiver != "fixture-a" || q.Content != "指定私聊正文" {
				t.Error("私聊正文或目标丢失")
			}
			sends++
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"base_response": map[string]int{"code": 0}, "list": []any{map[string]any{"code": 0, "new_id": sentID, "client_id": clientID, "create_time": createdAt}}}})
		case "/api/message/revoke":
			var q map[string]json.RawMessage
			_ = json.NewDecoder(r.Body).Decode(&q)
			field := func(key string) string {
				var out string
				if json.Unmarshal(q[key], &out) == nil {
					return out
				}
				return string(q[key])
			}
			if field("receiver") != "fixture-a" || field("new_id") != sentID || field("client_id") != clientID || field("create_time") != createdAt {
				t.Error("撤回没有使用原始发送回执", field("new_id"), field("client_id"), field("create_time"))
			}
			revokes++
			fmt.Fprint(w, `{"code":0,"data":{"base_response":{"code":0}}}`)
		default:
			t.Error("意外请求", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	client, e := api.NewGRPCClient(context.Background(), core.Address, "")
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	oldCore := api.GetCore
	api.GetCore = func() *api.Client { return client }
	defer func() { api.GetCore = oldCore }()
	contactability.LoadCache(t.TempDir())
	contactability.SetSelf(&contact.SelfInfo{Username: "fixture-self"})
	defer contactability.SetSelf(nil)
	chatroomability.Initial(t.TempDir())
	wasReady := messageability.OutboundReady()
	messageability.SetOutboundReady(true)
	defer func() { messageability.SetOutboundReady(wasReady); messageability.ResetReceipts() }()
	b := &Bridge{}
	b.Ready.Store(true)
	invoke := func(op, target, content string, p control.ActionParams) (any, error) {
		raw, _ := json.Marshal(p)
		return b.Execute(context.Background(), control.Command{Operation: "ai-action", ID: op, Target: target, Content: content, Params: raw})
	}
	r, e := invoke("chatroom.Create", "fixture-origin@chatroom", "", control.ActionParams{Members: []string{"fixture-a", "fixture-b"}})
	if e != nil {
		t.Fatal(e)
	}
	if r.(map[string]any)["id"] != group {
		t.Fatal("真实群ID丢失")
	}
	if _, e = b.Execute(context.Background(), control.Command{Operation: "group-name", Target: group, Content: "读回确认的新群名"}); e != nil {
		t.Fatal(e)
	}
	if cached := contact.Instance.Get(group); cached == nil || cached.GetNickname() != "读回确认的新群名" {
		t.Fatal("群名未按读回同步")
	}
	r, e = invoke("chatroom.InviteMember", group, "", control.ActionParams{Members: []string{"fixture-c"}})
	if e != nil {
		t.Fatal(e)
	}
	if r.(map[string]any)["status"] != "invitation_sent" {
		t.Fatal("把邀请成功冒充为已入群")
	}
	r, e = invoke("message.Send", "fixture-a", "指定私聊正文", control.ActionParams{Kind: "text"})
	if e != nil {
		t.Fatal(e)
	}
	receipt := r.(map[string]string)
	if receipt["id"] != sentID || receipt["client_id"] != clientID || receipt["create_time"] != createdAt || receipt["account"] != "fixture-self" {
		t.Fatal("原始大整数回执未完整传递", receipt)
	}
	persisted, _ := json.Marshal(control.RevokeReceipt{Account: receipt["account"], ClientID: receipt["client_id"], Created: receipt["create_time"]})
	messageability.ResetReceipts()
	if _, e = b.Execute(context.Background(), control.Command{Operation: "revoke", Target: "fixture-a", ID: receipt["id"], Params: persisted}); e != nil {
		t.Fatal(e)
	}
	mu.Lock()
	reject = true
	mu.Unlock()
	if _, e = invoke("chatroom.InviteMember", group, "", control.ActionParams{Members: []string{"fixture-c"}}); e == nil {
		t.Fatal("微信拒绝被吞掉")
	}
	mu.Lock()
	defer mu.Unlock()
	if sends != 1 || revokes != 1 || invites != 1 {
		t.Fatal("出现额外写入", sends, revokes, invites)
	}
}

func TestAIVideoRejectsUntrustedOrMissingMediaBeforeTools(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("https://example.test/video.mp4"), []byte("#EXTM3U\nfile:///fixture")} {
		if _, _, e := prepareAIVideo(context.Background(), data); e == nil {
			t.Fatal("接受了非MP4字节")
		}
	}
}

func TestPrepareAIVideoFromSyntheticLocalMP4(t *testing.T) {
	ffmpeg, e := exec.LookPath("ffmpeg")
	if e != nil {
		t.Skip("未安装可选ffmpeg")
	}
	if _, e = exec.LookPath("ffprobe"); e != nil {
		t.Skip("未安装可选ffprobe")
	}
	path := filepath.Join(t.TempDir(), "fixture.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, e := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=32x32:d=0.25", "-c:v", "mpeg4", "-y", path).CombinedOutput(); e != nil {
		t.Fatal("生成离线视频失败", e, string(out))
	}
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	thumb, duration, e := prepareAIVideo(ctx, data)
	if e != nil || duration != 1 || len(thumb) < 3 || thumb[0] != 0xff || thumb[1] != 0xd8 {
		t.Fatal("视频封面或时长无效", duration, len(thumb), e)
	}
}

func TestAILabelChangesPreserveUnrelatedLabels(t *testing.T) {
	var mu sync.Mutex
	labels := "1,2"
	known := true
	writes := 0
	core := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/contacts/detail":
			person := map[string]any{"username": map[string]string{"value": "fixture-person"}}
			if known {
				person["label_id_list"] = labels
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"base_response": map[string]int{"code": 0}, "contact_list": []any{person}}})
		case "/api/contacts/fixture-person/labels":
			var q struct {
				Labels string `json:"label_ids"`
			}
			_ = json.NewDecoder(r.Body).Decode(&q)
			labels = q.Labels
			writes++
			fmt.Fprint(w, `{"code":0,"data":{"base_response":{"code":0}}}`)
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	client, e := api.NewGRPCClient(context.Background(), core.Address, "")
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	old := api.GetCore
	api.GetCore = func() *api.Client { return client }
	defer func() { api.GetCore = old }()
	b := &Bridge{}
	assertState := func(expected string, count int) {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if labels != expected || writes != count {
			t.Fatal(labels, writes, expected, count)
		}
	}
	if _, e = b.modifyAILabels(control.ActionParams{Members: []string{"fixture-person"}, Labels: []uint32{3}}); e != nil {
		t.Fatal(e)
	}
	assertState("1,2,3", 1)
	if _, e = b.modifyAILabels(control.ActionParams{Members: []string{"fixture-person"}, Labels: []uint32{2}, LabelMode: "remove"}); e != nil {
		t.Fatal(e)
	}
	assertState("1,3", 2)
	mu.Lock()
	known = false
	mu.Unlock()
	if _, e = b.modifyAILabels(control.ActionParams{Members: []string{"fixture-person"}, Labels: []uint32{4}}); e == nil {
		t.Fatal("未知标签仍被覆盖")
	}
	assertState("1,3", 2)
	if _, e = b.modifyAILabels(control.ActionParams{Members: []string{"fixture-person"}, LabelMode: "replace"}); e != nil {
		t.Fatal(e)
	}
	assertState("", 3)
}
