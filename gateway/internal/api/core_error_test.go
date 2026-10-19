package api

import (
	"errors"
	"github.com/we-clay/we-clay/sdk"
	"testing"
)

func TestStructuredCoreErrorAndLegacyEnvelope(t *testing.T) {
	_, err := decodeResponse(400, "application/json", []byte(`{"code":-1,"message":"synthetic rejection","wechat_code":-106,"category":"restricted","operation":"synthetic","restricted":true}`))
	var e *sdk.CoreError
	if !errors.As(err, &e) || e.WeChatCode == nil || *e.WeChatCode != -106 || e.Category != "restricted" || !e.Restricted {
		t.Fatal("lost core error fields", err)
	}
	_, err = decodeResponse(400, "application/json", []byte(`{"code":-1,"message":"legacy failure"}`))
	if !errors.As(err, &e) || e.WeChatCode != nil {
		t.Fatal("invented legacy WeChat code", err)
	}
}
