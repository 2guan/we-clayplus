package offline_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/we-clay/we-clay/gateway/internal/offline"
	userapi "github.com/we-clay/we-clay/gateway/internal/api/user"
	"github.com/we-clay/we-clay/sdk/corewire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestOfflineServerLifecycleAndAPIs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tmpDir := t.TempDir()
	srv, err := offline.Start(ctx, tmpDir)
	if err != nil {
		t.Fatalf("启动脱机服务失败: %v", err)
	}
	defer srv.Close()

	if srv.Address == "" {
		t.Fatal("服务地址为空")
	}

	conn, err := grpc.NewClient(srv.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连接脱机服务失败: %v", err)
	}
	defer conn.Close()

	client := corewire.NewCoreServiceClient(conn)

	// 1. 测试个人资料
	resp, err := client.Invoke(ctx, &corewire.CallRequest{Method: "GET", Path: "/api/user/profile"})
	if err != nil {
		t.Fatalf("调用 /api/user/profile 失败: %v", err)
	}
	if resp.Status != 200 {
		t.Fatalf("期望状态码 200，实际: %d", resp.Status)
	}
	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &env); err != nil {
		t.Fatalf("反序列化外层信封失败: %v", err)
	}
	var profile userapi.GetProfileResponse
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(env.Data, &profile); err != nil {
		t.Fatalf("protojson 反序列化资料失败: %v, raw: %s", err, string(env.Data))
	}
	if profile.GetUserInfo().GetNickName().GetValue() == "" {
		t.Fatalf("昵称不能为空: %+v", profile.GetUserInfo())
	}

	// 2. 测试联系人列表
	contactsResp, err := client.Invoke(ctx, &corewire.CallRequest{Method: "GET", Path: "/api/contacts"})
	if err != nil {
		t.Fatalf("调用 /api/contacts 失败: %v", err)
	}
	var contactsEnv struct {
		Code int      `json:"code"`
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(contactsResp.Body, &contactsEnv); err != nil || len(contactsEnv.Data) == 0 {
		t.Fatalf("联系人列表应非空: %v", err)
	}

	// 3. 测试朋友圈时间线
	momentsResp, err := client.Invoke(ctx, &corewire.CallRequest{Method: "GET", Path: "/api/moments/timeline"})
	if err != nil {
		t.Fatalf("调用 /api/moments/timeline 失败: %v", err)
	}
	if momentsResp.Status != 200 {
		t.Fatalf("朋友圈时间线应返回 200")
	}

	// 4. 测试发送文本消息
	msgBody, _ := json.Marshal(map[string]string{"receiver": "doubleguan", "content": "测试离线消息"})
	msgResp, err := client.Invoke(ctx, &corewire.CallRequest{Method: "POST", Path: "/api/message/text", Body: msgBody})
	if err != nil {
		t.Fatalf("发送文本消息失败: %v", err)
	}
	if msgResp.Status != 200 {
		t.Fatalf("发送消息应返回 200")
	}

	// 5. 测试事件订阅长流
	stream, err := client.Events(ctx)
	if err != nil {
		t.Fatalf("建立事件流失败: %v", err)
	}
	if err := stream.Send(&corewire.EventControl{Control: &corewire.EventControl_Subscribe{Subscribe: &corewire.Subscribe{Version: corewire.Version}}}); err != nil {
		t.Fatalf("发送订阅控制包失败: %v", err)
	}
	event, err := stream.Recv()
	if err != nil {
		t.Fatalf("接收事件失败: %v", err)
	}
	if !event.Ready {
		t.Fatalf("首次事件应为 Ready: true")
	}
}
