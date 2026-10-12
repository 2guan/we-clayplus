package momentsability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/we-clay/we-clay/gateway/internal/api"
	momentsapi "github.com/we-clay/we-clay/gateway/internal/api/moments"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	sdk "github.com/we-clay/we-clay/sdk/moments"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// 使用本地 core gRPC 适配器和插件 gRPC，核对两条调用路径的真实编码。
func commentClients(t *testing.T, handler http.HandlerFunc) map[string]sdk.Ability {
	t.Helper()
	server := coretest.NewServer(t, handler)
	t.Cleanup(server.Close)
	core, err := api.NewGRPCClient(context.Background(), server.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { core.Close() })
	old := api.GetCore
	api.GetCore = func() *api.Client { return core }
	t.Cleanup(func() { api.GetCore = old })
	direct := &ability{api: momentsapi.Get()}
	lis := bufconn.Listen(1 << 20)
	t.Cleanup(func() { lis.Close() })
	rpc := grpc.NewServer()
	sdk.RegisterMomentsServiceServer(rpc, &sdk.Server{Impl: direct})
	go rpc.Serve(lis)
	t.Cleanup(rpc.Stop)
	conn, err := grpc.NewClient("passthrough:///comment-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return map[string]sdk.Ability{"direct": direct, "plugin": sdk.Client{Client: sdk.NewMomentsServiceClient(conn)}}
}

func TestCommentDirectAndPluginPreserveRequestsAndReceipts(t *testing.T) {
	const momentID uint64 = 18446744073709551614
	const content = "测试留言：你好 🌱\n第二行"
	var calls atomic.Int32
	clients := commentClients(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/moments/comment/"+strconv.FormatUint(momentID, 10) || r.URL.RawQuery != "" {
			t.Error("comment route or ID changed")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var fields map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil || len(fields) != 3 {
			t.Error("comment request shape mismatch", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var text string
		var typ, reply int32
		if json.Unmarshal(fields["content"], &text) != nil || json.Unmarshal(fields["type"], &typ) != nil || json.Unmarshal(fields["reply_comment_id"], &reply) != nil || text != content || typ != 2 || (reply != 0 && reply != 7) {
			t.Error("comment parameters changed")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		encoded, _ := json.Marshal(text)
		fmt.Fprintf(w, `{"code":0,"data":{"base_response":{"code":0},"object":{"id":"%d","username":"test-author","object_description":"<TimelineObject/>","comment_count":1,"comment_user_list":[{"username":"test-self","nickname":"测试","type":2,"content":%s,"create_time":123456,"comment_id":123,"reply_comment_id":%d,"reply_username":"test-peer","comment_id2":"18446744073709551613","reply_comment_id2":"18446744073709551612","delete_flag":0,"comment_flag":3}]}}}`, momentID, encoded, reply)
	})
	for name, client := range clients {
		for _, reply := range []int32{0, 7} {
			t.Run(fmt.Sprintf("%s/reply-%d", name, reply), func(t *testing.T) {
				result, err := client.Comment(momentID, content, 2, reply)
				if err != nil {
					t.Fatal(err)
				}
				obj := result.GetObject()
				if result.GetBaseResult() == nil || result.GetBaseResult().GetCode() != 0 || obj.GetId() != momentID || obj.GetUsername() != "test-author" || obj.GetObjectDescription() != "<TimelineObject/>" || obj.GetCommentCount() != 1 || len(obj.GetCommentUserList()) != 1 {
					t.Fatal("comment object receipt lost")
				}
				comment := obj.GetCommentUserList()[0]
				if comment.GetUsername() != "test-self" || comment.GetNickname() != "测试" || comment.GetContent() != content || comment.GetType() != 2 || comment.GetCreateTime() != 123456 || comment.GetCommentId() != 123 || comment.GetReplyCommentId() != reply || comment.GetReplyUsername() != "test-peer" || comment.GetCommentId2() != 18446744073709551613 || comment.GetReplyCommentId2() != 18446744073709551612 || comment.GetCommentFlag() != 3 {
					t.Fatal("comment/reply receipt fields lost")
				}
			})
		}
	}
	if calls.Load() != 4 {
		t.Fatal("unexpected request count")
	}
}

func TestCommentInvalidParametersNeverReachCore(t *testing.T) {
	var calls atomic.Int32
	clients := commentClients(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for name, client := range clients {
		for _, tc := range []struct {
			id         uint64
			text       string
			typ, reply int32
		}{
			{0, "test", 2, 0}, {1, "test", 0, 0}, {1, "test", -1, 0},
			{1, "test", 2, -1}, {1, "", 2, 0}, {1, " \n\t", 2, 0},
		} {
			if result, err := client.Comment(tc.id, tc.text, tc.typ, tc.reply); err == nil || result != nil {
				t.Fatalf("%s accepted invalid comment", name)
			}
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid comment reached core")
	}
}

func TestCommentRejectedAndAmbiguousReceiptsAreNotRetried(t *testing.T) {
	var body atomic.Value
	var calls atomic.Int32
	clients := commentClients(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, body.Load().(string))
	})
	for name, client := range clients {
		for kind, raw := range map[string]string{
			"missing data": `null`, "missing status": `{}`,
			"rejected":     `{"base_response":{"code":-4}}`,
			"wrong object": `{"base_response":{"code":0},"object":{"id":"2"}}`,
		} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				body.Store(`{"code":0,"data":` + raw + `}`)
				before := calls.Load()
				if result, err := client.Comment(1, "test", 2, 0); err == nil || result != nil {
					t.Fatal("invalid receipt became success")
				}
				if calls.Load() != before+1 {
					t.Fatal("comment automatically retried")
				}
			})
		}
		// core 允许只有基础回执；不捏造朋友圈对象或评论 ID。
		body.Store(`{"code":0,"data":{"base_response":{"code":0}}}`)
		result, err := client.Comment(1, "test", 2, 0)
		if err != nil || result.GetObject() != nil {
			t.Fatal("optional receipt object fabricated", err)
		}
	}
}
