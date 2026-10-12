// Package coretest provides an offline gRPC core fixture. No WeChat or HTTP
// network connection is made; HTTP-shaped assertions exercise existing models.
package coretest

import (
	"bytes"
	"context"
	"github.com/we-clay/we-clay/sdk/corewire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

type Server struct {
	Address string
	server  *grpc.Server
}

func (s *Server) Close() { s.server.Stop() }
func NewService(t *testing.T, impl corewire.CoreServiceServer) *Server {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer(grpc.MaxRecvMsgSize(corewire.MaxMessageBytes), grpc.MaxSendMsgSize(corewire.MaxMessageBytes))
	corewire.RegisterCoreServiceServer(s, impl)
	go s.Serve(l)
	out := &Server{Address: l.Addr().String(), server: s}
	t.Cleanup(out.Close)
	return out
}

type adapter struct {
	corewire.UnimplementedCoreServiceServer
	handler http.Handler
}

func (a *adapter) Invoke(ctx context.Context, q *corewire.CallRequest) (*corewire.CallResponse, error) {
	address := "http://127.0.0.1:8086" + q.GetPath()
	if q.GetQuery() != "" {
		address += "?" + q.GetQuery()
	}
	r, err := http.NewRequestWithContext(ctx, q.GetMethod(), address, bytes.NewReader(q.GetBody()))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", q.GetContentType())
	md, _ := metadata.FromIncomingContext(ctx)
	if v := md.Get("authorization"); len(v) == 1 {
		r.Header.Set("Authorization", v[0])
	}
	w := httptest.NewRecorder()
	_ = grpc.SendHeader(ctx, metadata.Pairs("we-clay-protocol", "1"))
	a.handler.ServeHTTP(w, r)
	if r.MultipartForm != nil {
		_ = r.MultipartForm.RemoveAll()
	}
	return &corewire.CallResponse{Status: int32(w.Code), ContentType: w.Header().Get("Content-Type"), Body: w.Body.Bytes()}, nil
}
func NewServer(t *testing.T, h http.Handler) *Server { return NewService(t, &adapter{handler: h}) }
