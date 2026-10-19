package sdk

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"testing"
)

func TestCoreErrorSurvivesPluginGRPC(t *testing.T) {
	n := int32(-34)
	original := &CoreError{Status: 400, Code: -1, WeChatCode: &n, Category: "rate_limited", Operation: "synthetic", Restricted: true, Message: "synthetic rejection"}
	transported := status.Convert(original).Err()
	got, ok := AsCoreError(transported)
	if !ok || got.WeChatCode == nil || *got.WeChatCode != -34 || got.Operation != "synthetic" || !got.Restricted || status.Code(transported) != codes.ResourceExhausted {
		t.Fatal("lost plugin error metadata")
	}
	if _, ok = AsCoreError(status.Error(codes.Unknown, "unrelated")); ok {
		t.Fatal("invented core error")
	}
}
