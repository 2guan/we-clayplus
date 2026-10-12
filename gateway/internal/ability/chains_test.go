package ability_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"github.com/we-clay/we-clay/sdk/cdn"
	"github.com/we-clay/we-clay/sdk/chatroom"
	"github.com/we-clay/we-clay/sdk/contact"
	"github.com/we-clay/we-clay/sdk/favor"
	"github.com/we-clay/we-clay/sdk/moments"
	"github.com/we-clay/we-clay/sdk/report"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

func withCore(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	s := coretest.NewServer(t, handler)
	t.Cleanup(s.Close)
	c, err := api.NewGRPCClient(context.Background(), s.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	old := api.GetCore
	api.GetCore = func() *api.Client { return c }
	t.Cleanup(func() { api.GetCore = old; c.Close() })
}

func TestSearchTicketsSurvivePluginAndFriendRequest(t *testing.T) {
	var empty atomic.Bool
	var requests atomic.Int32
	withCore(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Error("expected POST")
		}
		switch r.URL.Path {
		case "/api/contacts/search":
			var q map[string]json.RawMessage
			json.NewDecoder(r.Body).Decode(&q)
			if string(q["keyword"]) != `"test-query"` || string(q["from_scene"]) != "3" || string(q["search_scene"]) != "1" || len(q) != 3 {
				t.Error("search parameters changed")
			}
			if empty.Load() {
				io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0}}}`)
				return
			}
			io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0},"username":"test-v1","antispam_ticket":"test-v2"}}`)
		case "/api/contacts/friend-requests":
			requests.Add(1)
			var q struct {
				V1, V2, Content string
				Operate, Scene  int
			}
			d := json.NewDecoder(r.Body)
			d.DisallowUnknownFields()
			if err := d.Decode(&q); err != nil || q.V1 != "test-v1" || q.V2 != "test-v2" || q.Content != "hello" || q.Operate != 2 || q.Scene != 3 {
				w.WriteHeader(400)
				io.WriteString(w, `{"code":-1,"message":"missing original tickets"}`)
				return
			}
			io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0}}}`)
		default:
			t.Error("unexpected path")
			w.WriteHeader(404)
		}
	})
	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	contact.RegisterContactServiceServer(server, &contact.Server{Impl: contact.Instance})
	go server.Serve(lis)
	t.Cleanup(func() { server.Stop(); lis.Close() })
	conn, err := grpc.NewClient("passthrough:///contact-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, client := range []contact.Ability{contact.Instance, contact.Client{Client: contact.NewContactServiceClient(conn)}} {
		found := client.Search("test-query", 3, 1)
		if found == nil || found.GetV1() != "test-v1" || found.GetV2() != "test-v2" {
			t.Fatal("search lost friend-request tickets")
		}
		if err := client.AddFriend(found.GetV1(), found.GetV2(), "hello", 2, 3); err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 2 {
		t.Fatal("friend request was repeated")
	}
	empty.Store(true)
	if contact.Instance.Search("test-query", 3, 1) != nil {
		t.Fatal("empty search became a contact")
	}
}

func TestMomentsPagingAndSyncParameters(t *testing.T) {
	const id = uint64(9223372036854770001)
	key := []byte{251, 255, 0, 1} // Base64 包含 + / =，不能在 Query 中被改变。
	withCore(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/moments/user/test-peer", "/api/moments/timeline":
			if r.Method != "GET" || r.URL.Query().Get("max_id") != fmt.Sprint(id) || r.URL.Query().Get("first_page_md5") != "hash+/=" {
				t.Error("pagination parameters corrupted")
			}
			code := 0
			if strings.Contains(r.URL.Path, "/user/") {
				code = 207
			}
			fmt.Fprintf(w, `{"code":0,"data":{"base_response":{"code":%d},"continue_id":"%d","object_list":[{"id":"%d"}]}}`, code, id, id)
		case "/api/moments/sync":
			got, err := base64.StdEncoding.DecodeString(r.URL.Query().Get("key"))
			if r.Method != "POST" || err != nil || !bytes.Equal(got, key) {
				t.Error("sync key corrupted")
			}
			io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0},"key":{"size":4,"data":"+/8AAQ=="},"continue_flag":1}}`)
		default:
			t.Error("unexpected path")
			w.WriteHeader(404)
		}
	})
	page, err := moments.Instance.UserPage("test-peer", "hash+/=", id)
	if err != nil || page.GetBaseResult().GetCode() != 207 || page.GetContinueId() != id || page.GetObjectList()[0].GetId() != id {
		t.Fatal("normal end-of-page or ID lost", err)
	}
	line, err := moments.Instance.Timeline("hash+/=", id)
	if err != nil || line.GetObjectList()[0].GetId() != id {
		t.Fatal("timeline ID lost", err)
	}
	sync, err := moments.Instance.Sync(key)
	if err != nil || !bytes.Equal(sync.GetKey().GetData(), key) || sync.GetContinueFlag() != 1 {
		t.Fatal("sync cursor lost", err)
	}
}

func TestFavorSyncPreservesTypeWithoutInventingID(t *testing.T) {
	var unknown atomic.Bool
	withCore(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/favor/sync" || r.URL.Query().Get("key") != "+/8AAQ==" {
			t.Error("favorite sync contract")
		}
		if unknown.Load() {
			io.WriteString(w, `{"code":0,"data":{"items":[],"key":"AQ==","hasMore":true,"unknown":[{"id":201}]}}`)
			return
		}
		io.WriteString(w, `{"code":0,"data":{"items":[{"type":7,"client_id":"test-client","source_type":2,"source_id":"test-source","object":"<fav/>"}],"key":"Ag==","hasMore":true}}`)
	})
	key := []byte{251, 255, 0, 1}
	r, err := favor.Instance.Sync(key)
	if err != nil || len(r.GetItems()) != 1 {
		t.Fatal(err)
	}
	item := r.GetItems()[0]
	if item.GetId() != 0 || item.GetType() != 7 || item.GetClientId() != "test-client" || item.GetSourceType() != 2 || item.GetSourceId() != "test-source" || item.GetObject() != "<fav/>" {
		t.Fatal("favorite metadata corrupted")
	}
	encoded, _ := proto.Marshal(r)
	var pluginResult favor.Sync_Response
	if err := proto.Unmarshal(encoded, &pluginResult); err != nil || !proto.Equal(r, &pluginResult) {
		t.Fatal("plugin wire fields lost")
	}
	unknown.Store(true)
	if r, err := favor.Instance.Sync(key); err == nil || r != nil {
		t.Fatal("advanced cursor past unsupported data")
	}
}

func TestCDNUploadThenDownloadUsesOriginalIdentifiers(t *testing.T) {
	payload := []byte("synthetic-media")
	var malformed atomic.Bool
	withCore(t, func(w http.ResponseWriter, r *http.Request) {
		if malformed.Load() {
			io.WriteString(w, `{"code":0,"data":{}}`)
			return
		}
		if r.Method == "POST" {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			part := "image"
			if strings.HasSuffix(r.URL.Path, "/video") {
				part = "video"
			}
			f, _, err := r.FormFile(part)
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()
			data, _ := io.ReadAll(f)
			if !bytes.Equal(data, payload) {
				t.Error("media payload changed")
			}
			if strings.Contains(r.URL.Path, "/moments/") {
				if len(r.MultipartForm.Value) != 0 {
					t.Error("SNS upload fields changed")
				}
				io.WriteString(w, `{"code":0,"data":{"file_url":"https://example.invalid/image","thumb_url":"https://example.invalid/thumb","file_md5":"synthetic-md5","image_width":12,"image_height":34}}`)
				return
			}
			if r.FormValue("receiver") != "test-peer" {
				t.Error("receiver lost")
			}
			if part == "video" {
				f, _, e := r.FormFile("thumb")
				if e != nil {
					t.Error(e)
					return
				}
				defer f.Close()
				b, _ := io.ReadAll(f)
				if string(b) != "thumb" || r.FormValue("duration") != "7" {
					t.Error("video metadata changed")
				}
			}
			fmt.Fprintf(w, `{"code":0,"data":{"aes_key":"synthetic-key","file_id":"test-file+/=","file_size":%d}}`, len(payload))
			return
		}
		if r.Method != "GET" || r.URL.Query().Get("id") != "test-file+/=" || r.URL.Query().Get("key") != "synthetic-key" {
			t.Error("CDN identifier corrupted")
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(payload)
	})
	i, err := cdn.Instance.UploadImage("test-peer", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	r, err := cdn.Instance.DownloadImage(i.GetFileId(), i.GetAesKey())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if !bytes.Equal(got, payload) {
		t.Fatal("image roundtrip changed")
	}
	v, err := cdn.Instance.UploadVideo("test-peer", []byte("thumb"), bytes.NewReader(payload), 7)
	if err != nil {
		t.Fatal(err)
	}
	r, err = cdn.Instance.DownloadVideo(v.GetFileId(), v.GetAesKey())
	if err != nil {
		t.Fatal(err)
	}
	got, _ = io.ReadAll(r)
	r.Close()
	if !bytes.Equal(got, payload) {
		t.Fatal("video roundtrip changed")
	}
	cover, err := cdn.Instance.DownloadVideoCover(v.GetFileId(), v.GetAesKey())
	if err != nil || !bytes.Equal(cover, payload) {
		t.Fatal("cover roundtrip", err)
	}
	sns, err := cdn.Instance.UploadMomentsImage(payload)
	if err != nil || sns.GetImageWidth() != 12 || sns.GetImageHeight() != 34 || sns.GetFileUrl() != "https://example.invalid/image" {
		t.Fatal("SNS image mapping", err)
	}
	malformed.Store(true)
	if _, err := cdn.Instance.UploadImage("test-peer", bytes.NewReader(payload)); err == nil {
		t.Fatal("empty upload receipt accepted")
	}
	if _, err := cdn.Instance.UploadVideo("test-peer", []byte("thumb"), bytes.NewReader(payload), 7); err == nil {
		t.Fatal("empty video receipt accepted")
	}
	if _, err := cdn.Instance.UploadMomentsImage(payload); err == nil {
		t.Fatal("empty SNS receipt accepted")
	}
	if _, err := cdn.Instance.DownloadImage("test-file+/=", "synthetic-key"); err == nil {
		t.Fatal("JSON saved as image")
	}
}

func TestVerifiedRemarkAndVoidTypingRemainCompatible(t *testing.T) {
	withCore(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/contacts/remark/") {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if r.Method != "PUT" || !reflect.DeepEqual(body, map[string]string{"remark": ""}) {
				t.Error("empty remark must remain explicit")
			}
			// core 已验证 packed 状态并读回；JSON 无法表示 unknown protobuf 字段。
			io.WriteString(w, `{"code":0,"data":{"code":0,"result":{"count":1}}}`)
			return
		}
		if r.Method != "POST" || !reflect.DeepEqual(r.URL.Query(), url.Values{"receiver": {"test-peer"}}) {
			t.Error("typing contract")
		}
		io.WriteString(w, `{"code":0,"data":null}`) // 合法的 void 操作
	})
	if err := contact.Instance.SetRemark("test-peer", ""); err != nil {
		t.Fatal(err)
	}
	if err := report.Instance.StartTyping("test-peer"); err != nil {
		t.Fatal(err)
	}
	if err := report.Instance.StopTyping("test-peer"); err != nil {
		t.Fatal(err)
	}
}

func TestSuccessEnvelopeStillRequiresOperationResults(t *testing.T) {
	withCore(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0},"list":[-4]}}`)
	})
	if _, err := moments.Instance.Unlike(42); err == nil {
		t.Fatal("nested unlike rejection swallowed")
	}
}

func TestPackedGroupNameReceiptRequiresReadback(t *testing.T) {
	var writes, reads atomic.Int32
	var mismatch atomic.Bool
	withCore(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/chatroom/name/test-room@chatroom":
			writes.Add(1)
			if r.Method != "PUT" || r.URL.Query().Get("name") != "测试群" {
				t.Error("group-name request changed")
			}
			io.WriteString(w, `{"code":0,"data":{"code":0,"result":{"count":1}}}`)
		case "/api/contacts/detail":
			reads.Add(1)
			var names []string
			json.NewDecoder(r.Body).Decode(&names)
			if r.Method != "POST" || !reflect.DeepEqual(names, []string{"test-room@chatroom"}) {
				t.Error("wrong readback target")
			}
			name := "测试群"
			if mismatch.Load() {
				name = "unchanged"
			}
			fmt.Fprintf(w, `{"code":0,"data":{"base_response":{"code":0},"contact_list":[{"username":"test-room@chatroom","nickname":%q}]}}`, name)
		default:
			t.Error("unexpected route")
			w.WriteHeader(404)
		}
	})
	if _, err := chatroom.Instance.SetName("test-room@chatroom", "测试群"); err != nil {
		t.Fatal(err)
	}
	mismatch.Store(true)
	if _, err := chatroom.Instance.SetName("test-room@chatroom", "测试群"); err == nil {
		t.Fatal("unconfirmed group name returned success")
	}
	if writes.Load() != 2 || reads.Load() != 2 {
		t.Fatal("must perform one write and one read per call")
	}
}
