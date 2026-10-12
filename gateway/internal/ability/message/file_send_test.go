package messageability

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"

	api "github.com/we-clay/we-clay/gateway/internal/api/message"
	"github.com/we-clay/we-clay/sdk/contact"
	sdk "github.com/we-clay/we-clay/sdk/message"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

type fileCall struct {
	receiver, filename string
	data               []byte
}
type fileAPI struct {
	api.MessageService
	calls    chan fileCall
	response *api.SendAppMessageResponse
	err      error
}

func (f *fileAPI) SendFile(receiver, filename string, r io.Reader) (*api.SendAppMessageResponse, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	f.calls <- fileCall{receiver, filename, b}
	return f.response, f.err
}
func TestGRPCFileReachesHostAPI(t *testing.T) {
	payload := bytes.Repeat([]byte{0, 255, 2, 4, 127}, (5<<20)/5+1)
	f := &fileAPI{calls: make(chan fileCall, 1), response: &api.SendAppMessageResponse{NewId: proto.Uint64(9007199254740993), CreateTime: proto.Int32(1791774000)}}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	sdk.RegisterMessageServiceServer(server, &sdk.Server{Impl: &ability{api: f}})
	go server.Serve(listener)
	conn, err := grpc.NewClient("passthrough:///file-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	defer server.Stop()
	defer listener.Close()
	client := sdk.Client{Client: sdk.NewMessageServiceClient(conn)}
	msg := &sdk.Message{Type: sdk.TypeFile, Receiver: &contact.Contact{Username: "test-peer"}, Data: &sdk.Message_File{File: &sdk.FileData{Filename: "测试 文件.bin", Media: &sdk.Media{Data: payload}}}}
	response, err := client.Send(msg)
	if err != nil {
		t.Fatal(err)
	}
	call := <-f.calls
	if call.receiver != "test-peer" || call.filename != "测试 文件.bin" || !bytes.Equal(call.data, payload) {
		t.Fatal("file metadata or bytes changed across gRPC and host")
	}
	if response.GetNewId() != 9007199254740993 || response.GetCreateTime() != 1791774000 {
		t.Fatal("receipt lost precision or timestamp")
	}
	select {
	case <-f.calls:
		t.Fatal("duplicate file send")
	default:
	}
	if msg.GetContent() != "" || !bytes.Equal(msg.GetFile().GetMedia().GetData(), payload) {
		t.Fatal("caller mutated")
	}
}
func TestFileRejectsInvalidOrUnconfirmedSend(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response *api.SendAppMessageResponse
		err      error
	}{
		{"error", nil, errors.New("synthetic failure")}, {"nil receipt", nil, nil}, {"zero id", &api.SendAppMessageResponse{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fileAPI{calls: make(chan fileCall, 2), response: tc.response, err: tc.err}
			a := &ability{api: f}
			msg := &sdk.Message{Type: sdk.TypeFile, Receiver: &contact.Contact{Username: "test-peer"}, Data: &sdk.Message_File{File: &sdk.FileData{Filename: "test.txt", Media: &sdk.Media{Data: []byte("test")}}}}
			if _, err := a.Send(msg); err == nil {
				t.Fatal("unconfirmed delivery reported success")
			}
			if len(f.calls) != 1 {
				t.Fatal("send retried")
			}
			msg.GetFile().Filename = ""
			if _, err := a.Send(msg); err == nil || len(f.calls) != 1 {
				t.Fatal("invalid file reached API")
			}
		})
	}
}

type voiceAPI struct {
	api.MessageService
	format   int32
	duration int32
	data     []byte
}

func (f *voiceAPI) SendVoice(_ string, r io.Reader, duration, format int32) (*api.UploadVoiceResponse, error) {
	f.format = format
	f.duration = duration
	f.data, _ = io.ReadAll(r)
	return &api.UploadVoiceResponse{NewId: proto.Uint64(123)}, nil
}
func TestVoiceFormatDefaultAndExplicitZero(t *testing.T) {
	for _, tc := range []struct {
		format *int32
		want   int32
	}{{nil, 4}, {proto.Int32(0), 0}, {proto.Int32(4), 4}} {
		f := new(voiceAPI)
		a := &ability{api: f}
		msg := &sdk.Message{Type: sdk.TypeVoice, Receiver: &contact.Contact{Username: "test-peer"}, Data: &sdk.Message_Voice{Voice: &sdk.VoiceData{Media: &sdk.Media{Data: []byte{1, 2}}, Duration: 1500, Format: tc.format}}}
		if _, err := a.Send(msg); err != nil {
			t.Fatal(err)
		}
		if f.format != tc.want || f.duration != 1500 || !bytes.Equal(f.data, []byte{1, 2}) {
			t.Fatal("voice format/duration/bytes changed")
		}
	}
}
