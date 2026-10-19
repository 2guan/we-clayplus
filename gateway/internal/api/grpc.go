package api

import (
	"context"
	"fmt"
	"github.com/we-clay/we-clay/gateway/internal/config"
	"github.com/we-clay/we-clay/sdk/corewire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"log/slog"
	"mime"
	"strings"

	"time"
)

func NewConfiguredClient(ctx context.Context, cfg *config.Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return NewGRPCClient(ctx, cfg.CoreAddress, cfg.CoreToken)
}

// 只允许回环地址；远程部署由受信任的加密隧道提供本机端点。
func NewGRPCClient(ctx context.Context, address, token string) (*Client, error) {
	if err := (&config.Config{CoreAddress: address}).Validate(); err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient("passthrough:///"+address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDisableRetry(), grpc.WithDisableServiceConfig(),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second, Timeout: 10 * time.Second, PermitWithoutStream: true}),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(corewire.MaxMessageBytes), grpc.MaxCallSendMsgSize(corewire.MaxMessageBytes)))
	if err != nil {
		return nil, err
	}
	return &Client{ctx: ctx, token: token, conn: conn, rpc: corewire.NewCoreServiceClient(conn)}, nil
}
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
func (c *Client) authenticated(ctx context.Context) context.Context {
	if c.token != "" {
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.token)
	}
	return ctx
}
func (r *Request) doGRPC() ([]byte, error) {
	if r.client.rpc == nil {
		return nil, fmt.Errorf("gateway 尚未运行")
	}
	ctx := r.client.ctx
	if r.ctx != nil {
		ctx = r.ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	started := time.Now()
	// 只记录类别与耗时，路径中的联系人 ID、正文、密钥都不进入诊断日志。
	defer func() {
		if elapsed := time.Since(started); elapsed >= time.Second {
			parts := strings.Split(strings.Trim(r.uri, "/"), "/")
			category := "core"
			if len(parts) > 1 {
				category = parts[1]
			}
			slog.Info("core 请求耗时", "category", category, "method", r.method, "elapsed_ms", elapsed.Milliseconds())
		}
	}()
	result, err := r.client.rpc.Invoke(r.client.authenticated(ctx), &corewire.CallRequest{
		Method: r.method, Path: r.uri, Query: r.query.Encode(), ContentType: r.contentType, Body: r.body,
	})
	// 结果未知时交给调用方处理，不能自动重发写操作。
	if err != nil {
		return nil, err
	}
	data, err := decodeResponse(int(result.GetStatus()), result.GetContentType(), result.GetBody())
	if err != nil {
		return nil, err
	}
	if r.binary {
		kind, _, err := mime.ParseMediaType(result.GetContentType())
		if err != nil || kind != "application/octet-stream" || len(data) == 0 {
			return nil, fmt.Errorf("core media response missing or invalid")
		}
	}
	return data, nil
}
func (c *Client) Events(ctx context.Context) (grpc.BidiStreamingClient[corewire.EventControl, corewire.CoreEvent], error) {
	if c.rpc == nil {
		return nil, fmt.Errorf("gRPC transport required")
	}
	return c.rpc.Events(c.authenticated(ctx))
}

// Context 是此连接绑定的运行周期。
func (c *Client) Context() context.Context {
	if c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}
