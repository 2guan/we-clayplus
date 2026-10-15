// 文件名：console_config.go
// 主要功能：向控制台提供已加载扩展插件的配置读取、校验和原子保存。
// 作者：2guan
package plugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/we-clay/we-clay/sdk/control"
)

// ConsolePlugins 返回已加载扩展，基础服务由控制台自身提供，内部管理命令不作为可配置插件。
func ConsolePlugins() []control.PluginInfo {
	mu.Lock()
	defer mu.Unlock()
	result := []control.PluginInfo{}
	// 开关显示已经保存的期望状态，避免监听器重载前的短暂延迟让开关跳回旧值。
	saved, _ := readPluginConfigFile()
	for _, w := range plugins {
		if slices.Contains(w.types, "builtin") {
			continue
		}
		status := "stopped"
		if w.Enable {
			status = "ready"
		}
		enabled := w.Enable
		if cfg := saved[w.Name]; cfg != nil {
			enabled = cfg.Enable
		}
		result = append(result, control.PluginInfo{ID: w.Name, Name: w.Name, Description: w.Description, Version: w.Version, Enabled: enabled, Status: status, Abilities: []string{}, Usage: consolePluginUsage(w)})
	}
	return result
}

// 控制台读取注册时缓存的命令说明，无需调用插件或连接微信。
func consolePluginUsage(w *wrapper) *control.PluginUsage {
	usage := &control.PluginUsage{Steps: []string{"在配置中启用插件，并检查黑白名单和适用对象。"}}
	for _, schema := range w.commandSchemas {
		if schema == nil || schema.GetMain() == "" {
			continue
		}
		name := strings.TrimSpace("/" + schema.GetMain() + " " + schema.GetSub())
		command := control.PluginCommandHelp{Name: name, Description: schema.GetDescription(), Syntax: schema.GetUsage()}
		// 保留示例原文，不添加前缀、不替换参数；空示例不生成复制按钮。
		for _, example := range schema.GetExamples() {
			if strings.TrimSpace(example) != "" {
				command.Examples = append(command.Examples, example)
			}
		}
		usage.Commands = append(usage.Commands, command)
	}
	if len(usage.Commands) > 0 {
		usage.Steps = append(usage.Steps, "在允许的微信会话中按下方命令用法发送消息，具体参数以插件声明为准。")
	} else if len(w.commands) > 0 {
		usage.Steps = append(usage.Steps, "已注册命令："+strings.Join(w.commands, "、")+"。插件未提供详细命令说明，请查阅插件文档确认用法。")
	} else {
		usage.Steps = append(usage.Steps, "此插件未声明聊天命令；具体事件触发条件或调用入口请查阅插件文档。")
	}
	return usage
}

// ConsoleConfiguration 的路径来自宿主配置，绝不采用浏览器传入的文件名。
func ConsoleConfiguration(name string, content *string, revision string) (control.PluginDocument, error) {
	mu.Lock()
	defer mu.Unlock()
	var result control.PluginDocument
	w := findPlugin(name)
	if w == nil || slices.Contains(w.types, "builtin") {
		return result, fmt.Errorf("未找到可配置的扩展插件")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return result, fmt.Errorf("读取插件配置失败")
	}
	currentRevision := fmt.Sprintf("%x", sha256.Sum256(raw))
	if content != nil {
		if revision != currentRevision {
			return result, fmt.Errorf("插件配置已变化，请重新读取后再保存")
		}
		if len(*content) > 256<<10 {
			return result, fmt.Errorf("插件配置不能超过 256 KiB")
		}
		var cfg Config
		if err := toml.NewDecoder(bytes.NewBufferString(*content)).DisallowUnknownFields().Decode(&cfg); err != nil {
			return result, fmt.Errorf("TOML 配置格式或字段类型无效")
		}
		if cfg.Mode != "blacklist" && cfg.Mode != "whitelist" {
			return result, fmt.Errorf("mode 只能是 blacklist 或 whitelist")
		}
		if cfg.Config != nil && !slices.Contains(w.abilities, "config") {
			return result, fmt.Errorf("此插件未声明自定义配置能力")
		}
		// 只替换选中插件的配置段，保留文件内其他插件和未加载插件的设置。
		next := map[string]any{}
		if err = toml.Unmarshal(raw, &next); err != nil {
			return result, fmt.Errorf("现有配置文件无法解析，未覆盖文件")
		}
		next[name] = &cfg
		encoded, err := toml.Marshal(next)
		if err != nil {
			return result, fmt.Errorf("配置无法序列化")
		}
		// 同目录临时文件加原子替换，避免监听器读到只写了一半的 TOML。
		file, err := os.CreateTemp(filepath.Dir(configPath), ".console-config-*")
		if err != nil {
			return result, fmt.Errorf("创建配置临时文件失败")
		}
		defer os.Remove(file.Name())
		if _, err = file.Write(encoded); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			return result, fmt.Errorf("写入配置失败，原文件未修改")
		}
		if err = os.Rename(file.Name(), configPath); err != nil {
			return result, fmt.Errorf("替换配置文件失败")
		}
		raw = encoded
		currentRevision = fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	// 优先返回落盘值；插件运行时仍由既有监听器注入，注入失败会记入实时日志。
	saved := map[string]*Config{}
	if err = toml.Unmarshal(raw, &saved); err != nil {
		return result, fmt.Errorf("配置文件无法解析")
	}
	cfg := saved[name]
	if cfg == nil {
		cfg = w.Config
	}
	text, err := toml.Marshal(cfg)
	if err != nil {
		return result, fmt.Errorf("读取插件配置失败")
	}
	values := map[string]any{}
	if err = toml.Unmarshal(text, &values); err != nil {
		return result, err
	}
	// 未显式覆盖的调度参数展示运行中的默认值，首次编辑也可直接用表单调整。
	if _, ok := values["priority"]; !ok {
		values["priority"] = w.Metadata.Priority
	}
	if _, ok := values["next"]; !ok {
		values["next"] = w.Metadata.Next
	}
	if _, ok := values["always_run"]; !ok {
		values["always_run"] = w.Metadata.AlwaysRun
	}
	if _, ok := values["limits"]; !ok {
		values["limits"] = []string{}
	}
	text, err = toml.Marshal(values)
	if err != nil {
		return result, err
	}
	// 统一 JSON 数字类型，前端和内置表单使用相同的数据形状。
	normalized, err := json.Marshal(values)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(normalized, &values); err != nil {
		return result, err
	}
	return control.PluginDocument{ID: name, Text: string(text), Values: values, Revision: currentRevision, Source: "plugins/config.toml · [" + name + "]", Effect: "保存后由配置监听器重载；运行结果见实时日志。"}, nil
}
