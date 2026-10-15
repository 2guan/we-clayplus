package message

import (
	"errors"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type downloadTestServer struct {
	UnimplementedMessageServiceServer
	download func(*Download_Request, grpc.ServerStreamingServer[Download_Response]) error
}

func (s *downloadTestServer) Download(req *Download_Request, stream grpc.ServerStreamingServer[Download_Response]) error {
	return s.download(req, stream)
}

func TestDownloadReadsFramesAsTheyArrive(t *testing.T) {
	firstSent := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	server := &downloadTestServer{download: func(_ *Download_Request, stream grpc.ServerStreamingServer[Download_Response]) error {
		if err := stream.Send(&Download_Response{Data: []byte("abc")}); err != nil {
			return err
		}
		close(firstSent)
		select {
		case <-release:
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
		return stream.Send(&Download_Response{Data: []byte("defgh")})
	}}
	client := Client{Client: dialMessage(t, server, false)}
	type result struct {
		reader io.ReadCloser
		err    error
	}
	ready := make(chan result, 1)
	go func() {
		reader, err := client.Download(&Message{})
		ready <- result{reader, err}
	}()
	var got result
	select {
	case got = <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("Download waited for the whole stream")
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	defer got.reader.Close()
	select {
	case <-firstSent:
	case <-time.After(2 * time.Second):
		t.Fatal("first frame was not sent")
	}
	buf := make([]byte, 2)
	if n, err := got.reader.Read(buf); n != 2 || err != nil || string(buf) != "ab" {
		t.Fatalf("first read = (%d, %v, %q)", n, err, buf)
	}
	if n, err := got.reader.Read(buf); n != 1 || err != nil || string(buf[:n]) != "c" {
		t.Fatalf("remaining first frame = (%d, %v, %q)", n, err, buf[:n])
	}
	release <- struct{}{}
	rest, err := io.ReadAll(got.reader)
	if err != nil || string(rest) != "defgh" {
		t.Fatalf("rest = (%q, %v)", rest, err)
	}
	if n, err := got.reader.Read(buf); n != 0 || err != io.EOF {
		t.Fatalf("after EOF = (%d, %v)", n, err)
	}
}

func TestDownloadCloseCancelsBlockedRead(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	server := &downloadTestServer{download: func(_ *Download_Request, stream grpc.ServerStreamingServer[Download_Response]) error {
		close(started)
		<-stream.Context().Done()
		close(cancelled)
		return stream.Context().Err()
	}}
	client := Client{Client: dialMessage(t, server, false)}
	reader, err := client.Download(&Message{})
	if err != nil {
		t.Fatal(err)
	}
	readResult := make(chan error, 1)
	go func() {
		_, err := reader.Read(make([]byte, 1))
		readResult <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("download server did not start")
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal("second close:", err)
	}
	select {
	case err := <-readResult:
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("blocked read after close = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not unblock Read")
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel the gRPC stream")
	}
}

func TestDownloadReportsStreamErrorOnRead(t *testing.T) {
	server := &downloadTestServer{download: func(_ *Download_Request, stream grpc.ServerStreamingServer[Download_Response]) error {
		if err := stream.Send(&Download_Response{Data: []byte("ok")}); err != nil {
			return err
		}
		return status.Error(codes.FailedPrecondition, "upload incomplete")
	}}
	client := Client{Client: dialMessage(t, server, false)}
	reader, err := client.Download(&Message{})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	buf := make([]byte, 4)
	if n, err := reader.Read(buf); n != 2 || err != nil || string(buf[:n]) != "ok" {
		t.Fatalf("data before error = (%d, %v, %q)", n, err, buf[:n])
	}
	if n, err := reader.Read(buf); n != 0 || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stream error = (%d, %v)", n, err)
	}
}
