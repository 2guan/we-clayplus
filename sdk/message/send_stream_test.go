package message

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-clay/we-clay/sdk/contact"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

type sendRecorder struct {
	Ability
	calls    atomic.Int32
	received chan *Message
	failure  error
}

func (r *sendRecorder) SupportsFileSend() bool { return true }
func (r *sendRecorder) Send(m *Message) (*Send_Response, error) {
	r.calls.Add(1)
	r.received <- proto.Clone(m).(*Message)
	if r.failure != nil {
		return nil, r.failure
	}
	return &Send_Response{NewId: 123456789, CreateTime: 42}, nil
}
func newRecorder() *sendRecorder { return &sendRecorder{received: make(chan *Message, 4)} }
func dialMessage(t *testing.T, impl MessageServiceServer, legacy bool) MessageServiceClient {
	t.Helper()
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer() // Default 4 MiB receive limit; large media must be chunked.
	if legacy {
		desc := MessageService_ServiceDesc
		desc.Methods = nil // Original service has no capabilities method.
		server.RegisterService(&desc, impl)
	} else {
		RegisterMessageServiceServer(server, impl)
	}
	go server.Serve(listener)
	conn, err := grpc.NewClient("passthrough:///message-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(); server.Stop(); listener.Close() })
	return NewMessageServiceClient(conn)
}
func testMedia(typ int32, data, thumb []byte) *Message {
	m := &Message{Type: TypeOf(typ), Receiver: &contact.Contact{Username: "test-peer"}}
	media := &Media{Data: data, Size: uint32(len(data))}
	switch typ {
	case 3:
		m.Data = &Message_Image{Image: &ImageData{Media: media, Width: 42, Height: 21}}
	case 34:
		m.Data = &Message_Voice{Voice: &VoiceData{Media: media, Duration: 1500, Format: proto.Int32(4)}}
	case 43:
		m.Data = &Message_Video{Video: &VideoData{Media: media, Duration: 7, Thumb: thumb}}
	case 47:
		m.Data = &Message_Emoji{Emoji: &EmojiData{Media: media, Desc: "test"}}
	case 6:
		m.Data = &Message_File{File: &FileData{Filename: "测试-file.bin", Media: media}}
	}
	return m
}
func TestChunkedSendPreservesMediaAndCaller(t *testing.T) {
	payload := bytes.Repeat([]byte{0, 255, 1, 127, 2}, (5<<20)/5+1)
	thumb := bytes.Repeat([]byte{42}, (4<<20)+13)
	for _, typ := range []int32{3, 34, 43, 47, 6} {
		t.Run(TypeOf(typ).Desc, func(t *testing.T) {
			rec := newRecorder()
			client := Client{Client: dialMessage(t, &Server{Impl: rec}, false)}
			msg := testMedia(typ, payload, thumb)
			before := proto.Clone(msg)
			response, err := client.Send(msg)
			if err != nil || response.GetNewId() != 123456789 {
				t.Fatalf("response=%v err=%v", response, err)
			}
			got := <-rec.received
			if !proto.Equal(got, before) || !proto.Equal(msg, before) {
				t.Fatal("media/metadata changed or caller was mutated")
			}
			if rec.calls.Load() != 1 {
				t.Fatal("duplicate dispatch")
			}
		})
	}
}
func TestLegacyInlineClientOnNewServer(t *testing.T) {
	rec := newRecorder()
	client := dialMessage(t, &Server{Impl: rec}, false)
	for _, msg := range []*Message{{Type: TypeText, Receiver: &contact.Contact{Username: "test-peer"}, Content: "legacy text"}, testMedia(3, []byte{1, 2, 3}, nil)} {
		stream, err := client.Send(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err = stream.Send(&Send_Request{Message: msg}); err != nil {
			t.Fatal(err)
		}
		response, err := stream.CloseAndRecv()
		if err != nil || response.GetNewId() == 0 {
			t.Fatalf("response=%v err=%v", response, err)
		}
		if !proto.Equal(<-rec.received, msg) {
			t.Fatal("legacy inline data changed")
		}
	}
}

type legacyMessageServer struct {
	UnimplementedMessageServiceServer
	recorder *sendRecorder
}

func (s *legacyMessageServer) Send(stream grpc.ClientStreamingServer[Send_Request, Send_Response]) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	if req.GetChunked() || req.GetDataSize() != 0 || req.GetThumbSize() != 0 || len(req.GetData()) > 0 || len(req.GetThumb()) > 0 {
		return status.Error(codes.InvalidArgument, "new fields sent to legacy server")
	}
	// The old implementation consumed and ignored all subsequent frames.
	for {
		_, err = stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	response, err := s.recorder.Send(req.Message)
	if err != nil {
		return err
	}
	return stream.SendAndClose(response)
}
func TestNewClientOnLegacyHost(t *testing.T) {
	rec := newRecorder()
	client := Client{Client: dialMessage(t, &legacyMessageServer{recorder: rec}, true)}
	small := testMedia(3, []byte{1, 2, 3}, nil)
	if _, err := client.Send(small); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(<-rec.received, small) {
		t.Fatal("fallback lost inline bytes")
	}
	for _, msg := range []*Message{testMedia(6, []byte{1}, nil), testMedia(3, make([]byte, 5<<20), nil)} {
		if _, err := client.Send(msg); err == nil {
			t.Fatal("unsupported extension accepted by old host")
		}
	}
	if rec.calls.Load() != 1 {
		t.Fatal("unsupported media reached old host")
	}
}
func TestSendFailureIsNeverRetried(t *testing.T) {
	rec := newRecorder()
	rec.failure = status.Error(codes.Unavailable, "delivery unknown")
	client := Client{Client: dialMessage(t, &Server{Impl: rec}, false)}
	_, err := client.Send(testMedia(6, []byte{1, 2, 3}, nil))
	if status.Code(err) != codes.Unavailable || rec.calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, rec.calls.Load())
	}
}
func TestMalformedStreamsNeverDispatch(t *testing.T) {
	header := func() *Send_Request {
		return &Send_Request{Message: testMedia(6, nil, nil), Chunked: true, DataSize: 3}
	}
	cases := map[string][]*Send_Request{
		"no header":                {},
		"data first":               {{Data: []byte{1}}},
		"truncated":                {header(), {Data: []byte{1, 2}}},
		"overrun":                  {header(), {Data: []byte{1, 2, 3, 4}}},
		"duplicate header":         {header(), header()},
		"empty chunk":              {header(), {}},
		"both chunk types":         {header(), {Data: []byte{1}, Thumb: []byte{2}}},
		"oversized chunk":          {header(), {Data: make([]byte, (512<<10)+1)}},
		"excessive declaration":    {{Message: testMedia(6, nil, nil), Chunked: true, DataSize: (32 << 20) + 1}},
		"inline mixed with chunks": {{Message: testMedia(6, []byte{1}, nil), Chunked: true, DataSize: 1}},
		"text with chunks":         {{Message: &Message{Type: TypeText, Content: "test"}, Chunked: true, DataSize: 3}},
		"extra legacy frame":       {{Message: testMedia(3, []byte{1}, nil)}, {Data: []byte{2}}},
		"thumbnail on file":        {{Message: testMedia(6, nil, nil), Chunked: true, DataSize: 3, ThumbSize: 1}},
		"missing thumbnail":        {{Message: testMedia(43, nil, nil), Chunked: true, DataSize: 3}},
	}
	for name, frames := range cases {
		t.Run(name, func(t *testing.T) {
			rec := newRecorder()
			client := dialMessage(t, &Server{Impl: rec}, false)
			stream, err := client.Send(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range frames {
				if err = stream.Send(f); err != nil {
					break
				}
			}
			_, err = stream.CloseAndRecv()
			if err == nil || rec.calls.Load() != 0 {
				t.Fatalf("err=%v dispatches=%d", err, rec.calls.Load())
			}
		})
	}
}
func TestCancelledStreamNeverDispatches(t *testing.T) {
	rec := newRecorder()
	client := dialMessage(t, &Server{Impl: rec}, false)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.Send(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = stream.Send(&Send_Request{Message: testMedia(6, nil, nil), Chunked: true, DataSize: 3}); err != nil {
		t.Fatal(err)
	}
	cancel()
	_, err = stream.CloseAndRecv()
	if status.Code(err) != codes.Canceled {
		t.Fatal(err)
	}
	select {
	case <-rec.received:
		t.Fatal("cancelled partial data dispatched")
	case <-time.After(20 * time.Millisecond):
	}
}

// Freeze existing names, tags, types, and RPC signatures from the pre-change
// descriptor. New fields/methods are allowed; existing wire contracts are not.
func TestExistingMessageWireContractUnchanged(t *testing.T) {
	raw, err := os.ReadFile("testdata/legacy-message-descriptor.pb")
	if err != nil {
		t.Fatal(err)
	}
	old := new(descriptorpb.FileDescriptorSet)
	if err = proto.Unmarshal(raw, old); err != nil {
		t.Fatal(err)
	}
	var check func(string, []*descriptorpb.DescriptorProto, []*descriptorpb.DescriptorProto)
	check = func(path string, before, after []*descriptorpb.DescriptorProto) {
		for _, b := range before {
			var a *descriptorpb.DescriptorProto
			for _, candidate := range after {
				if candidate.GetName() == b.GetName() {
					a = candidate
					break
				}
			}
			if a == nil {
				t.Fatalf("removed message %s.%s", path, b.GetName())
			}
			for _, bf := range b.Field {
				var af *descriptorpb.FieldDescriptorProto
				for _, f := range a.Field {
					if f.GetNumber() == bf.GetNumber() {
						af = f
						break
					}
				}
				if af == nil || af.GetName() != bf.GetName() || af.GetType() != bf.GetType() || af.GetTypeName() != bf.GetTypeName() || af.GetLabel() != bf.GetLabel() || af.GetOneofIndex() != bf.GetOneofIndex() || af.GetProto3Optional() != bf.GetProto3Optional() {
					t.Fatalf("changed field %s.%s.%s", path, b.GetName(), bf.GetName())
				}
			}
			check(path+"."+b.GetName(), b.NestedType, a.NestedType)
		}
	}
	for _, b := range old.File {
		fd, e := protoregistry.GlobalFiles.FindFileByPath(b.GetName())
		if e != nil {
			t.Fatal(e)
		}
		a := protodesc.ToFileDescriptorProto(fd)
		check(b.GetPackage(), b.MessageType, a.MessageType)
		for _, bs := range b.Service {
			var as *descriptorpb.ServiceDescriptorProto
			for _, candidate := range a.Service {
				if candidate.GetName() == bs.GetName() {
					as = candidate
					break
				}
			}
			if as == nil {
				t.Fatalf("removed service %s", bs.GetName())
			}
			for _, bm := range bs.Method {
				var am *descriptorpb.MethodDescriptorProto
				for _, candidate := range as.Method {
					if candidate.GetName() == bm.GetName() {
						am = candidate
						break
					}
				}
				if am == nil || am.GetInputType() != bm.GetInputType() || am.GetOutputType() != bm.GetOutputType() || am.GetClientStreaming() != bm.GetClientStreaming() || am.GetServerStreaming() != bm.GetServerStreaming() {
					t.Fatalf("changed RPC %s.%s", bs.GetName(), bm.GetName())
				}
			}
		}
	}
}

func TestMD5OnlyEmojiRemainsCompatible(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		rec := newRecorder()
		var server MessageServiceServer = &Server{Impl: rec}
		if legacy {
			server = &legacyMessageServer{recorder: rec}
		}
		client := Client{Client: dialMessage(t, server, legacy)}
		msg := testMedia(47, nil, nil)
		msg.GetEmoji().GetMedia().Md5 = "d41d8cd98f00b204e9800998ecf8427e"
		if _, err := client.Send(msg); err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(<-rec.received, msg) {
			t.Fatal("MD5-only emoji changed")
		}
	}
}
