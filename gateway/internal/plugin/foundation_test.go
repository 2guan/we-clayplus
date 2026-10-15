// 文件名：foundation_test.go
// 主要功能：验证基础插件默认注册、SDK 调用的异步任务契约和停用边界。
// 作者：2guan
package plugin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/sdk/control"
	sdk "github.com/we-clay/we-clay/sdk/plugin"
)

func TestFoundationCapabilities(t *testing.T) {
	withPluginState(t)
	server, err := dashboard.Start(context.Background(), control.Config{Mock: true, Disabled: true, DataDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err = RegisterFoundation(server, true, true); err != nil {
		t.Fatal(err)
	}
	if findPlugin("dashboard") == nil || findPlugin("ai") == nil || findPlugin("prompt_guard") == nil || findPlugin("context_cleaner") == nil {
		t.Fatal("缺少默认基础插件")
	}
	response, err := (&gatewayService{}).CallPlugin(context.Background(), &sdk.CallPlugin_Request{Capability: "ai.chat", Args: []byte(`{"prompt":"脱机任务"}`)})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(response.GetData(), &result); err != nil || result.ID == "" {
		t.Fatalf("异步任务响应无效：%v", err)
	}
	task, err := server.Task(result.ID)
	if err != nil || task.Target != "" {
		t.Fatal("插件调用不应隐式发送微信消息")
	}
}
