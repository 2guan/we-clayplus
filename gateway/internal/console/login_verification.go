package console

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/we-clay/we-clay/gateway/internal/api"
)

// loginVerification handles only the fixed core login operations. The
// verification URL comes from the current core session, never from a browser.
// Independent core instances use their lifecycle controller instead.
func loginVerification(ctx context.Context, operation string, params json.RawMessage) (any, error) {
	client := api.GetCore()
	var view struct {
		Online      bool   `json:"online"`
		Phase       string `json:"phase"`
		VerifyURL   string `json:"verify_url"`
		PINRequired bool   `json:"pin_required"`
	}
	if err := client.Get("/api/login/view").WithContext(ctx).DoJson(&view); err != nil {
		return nil, errors.New("暂时无法核实登录验证状态")
	}
	if view.Online || view.Phase != "verification_required" {
		return nil, errors.New("当前没有待处理的登录验证")
	}
	switch operation {
	case "login-verify-code":
		var request struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(params, &request) != nil || len(request.Code) == 0 || len(request.Code) > 32 || strings.ContainsAny(request.Code, "\r\n") {
			return nil, errors.New("验证码格式无效")
		}
		if !view.PINRequired {
			return nil, errors.New("当前验证不需要输入验证码")
		}
		var result json.RawMessage
		if err := client.Post("/api/login/verification-code").WithContext(ctx).Body(map[string]string{"code": request.Code}).DoJson(&result); err != nil {
			return nil, errors.New("验证码提交失败，请查看本机 core.log")
		}
	case "login-device-confirm":
		if view.VerifyURL == "" || view.PINRequired {
			return nil, errors.New("当前验证不支持设备确认")
		}
		for _, path := range []string{"/api/login/device-confirm/get", "/api/login/device-confirm/ok"} {
			var result json.RawMessage
			if err := client.Post(path).WithContext(ctx).Body(map[string]string{"url": view.VerifyURL}).DoJson(&result); err != nil {
				return nil, errors.New("设备确认失败，请查看本机 core.log")
			}
		}
	default:
		return nil, errors.New("不支持的登录验证操作")
	}
	return map[string]bool{"submitted": true}, nil
}
