package api

import (
	"testing"

	baseapi "github.com/we-clay/we-clay/gateway/internal/api/base"
	"google.golang.org/protobuf/proto"
)

func TestCommandReceiptValidation(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		response   *baseapi.OperateResponse
		ok         bool
	}{
		{"empty", "/api/chatroom/name/test-room", &baseapi.OperateResponse{}, false},
		{"no result", "/api/chatroom/name/test-room", &baseapi.OperateResponse{Code: proto.Int32(0)}, false},
		{"empty result", "/api/chatroom/name/test-room", &baseapi.OperateResponse{Code: proto.Int32(0), Result: &baseapi.OperateResponse_Result{}}, false},
		{"nested failure", "/api/chatroom/name/test-room", &baseapi.OperateResponse{Code: proto.Int32(0), Result: &baseapi.OperateResponse_Result{Code: proto.Int32(-4)}}, false},
		{"nested success", "/api/chatroom/name/test-room", &baseapi.OperateResponse{Code: proto.Int32(0), Result: &baseapi.OperateResponse_Result{Code: proto.Int32(0)}}, true},
		{"count alone not generic success", "/api/other-operation", &baseapi.OperateResponse{Code: proto.Int32(0), Result: &baseapi.OperateResponse_Result{Count: proto.Uint32(1)}}, false},
		{"verified contact packed result", "/api/contacts/remark/test-peer", &baseapi.OperateResponse{Code: proto.Int32(0), Result: &baseapi.OperateResponse_Result{Count: proto.Uint32(1)}}, true},
		{"verified group packed result", "/api/chatroom/remark/test-room", &baseapi.OperateResponse{Code: proto.Int32(0), Result: &baseapi.OperateResponse_Result{Count: proto.Uint32(1)}}, true},
		{"verified save packed result", "/api/chatroom/contact-list/test-room", &baseapi.OperateResponse{Code: proto.Int32(0), Result: &baseapi.OperateResponse_Result{Count: proto.Uint32(1)}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateReceipt(tc.response, tc.path, nil)
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v, want success=%v", err, tc.ok)
			}
		})
	}
}
