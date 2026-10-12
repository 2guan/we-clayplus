package userability

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/we-clay/we-clay/gateway/internal/api"
	userapi "github.com/we-clay/we-clay/gateway/internal/api/user"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	sdk "github.com/we-clay/we-clay/sdk/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type failedAvatarReader struct{}

func (failedAvatarReader) Read([]byte) (int, error) { return 0, errors.New("synthetic reader failure") }

func TestAvatarInvalidInputsNeverReachCore(t *testing.T) {
	var calls atomic.Int32
	s := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); t.Error("invalid avatar reached core") }))
	defer s.Close()
	c, err := api.NewGRPCClient(context.Background(), s.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	old := api.GetCore
	api.GetCore = func() *api.Client { return c }
	defer func() { api.GetCore = old }()
	direct := &ability{api: userapi.Get()}
	// plugin 使用空客户端：非法输入必须在任何 RPC 前拒绝，不能触发空指针。
	for name, client := range map[string]sdk.Ability{"direct": direct, "plugin": sdk.Client{}} {
		for kind, reader := range map[string]io.Reader{"nil": nil, "empty": bytes.NewReader(nil), "read failure": failedAvatarReader{}, "too large": bytes.NewReader(make([]byte, sdk.MaxAvatarSize+1))} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				if _, err := client.UploadAvatar(reader); err == nil {
					t.Fatal("invalid input accepted")
				}
			})
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid input dispatched")
	}
}

func TestAvatarIncompleteAndRejectedReceiptsAreNotRetried(t *testing.T) {
	var body atomic.Value
	var calls atomic.Int32
	s := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); io.WriteString(w, body.Load().(string)) }))
	defer s.Close()
	c, err := api.NewGRPCClient(context.Background(), s.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	old := api.GetCore
	api.GetCore = func() *api.Client { return c }
	defer func() { api.GetCore = old }()
	a := &ability{api: userapi.Get()}
	for name, data := range map[string]string{
		"missing data": `null`, "missing status": `{}`, "rejected": `{"base_response":{"code":-4}}`,
		"wrong size":     `{"base_response":{"code":0},"size":99,"offset":4}`,
		"partial offset": `{"base_response":{"code":0},"size":4,"offset":3}`,
	} {
		t.Run(name, func(t *testing.T) {
			body.Store(`{"code":0,"data":` + data + `}`)
			before := calls.Load()
			if r, err := a.UploadAvatar(bytes.NewBufferString("test")); err == nil || r != nil {
				t.Fatal("invalid receipt became success")
			}
			if calls.Load() != before+1 {
				t.Fatal("upload retried")
			}
		})
	}
	body.Store(`{"code":0,"data":{"base_response":{"code":0}}}`)
	r, err := a.UploadAvatar(bytes.NewBufferString("test"))
	if err != nil || r.GetAvatar().GetSize() != 0 || r.GetAvatar().GetBigAvatarUrl() != "" {
		t.Fatal("optional absent fields must not be fabricated", err)
	}
}

// 核对 Client/插件 → gateway → core 的路由、multipart 和完整回执，不操作微信。
func TestAvatarUploadDirectAndPlugin(t *testing.T) {
	payload := bytes.Repeat([]byte("synthetic-avatar"), 2100) // 超过 core 单个 30000 字节块，gateway 不自行分块。
	var calls atomic.Int32
	s := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/user/avatar" {
			t.Error("avatar route mismatch")
			w.WriteHeader(400)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["avatar"]) != 1 || len(r.MultipartForm.Value) != 0 {
			t.Error("avatar multipart mismatch")
			w.WriteHeader(400)
			return
		}
		f, _, err := r.FormFile("avatar")
		if err != nil {
			t.Error(err)
			return
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil || !bytes.Equal(data, payload) {
			t.Error("avatar bytes changed")
			w.WriteHeader(400)
			return
		}
		fmt.Fprintf(w, `{"code":0,"data":{"base_response":{"code":0},"size":%d,"offset":%d,"final_md5":"synthetic-digest","big_avatar_url":"https://example.invalid/big","small_avatar_url":"https://example.invalid/small"}}`, len(data), len(data))
	}))
	defer s.Close()
	c, err := api.NewGRPCClient(context.Background(), s.Address, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	old := api.GetCore
	api.GetCore = func() *api.Client { return c }
	defer func() { api.GetCore = old }()
	direct := &ability{api: userapi.Get()}
	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.MaxRecvMsgSize(64 << 20))
	sdk.RegisterUserServiceServer(server, &sdk.Server{Impl: direct})
	go server.Serve(lis)
	defer server.Stop()
	defer lis.Close()
	conn, err := grpc.NewClient("passthrough:///avatar-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return lis.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	plugin := sdk.Client{Client: sdk.NewUserServiceClient(conn)}
	for name, client := range map[string]sdk.Ability{"direct": direct, "plugin": plugin} {
		t.Run(name, func(t *testing.T) {
			resp, err := client.UploadAvatar(bytes.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			if resp.GetBaseResult() == nil || resp.GetBaseResult().GetCode() != 0 {
				t.Fatal("missing business receipt")
			}
			a := resp.GetAvatar()
			if a.GetSize() != uint32(len(payload)) || a.GetOffset() != uint32(len(payload)) || a.GetFinalMd5() != "synthetic-digest" || a.GetBigAvatarUrl() != "https://example.invalid/big" || a.GetSmallAvatarUrl() != "https://example.invalid/small" {
				t.Fatal("avatar receipt fields lost")
			}
		})
	}
	if calls.Load() != 2 {
		t.Fatal("avatar uploaded zero or multiple times")
	}
}
