package message

import (
	"context"
	"io"
	"sync"
	"sync/atomic"

	"github.com/we-clay/we-clay/sdk"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Client 实现 Ability 接口，通过 gRPC 调用远程消息服务
type Client struct {
	Client MessageServiceClient
}

var _ Ability = (*Client)(nil)

// Send 发送消息（client-stream：首包消息元数据 + 后续二进制数据块）
func (c Client) Send(msg *Message) (*Send_Response, error) {
	return c.sendMessage(msg)
}

// Forward 转发消息
func (c Client) Forward(msg *Message, receiver string) (*Forward_Response, error) {
	return c.Client.Forward(context.Background(), &Forward_Request{
		Receiver: receiver,
		Message:  msg,
	})
}

// Revoke 撤回消息
func (c Client) Revoke(receiver string, newMsgId uint64) (*Revoke_Response, error) {
	return c.Client.Revoke(context.Background(), &Revoke_Request{
		Receiver: receiver,
		NewId:    newMsgId,
	})
}

// Pat 在指定会话中拍一拍指定联系人。
func (c Client) Pat(receiver, pattedUsername string) (*Pat_Response, error) {
	return c.Client.Pat(context.Background(), &Pat_Request{Receiver: receiver, PattedUsername: pattedUsername})
}

// TranscribeVoice 请求 gateway 转写现有的语音消息。
func (c Client) TranscribeVoice(ctx context.Context, msg *Message) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r, err := c.Client.TranscribeVoice(ctx, &TranscribeVoice_Request{Message: msg})
	if err != nil {
		return "", err
	}
	return r.GetText(), nil
}

// Download 下载媒体资源（server-stream）。调用方须关闭 reader；流中的错误会在 Read 时返回。
func (c Client) Download(msg *Message) (io.ReadCloser, error) {
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := c.Client.Download(ctx, &Download_Request{Message: msg})
	if err != nil {
		cancel()
		return nil, err
	}
	return &downloadReader{stream: stream, cancel: cancel}, nil
}

type downloadReader struct {
	stream grpc.ServerStreamingClient[Download_Response]
	cancel context.CancelFunc
	readMu sync.Mutex
	closed atomic.Bool
	data   []byte
	err    error
}

func (r *downloadReader) Read(p []byte) (int, error) {
	if r.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	if len(p) == 0 {
		return 0, nil
	}
	r.readMu.Lock()
	defer r.readMu.Unlock()
	if r.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	for len(r.data) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		resp, err := r.stream.Recv()
		if r.closed.Load() {
			return 0, io.ErrClosedPipe
		}
		if err != nil {
			r.err = err
			r.cancel()
			return 0, err
		}
		if resp != nil {
			r.data = resp.GetData()
		}
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func (r *downloadReader) Close() error {
	if !r.closed.Swap(true) {
		r.cancel()
	}
	return nil
}

// Server 实现 MessageServiceServer 接口，将 gRPC 请求委托给 Ability 实现
type Server struct {
	UnimplementedMessageServiceServer
	Impl Ability
}

// Send 发送消息（client-stream）
func (s Server) Send(stream grpc.ClientStreamingServer[Send_Request, Send_Response]) error {
	msg, err := receiveMessage(stream)
	if err != nil {
		return err
	}
	if msg.GetFile() != nil && !s.supportsFileSend() {
		return status.Error(codes.Unimplemented, "gateway does not support file sending")
	}
	if err := stream.Context().Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	if s.Impl == nil {
		return status.Error(codes.Unavailable, "message ability is unavailable")
	}
	resp, err := s.Impl.Send(msg)
	if err != nil {
		return err
	}
	return stream.SendAndClose(resp)
}

// Forward 转发消息
func (s Server) Forward(ctx context.Context, request *Forward_Request) (*Forward_Response, error) {
	return s.Impl.Forward(request.Message, request.Receiver)
}

// Revoke 撤回消息
func (s Server) Revoke(ctx context.Context, request *Revoke_Request) (*Revoke_Response, error) {
	return s.Impl.Revoke(request.Receiver, request.NewId)
}

// Pat 将拍一拍请求交给消息能力实现。
func (s Server) Pat(ctx context.Context, request *Pat_Request) (*Pat_Response, error) {
	if s.Impl == nil {
		return nil, status.Error(codes.Unavailable, "message ability is unavailable")
	}
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "pat request is required")
	}
	return s.Impl.Pat(request.GetReceiver(), request.GetPattedUsername())
}

// TranscribeVoice 转发给消息能力实现。旧 gateway 不提供此 RPC。
func (s Server) TranscribeVoice(ctx context.Context, request *TranscribeVoice_Request) (*TranscribeVoice_Response, error) {
	if s.Impl == nil {
		return nil, status.Error(codes.Unavailable, "message ability is unavailable")
	}
	if request == nil || request.GetMessage() == nil {
		return nil, status.Error(codes.InvalidArgument, "voice message is required")
	}
	transcriber, ok := s.Impl.(VoiceTranscriptionAbility)
	if !ok {
		return nil, status.Error(codes.Unimplemented, "voice transcription ability is unavailable")
	}
	text, err := transcriber.TranscribeVoice(ctx, request.GetMessage())
	if err != nil {
		return nil, err
	}
	return &TranscribeVoice_Response{Text: text}, nil
}

// Download 下载媒体资源（server-stream）
func (s Server) Download(request *Download_Request, stream grpc.ServerStreamingServer[Download_Response]) error {
	reader, err := s.Impl.Download(request.Message)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	buf := make([]byte, sdk.PROTO_STREAM_CHUNK_SIZE)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(&Download_Response{Data: buf[:n]}); sendErr != nil {
				return sendErr
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	return nil
}
