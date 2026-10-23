// 文件名：foundation.go
// 主要功能：把 Dashboard 与 AI 注册到既有插件管理器，向其他插件提供异步 AI 任务能力。
// 作者：2guan
package plugin

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/we-clay/we-clay/plugins/ai"
	"github.com/we-clay/we-clay/plugins/dashboard"
	"github.com/we-clay/we-clay/plugins/toolbox"
	sdk "github.com/we-clay/we-clay/sdk/plugin"
)

// foundationPlugin 复用控制台的同一执行器，避免两套模型配置和用量统计。
type foundationPlugin struct {
	server *dashboard.Server
	name   string
}

func (p *foundationPlugin) GetMetadata() *sdk.Metadata {
	if d, ok := toolbox.Find(p.name); ok {
		return &sdk.Metadata{Name: d.ID, Author: "2guan", Version: "1.0.0", Description: d.Description}
	}
	return &sdk.Metadata{Name: p.name, Author: "2guan", Version: "builtin", Description: map[string]string{"dashboard": "Web 控制台", "ai": "多模态 AI 服务", "prompt_guard": "提示词防护", "context_cleaner": "上下文清洗"}[p.name]}
}
func (p *foundationPlugin) GetCapabilities() []string {
	if p.name == "news" {
		return []string{"news.today", "news.diagram"}
	}
	if p.name != "ai" {
		return nil
	}
	return []string{"ai.chat", "ai.vision", "ai.audio", "ai.video", "ai.tts", "ai.image", "ai.task"}
}
func (p *foundationPlugin) OnCall(capability string, args map[string]string) (string, []byte, error) {
	if p.name == "news" {
		return "none", nil, p.server.CallNews(capability, args["receiver"])
	}
	if capability == "ai.task" {
		t, err := p.server.Task(args["id"])
		if err != nil {
			return "", nil, err
		}
		data, err := json.Marshal(t)
		return "application/json", data, err
	}
	kind := strings.TrimPrefix(capability, "ai.")
	id, err := p.server.Submit(ai.Request{Kind: kind, Prompt: args["prompt"], System: args["system"], Model: args["model"], Media: args["media"], MIME: args["mime"], Filename: args["filename"]})
	if err != nil {
		return "", nil, err
	}
	data, err := json.Marshal(map[string]string{"id": id, "status": "queued"})
	return "application/json", data, err
}

// RegisterFoundation 仅由 Gateway 在管理器初始化后调用。内建插件不经外部子进程加载。
func RegisterFoundation(server *dashboard.Server, dashboardEnabled, aiEnabled bool) error {
	operations.Lock()
	defer operations.Unlock()
	names := []string{"dashboard", "ai", "prompt_guard", "context_cleaner"}
	for _, d := range toolbox.Catalog {
		names = append(names, d.ID)
	}
	for _, name := range names {
		if findPlugin(name) != nil {
			return fmt.Errorf("基础插件名称被占用：%s", name)
		}
		p := &foundationPlugin{server: server, name: name}
		base := sdk.Plugin(p)
		enabled := dashboardEnabled
		if name == "prompt_guard" || name == "context_cleaner" {
			enabled = true
		}
		if name == "ai" {
			enabled = aiEnabled
		}
		w := &wrapper{Metadata: p.GetMetadata(), Config: &Config{Enable: enabled, Mode: "blacklist"}, types: []string{"builtin"}, plugin: &base}
		if len(p.GetCapabilities()) > 0 {
			called := sdk.CalledPlugin(p)
			w.calledPlugin = &called
			w.capabilities = p.GetCapabilities()
			w.types = append(w.types, "called")
		}
		mu.Lock()
		plugins = append(plugins, w)
		sortPlugins()
		rebuildCommandIndex()
		rebuildCapabilityIndex()
		mu.Unlock()
	}
	return nil
}
