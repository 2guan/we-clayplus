package message

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/we-clay/we-clay/sdk"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	maxSendMedia    = 128 << 20
	maxSendThumb    = 8 << 20
	maxSmallMedia   = 32 << 20
	legacySendLimit = 4 << 20
)

// FileSendSupport lets an ability opt in without changing the existing Ability
// interface, so implementations that only support the old messages still build.
type FileSendSupport interface{ SupportsFileSend() bool }

func (s Server) supportsFileSend() bool {
	v, ok := s.Impl.(FileSendSupport)
	return ok && v.SupportsFileSend()
}

func (s Server) GetSendCapabilities(context.Context, *SendCapabilities_Request) (*SendCapabilities_Response, error) {
	return &SendCapabilities_Response{
		ChunkedSend: true, MaxChunkBytes: sdk.PROTO_STREAM_CHUNK_SIZE,
		MaxMediaBytes: maxSendMedia, MaxThumbBytes: maxSendThumb,
		FileSend: s.supportsFileSend(),
	}, nil
}

// sendMedia identifies the binary body and enforces its existing core limit.
// It never modifies the caller's Message.
func sendMedia(msg *Message) (*Media, []byte, uint64, error) {
	if msg == nil {
		return nil, nil, 0, status.Error(codes.InvalidArgument, "message is required")
	}
	if msg.GetFile() != nil && msg.GetType().GetCode() != TypeFile.Code {
		return nil, nil, 0, status.Error(codes.InvalidArgument, "FileData requires the file message type")
	}
	var media *Media
	var thumb []byte
	limit := uint64(maxSmallMedia)
	switch msg.GetType().GetCode() {
	case TypeImage.Code:
		media = msg.GetImage().GetMedia()
	case TypeVoice.Code:
		media = msg.GetVoice().GetMedia()
	case TypeVideo.Code:
		media = msg.GetVideo().GetMedia()
		thumb = msg.GetVideo().GetThumb()
		limit = maxSendMedia
	case TypeEmoji.Code:
		media = msg.GetEmoji().GetMedia()
	case TypeFile.Code:
		if strings.TrimSpace(msg.GetFile().GetFilename()) == "" {
			return nil, nil, 0, status.Error(codes.InvalidArgument, "file name is required")
		}
		media = msg.GetFile().GetMedia()
	default:
		return nil, nil, 0, nil
	}
	if media == nil {
		return nil, nil, 0, status.Error(codes.InvalidArgument, "media body is required")
	}
	return media, thumb, limit, nil
}

func (c Client) sendMessage(msg *Message) (*Send_Response, error) {
	media, thumb, limit, err := sendMedia(msg)
	if err != nil {
		return nil, err
	}
	if c.Client == nil {
		return nil, status.Error(codes.Unavailable, "message client is unavailable")
	}
	if media == nil {
		return c.sendFrames(&Send_Request{Message: msg}, nil, nil, 0)
	}
	data := media.GetData()
	// Existing emoji sends may reference a server-cached MD5 without bytes.
	if len(data) == 0 && msg.GetType().GetCode() == TypeEmoji.Code && media.GetMd5() != "" {
		return c.sendFrames(&Send_Request{Message: msg}, nil, nil, 0)
	}
	if len(data) == 0 || uint64(len(data)) > limit || len(thumb) > maxSendThumb {
		return nil, status.Error(codes.InvalidArgument, "media is empty or exceeds its size limit")
	}
	if msg.GetType().GetCode() == TypeVideo.Code && len(thumb) == 0 {
		return nil, status.Error(codes.InvalidArgument, "video thumbnail is required")
	}
	// Negotiate with a read-only RPC before any Send. Never retry a Send after
	// an error: its delivery state may already be ambiguous.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	caps, err := c.Client.GetSendCapabilities(ctx, &SendCapabilities_Request{})
	cancel()
	if err != nil && status.Code(err) != codes.Unimplemented {
		return nil, err
	}
	if err != nil || caps == nil || !caps.GetChunkedSend() {
		if msg.GetFile() != nil {
			return nil, status.Error(codes.Unimplemented, "file sending requires an updated gateway")
		}
		frame := &Send_Request{Message: msg}
		if proto.Size(frame) > legacySendLimit {
			return nil, status.Error(codes.ResourceExhausted, "large media requires an updated gateway with chunked sending")
		}
		return c.sendFrames(frame, nil, nil, 0)
	}
	if msg.GetFile() != nil && !caps.GetFileSend() {
		return nil, status.Error(codes.Unimplemented, "gateway does not support file sending")
	}
	if uint64(len(data)) > caps.GetMaxMediaBytes() || uint64(len(thumb)) > caps.GetMaxThumbBytes() || caps.GetMaxChunkBytes() == 0 {
		return nil, status.Error(codes.ResourceExhausted, "media exceeds the gateway's advertised limits")
	}
	header := proto.Clone(msg).(*Message)
	headerMedia, _, _, err := sendMedia(header)
	if err != nil {
		return nil, err
	}
	headerMedia.Data = nil
	if header.GetVideo() != nil {
		header.GetVideo().Thumb = nil
	}
	chunkSize := min(int(caps.GetMaxChunkBytes()), sdk.PROTO_STREAM_CHUNK_SIZE)
	return c.sendFrames(&Send_Request{Message: header, Chunked: true, DataSize: uint64(len(data)), ThumbSize: uint64(len(thumb))}, data, thumb, chunkSize)
}

func (c Client) sendFrames(header *Send_Request, data, thumb []byte, chunkSize int) (*Send_Response, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := c.Client.Send(ctx)
	if err != nil {
		return nil, err
	}
	send := func(frame *Send_Request) error { return stream.Send(frame) }
	finishError := func(err error) (*Send_Response, error) {
		if err == io.EOF {
			return stream.CloseAndRecv()
		}
		return nil, err
	}
	if err = send(header); err != nil {
		return finishError(err)
	}
	for off := 0; off < len(data); off += chunkSize {
		end := min(off+chunkSize, len(data))
		if err = send(&Send_Request{Data: data[off:end]}); err != nil {
			return finishError(err)
		}
	}
	for off := 0; off < len(thumb); off += chunkSize {
		end := min(off+chunkSize, len(thumb))
		if err = send(&Send_Request{Thumb: thumb[off:end]}); err != nil {
			return finishError(err)
		}
	}
	return stream.CloseAndRecv()
}

func receiveMessage(stream grpc.ClientStreamingServer[Send_Request, Send_Response]) (*Message, error) {
	header, err := stream.Recv()
	if err == io.EOF {
		return nil, status.Error(codes.InvalidArgument, "message header is required")
	}
	if err != nil {
		return nil, err
	}
	if header.GetMessage() == nil || len(header.GetData()) != 0 || len(header.GetThumb()) != 0 {
		return nil, status.Error(codes.InvalidArgument, "first frame must contain only message metadata")
	}
	msg := header.GetMessage()
	media, thumb, limit, err := sendMedia(msg)
	if err != nil {
		return nil, err
	}
	if !header.GetChunked() {
		if header.GetDataSize() != 0 || header.GetThumbSize() != 0 {
			return nil, status.Error(codes.InvalidArgument, "size declarations require chunked mode")
		}
		md5OnlyEmoji := msg.GetType().GetCode() == TypeEmoji.Code && media.GetMd5() != ""
		if media != nil && ((len(media.GetData()) == 0 && !md5OnlyEmoji) || uint64(len(media.GetData())) > limit || len(thumb) > maxSendThumb) {
			return nil, status.Error(codes.InvalidArgument, "inline media is empty or exceeds its size limit")
		}
		_, err = stream.Recv()
		if err == io.EOF {
			return msg, nil
		}
		if err != nil {
			return nil, err
		}
		return nil, status.Error(codes.InvalidArgument, "legacy Send accepts exactly one frame")
	}
	if media == nil || len(media.GetData()) != 0 || len(thumb) != 0 {
		return nil, status.Error(codes.InvalidArgument, "chunked header requires a media body without inline bytes")
	}
	if header.GetDataSize() == 0 || header.GetDataSize() > limit || header.GetThumbSize() > maxSendThumb {
		return nil, status.Error(codes.ResourceExhausted, "declared media size exceeds its limit")
	}
	if msg.GetVideo() == nil && header.GetThumbSize() != 0 {
		return nil, status.Error(codes.InvalidArgument, "only video accepts thumbnail chunks")
	}
	if msg.GetVideo() != nil && header.GetThumbSize() == 0 {
		return nil, status.Error(codes.InvalidArgument, "video thumbnail is required")
	}
	var data, thumbnail []byte
	for {
		frame, e := stream.Recv()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if frame.GetMessage() != nil || frame.GetChunked() || frame.GetDataSize() != 0 || frame.GetThumbSize() != 0 {
			return nil, status.Error(codes.InvalidArgument, "metadata may appear only in the first frame")
		}
		d, t := frame.GetData(), frame.GetThumb()
		if (len(d) == 0) == (len(t) == 0) {
			return nil, status.Error(codes.InvalidArgument, "each frame must contain one nonempty data or thumbnail chunk")
		}
		if len(d) > sdk.PROTO_STREAM_CHUNK_SIZE || len(t) > sdk.PROTO_STREAM_CHUNK_SIZE {
			return nil, status.Error(codes.ResourceExhausted, "chunk exceeds its size limit")
		}
		if uint64(len(data))+uint64(len(d)) > header.GetDataSize() || uint64(len(thumbnail))+uint64(len(t)) > header.GetThumbSize() {
			return nil, status.Error(codes.InvalidArgument, "received bytes exceed the declared sizes")
		}
		data = append(data, d...)
		thumbnail = append(thumbnail, t...)
	}
	if uint64(len(data)) != header.GetDataSize() || uint64(len(thumbnail)) != header.GetThumbSize() {
		return nil, status.Error(codes.InvalidArgument, "incomplete media stream; nothing sent")
	}
	media.Data = data
	if msg.GetVideo() != nil {
		msg.GetVideo().Thumb = thumbnail
	}
	return msg, nil
}
