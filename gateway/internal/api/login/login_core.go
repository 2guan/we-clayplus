// Package loginapi 提供登录服务的远程实现（通过 gRPC 调用 core）。
package loginapi

import (
	"encoding/json"
	"sync"

	"github.com/we-clay/we-clay/gateway/internal/api"
	baseapi "github.com/we-clay/we-clay/gateway/internal/api/base"
)

// remote 登录服务 gRPC 实现
type remote struct{}

// Get 获取 LoginService 单例（gRPC 模式）
var Get = sync.OnceValue(func() LoginService {
	return &remote{}
})

// Login 执行扫码登录
func (w remote) Login() (*QRCodeResult, error) {
	var resp struct {
		UUID        string `json:"uuid"`
		Data        string `json:"data"`
		CheckTime   uint32 `json:"check_time"`
		ExpiredTime uint32 `json:"expired_time"`
	}
	if err := api.GetCore().Get("/api/login/login").DoJson(&resp); err != nil {
		return nil, err
	}
	return &QRCodeResult{
		Qrcode:      &baseapi.Buffer{Data: []byte(resp.Data)},
		Uuid:        &resp.UUID,
		CheckTime:   &resp.CheckTime,
		ExpiredTime: &resp.ExpiredTime,
	}, nil
}

// Init 首次登录后初始化
func (w remote) Init() (*InitResponse, error) {
	if _, err := api.GetCore().Get("/api/login/init").Do(); err != nil {
		return nil, err
	}
	return &InitResponse{Code: 0, Message: "ok"}, nil
}

// Refresh 刷新登录状态
func (w remote) Refresh() (*OperateResponse, error) {
	var state json.RawMessage
	if err := api.GetCore().Get("/api/login/status").DoJson(&state); err != nil {
		return nil, err
	}
	return &OperateResponse{Code: 0, Message: string(state)}, nil
}

// Wakeup 唤醒登录
func (w remote) Wakeup() (*OperateResponse, error) {
	if _, err := api.GetCore().Get("/api/login/awaken").Do(); err != nil {
		return nil, err
	}
	return &OperateResponse{Code: 0, Message: "ok"}, nil
}

// Logout 登出
func (w remote) Logout() (*OperateResponse, error) {
	if _, err := api.GetCore().Get("/api/login/logout").Do(); err != nil {
		return nil, err
	}
	return &OperateResponse{Code: 0, Message: "ok"}, nil
}

// PasswordLogin 使用账号密码登录
func (w remote) PasswordLogin(req *PasswordLoginRequest) (*PasswordLoginResult, error) {
	var resp PasswordLoginResult
	if err := api.GetCore().Post("/api/login/password").Body(map[string]any{
		"username":    req.GetUsername(),
		"password":    req.GetPassword(),
		"device_type": req.GetDeviceType(),
		"device_name": req.GetDeviceName(),
		"device_id":   req.GetDeviceId(),
	}).DoProto(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
