package sdk

import (
	"errors"
	"fmt"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strconv"
)

// CoreError 保留 core 信封及微信错误码；不要解析 Error() 的文字作业务判断。
// WeChatCode 为空表示 core 没有提供可确认的微信错误码（包括旧版本 core）。
type CoreError struct {
	Status     int
	Code       int64
	WeChatCode *int32
	Category   string
	Operation  string
	Restricted bool
	Message    string
}

func (e *CoreError) Error() string {
	return fmt.Sprintf("core returned status %d (code %d): %s", e.Status, e.Code, e.Message)
}

// GRPCStatus 让独立插件也能收到结构化错误，不新增私有 proto。
func (e *CoreError) GRPCStatus() *status.Status {
	code := codes.FailedPrecondition
	if e.Category == "rate_limited" {
		code = codes.ResourceExhausted
	}
	fields := map[string]string{"status": strconv.Itoa(e.Status), "code": strconv.FormatInt(e.Code, 10), "category": e.Category, "operation": e.Operation, "restricted": strconv.FormatBool(e.Restricted), "message": e.Message}
	if e.WeChatCode != nil {
		fields["wechat_code"] = strconv.FormatInt(int64(*e.WeChatCode), 10)
	}
	s := status.New(code, e.Error())
	detailed, err := s.WithDetails(&errdetails.ErrorInfo{Reason: "CORE_ERROR", Domain: "we-clay/core", Metadata: fields})
	if err != nil {
		return s
	}
	return detailed
}

// AsCoreError 同时识别直接调用的 error 与插件 gRPC ErrorInfo；不解析错误文字。
func AsCoreError(err error) (*CoreError, bool) {
	var direct *CoreError
	if errors.As(err, &direct) {
		return direct, true
	}
	s, ok := status.FromError(err)
	if !ok {
		return nil, false
	}
	for _, detail := range s.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok || info.Domain != "we-clay/core" || info.Reason != "CORE_ERROR" {
			continue
		}
		httpStatus, e1 := strconv.Atoi(info.Metadata["status"])
		code, e2 := strconv.ParseInt(info.Metadata["code"], 10, 64)
		if e1 != nil || e2 != nil {
			return nil, false
		}
		out := &CoreError{Status: httpStatus, Code: code, Category: info.Metadata["category"], Operation: info.Metadata["operation"], Restricted: info.Metadata["restricted"] == "true", Message: info.Metadata["message"]}
		if raw, exists := info.Metadata["wechat_code"]; exists {
			n, e := strconv.ParseInt(raw, 10, 32)
			if e != nil {
				return nil, false
			}
			v := int32(n)
			out.WeChatCode = &v
		}
		return out, true
	}
	return nil, false
}
