package sdk

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrNotImplemented 表示此接口尚未实现或尚未完成实际验证，当前保留占位，不发送请求。
var ErrNotImplemented = errors.New("此接口尚未实现或未完成验证，当前仅保留占位")

type notImplementedError struct{ method string }

func (e *notImplementedError) Error() string { return e.method + ": " + ErrNotImplemented.Error() }
func (e *notImplementedError) Unwrap() error { return ErrNotImplemented }
func (e *notImplementedError) GRPCStatus() *status.Status {
	return status.New(codes.Unimplemented, e.Error())
}

// NotImplemented 用于预留能力；可用 errors.Is(err, ErrNotImplemented) 判断。
func NotImplemented(method string) error { return &notImplementedError{method: method} }
