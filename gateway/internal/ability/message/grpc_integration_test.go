package messageability

import (
	"bytes"
	"context"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"

	httpapi "github.com/we-clay/we-clay/gateway/internal/api"
	messageapi "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/contact"
	sdk "github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// Exercise the real SDK -> gRPC -> host -> gRPC adapter boundary without core source or WeChat.
func TestGRPCFileReachesSeparateGRPCCore(t *testing.T) {
	payload := bytes.Repeat([]byte("synthetic"), (5<<20)/9+1)
	var calls atomic.Int32
	core := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.Path != "/api/message/file" || r.Header.Get("Authorization") != "Bearer test-only" {
			t.Error("incorrect core request")
			w.WriteHeader(400)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer file.Close()
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		data, _ := io.ReadAll(file)
		if header.Filename != "边界测试.txt" || r.FormValue("receiver") != "test-peer" || !bytes.Equal(data, payload) {
			t.Error("file changed across transports")
		}
		_, _ = io.WriteString(w, `{"code":0,"data":{"new_id":"9007199254740993","create_time":42}}`)
	}))
	defer core.Close()
	c, err := httpapi.NewGRPCClient(context.Background(), core.Address, "test-only")
	if err != nil {
		t.Fatal(err)
	}
	old := httpapi.GetCore
	httpapi.GetCore = func() *httpapi.Client { return c }
	defer func() { httpapi.GetCore = old }()
	listener := bufconn.Listen(1 << 20)
	defer listener.Close()
	server := grpc.NewServer()
	defer server.Stop()
	sdk.RegisterMessageServiceServer(server, &sdk.Server{Impl: &ability{api: messageapi.Get()}})
	go server.Serve(listener)
	conn, err := grpc.NewClient("passthrough:///separate-core-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := sdk.Client{Client: sdk.NewMessageServiceClient(conn)}
	response, err := client.Send(&sdk.Message{Type: sdk.TypeFile, Receiver: &contact.Contact{Username: "test-peer"}, Data: &sdk.Message_File{File: &sdk.FileData{Filename: "边界测试.txt", Media: &sdk.Media{Data: payload}}}})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || response.NewId != 9007199254740993 || response.CreateTime != 42 {
		t.Fatal("receipt lost or request repeated")
	}
}
