// 文件名：plugin_config.go
// 主要功能：统一插件可视化与 TOML 编辑，提供管理员校验、配置冲突保护及独立 Mock 持久化。
// 作者：2guan
package dashboard

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/we-clay/we-clay/sdk/control"
)

type pluginDocument struct {
	control.PluginDocument
	Groups []Group `json:"groups"`
}
type pluginConfigRequest struct {
	ID       string         `json:"id"`
	Text     *string        `json:"text,omitempty"`
	Values   map[string]any `json:"values,omitempty"`
	Revision string         `json:"revision"`
	Save     bool           `json:"save"`
}

// 配置内容只从管理员专用接口读取，不放入所有用户可见的 /api/state。
func (s *Server) pluginConfig(w http.ResponseWriter, r *http.Request, user User) {
	w.Header().Set("Cache-Control", "no-store")
	if err := requireAdmin(user); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	q := pluginConfigRequest{ID: r.URL.Query().Get("id")}
	if r.Method == http.MethodPost {
		if !decode(w, r, &q) {
			return
		}
	} else if r.Method != http.MethodGet {
		writeError(w, 405, "请求方法不支持")
		return
	}
	doc, err := s.readPluginDocument(r.Context(), q.ID)
	if err == nil && r.Method == http.MethodPost {
		if (q.Text == nil) == (q.Values == nil) {
			err = fmt.Errorf("请选择一种配置编辑方式")
		} else {
			var text []byte
			if q.Text != nil {
				text = []byte(*q.Text)
			} else {
				text, err = marshalPluginValues(q.Values, doc.Text)
			}
			if err == nil && len(text) > 256<<10 {
				err = fmt.Errorf("插件配置不能超过 256 KiB")
			}
			var values map[string]any
			if err == nil {
				err = toml.Unmarshal(text, &values)
				if err != nil {
					err = fmt.Errorf("TOML 语法错误，请检查引号、等号和配置段")
				}
			}
			if err == nil {
				values, err = jsonValues(values)
			}
			if err == nil && len(values) == 0 {
				err = fmt.Errorf("配置不能为空")
			}
			if err == nil && len(doc.Groups) > 0 {
				err = s.validatePluginSettings(doc.Groups, values)
			}
			if err == nil {
				doc.Text, doc.Values = string(text), values
				if q.Save {
					err = s.savePluginDocument(r.Context(), doc, q.Revision, user)
					if err == nil {
						doc, err = s.readPluginDocument(r.Context(), q.ID)
						s.notify()
					}
				}
			}
		}
	}
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, doc)
}

// JSON 没有整数类型；参照原 TOML 保留整数，避免可视化保存后插件的 int 字段收到浮点值。
func marshalPluginValues(values map[string]any, original string) ([]byte, error) {
	var template map[string]any
	if err := toml.Unmarshal([]byte(original), &template); err != nil {
		return nil, fmt.Errorf("原配置无法解析")
	}
	return toml.Marshal(pluginFormTypes(values, template))
}
func pluginFormTypes(value, template any) any {
	switch v := value.(type) {
	case map[string]any:
		old, _ := template.(map[string]any)
		next := map[string]any{}
		for k, item := range v {
			next[k] = pluginFormTypes(item, old[k])
		}
		return next
	case []any:
		old, _ := template.([]any)
		next := make([]any, len(v))
		for i, item := range v {
			var t any
			if i < len(old) {
				t = old[i]
			}
			next[i] = pluginFormTypes(item, t)
		}
		return next
	case float64:
		_, integer := template.(int64)
		if (integer || template == nil) && math.Trunc(v) == v && math.Abs(v) <= 9007199254740991 {
			return int64(v)
		}
	}
	return value
}

func jsonValues(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("配置值无法转换为表单数据")
	}
	var result map[string]any
	err = json.Unmarshal(raw, &result)
	return result, err
}
func pluginGroups(id string) []Group {
	groups := []Group{}
	for _, g := range Schema() {
		if id == "dashboard" && g.Area == "system" || id == "ai" && g.Area != "system" && g.Area != "plugins" || (isSafetyPlugin(id) || isBusinessPlugin(id)) && g.ID == id {
			groups = append(groups, g)
		}
	}
	return groups
}
func (s *Server) readPluginDocument(ctx context.Context, id string) (pluginDocument, error) {
	doc := pluginDocument{Groups: pluginGroups(id)}
	doc.ID = id
	if len(doc.Groups) > 0 {
		state := s.store.snapshot()
		doc.Values = map[string]any{}
		for _, g := range doc.Groups {
			values := map[string]any{}
			for _, f := range g.Fields {
				values[f.Key] = state.Settings[g.ID+"."+f.Key]
			}
			doc.Values[g.ID] = values
		}
		text, err := toml.Marshal(doc.Values)
		doc.Text = string(text)
		doc.Revision = strconv.FormatInt(state.ConfigRevision, 10)
		doc.Source = "控制台配置 · 数据库持久化"
		doc.Effect = "与系统设置、模型中心共用配置；监听地址等启动参数仍由部署配置管理。"
		return doc, err
	}
	if s.cfg.Mock {
		s.store.mu.Lock()
		defer s.store.mu.Unlock()
		text, err := s.mockPluginTextLocked(id)
		if err != nil {
			return doc, err
		}
		doc.Text = text
		doc.Revision = fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
		doc.Source = "Mock 插件配置 · 独立演示存储"
		doc.Effect = "保存后用于脱机演示，不执行真实插件或微信操作。"
		if err = toml.Unmarshal([]byte(text), &doc.Values); err != nil {
			return doc, err
		}
		doc.Values, err = jsonValues(doc.Values)
		return doc, err
	}
	if s.host == nil {
		return doc, fmt.Errorf("插件管理器尚未连接")
	}
	result, err := s.host.Execute(ctx, control.Command{Operation: "plugin-config-read", ID: id})
	if err != nil {
		return doc, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return doc, err
	}
	err = json.Unmarshal(raw, &doc.PluginDocument)
	return doc, err
}
func (s *Server) validatePluginSettings(groups []Group, values map[string]any) error {
	current := s.store.snapshot()
	for id, item := range values {
		var group *Group
		for i := range groups {
			if groups[i].ID == id {
				group = &groups[i]
				break
			}
		}
		if group == nil {
			return fmt.Errorf("此插件不包含配置组：%s", id)
		}
		fields, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("%s 必须是配置表", id)
		}
		if err := validateSettings(id, fields); err != nil {
			return err
		}
		for _, f := range group.Fields {
			if value, ok := fields[f.Key]; ok && !s.cfg.Mock && f.Preview && !reflect.DeepEqual(value, current.Settings[id+"."+f.Key]) {
				return fmt.Errorf("%s 当前仅提供 Mock 预览", f.Label)
			}
		}
	}
	return nil
}
func (s *Server) savePluginDocument(ctx context.Context, doc pluginDocument, revision string, user User) error {
	if len(doc.Groups) > 0 {
		return s.store.change(func(v *State) error {
			if revision != strconv.FormatInt(v.ConfigRevision, 10) {
				return fmt.Errorf("配置已发生变化，请重新读取后再保存")
			}
			before := map[string]any{}
			for group, item := range doc.Values {
				for key, value := range item.(map[string]any) {
					full := group + "." + key
					before[full] = v.Settings[full]
					v.Settings[full] = value
				}
			}
			raw, _ := json.Marshal(before)
			v.ConfigRevision++
			v.History = append(v.History, Change{randomID(), time.Now().Format(time.RFC3339), user.Name, doc.ID, "更新插件配置", raw})
			addLog(v, "INFO", "plugin", user.Name+" 更新插件配置："+doc.ID)
			return nil
		})
	}
	if s.cfg.Mock {
		return s.saveMockPlugin(doc, revision)
	}
	raw, _ := json.Marshal(map[string]string{"text": doc.Text, "revision": revision})
	_, err := s.host.Execute(ctx, control.Command{Operation: "plugin-config-save", ID: doc.ID, Params: raw})
	return err
}

// Mock 配置按插件 ID 存在独立文档中，凭据不会混入常规状态响应。
func (s *Server) mockPluginTextLocked(id string) (string, error) {
	var p *Plugin
	for i := range s.store.state.Plugins {
		if s.store.state.Plugins[i].ID == id && !s.store.state.Plugins[i].Builtin {
			p = &s.store.state.Plugins[i]
		}
	}
	if p == nil {
		return "", fmt.Errorf("插件不存在")
	}
	var text string
	err := s.store.db.QueryRow("SELECT body FROM documents WHERE id=?", "plugin-config:"+id).Scan(&text)
	if err == nil {
		return text, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	values := map[string]any{"enable": p.Enabled, "mode": "blacklist", "limits": []string{}, "priority": 100, "next": true, "always_run": false}
	if id == "welcome" {
		values["config"] = map[string]any{"message": "欢迎加入，一起轻松交流。", "mention": false, "delay_seconds": 2}
	} else {
		values["config"] = map[string]any{"title": "每日摘要", "time": "20:00", "max_messages": 200}
	}
	raw, err := toml.Marshal(values)
	return string(raw), err
}
func (s *Server) saveMockPlugin(doc pluginDocument, revision string) error {
	// 演示扩展遵循同样的宿主基础字段约束，自定义配置由各插件解释。
	var shape struct {
		Enable    bool           `toml:"enable"`
		Mode      string         `toml:"mode"`
		Limits    []string       `toml:"limits"`
		Priority  *int32         `toml:"priority"`
		Next      *bool          `toml:"next"`
		AlwaysRun *bool          `toml:"always_run"`
		Config    map[string]any `toml:"config"`
	}
	if err := toml.NewDecoder(bytes.NewBufferString(doc.Text)).DisallowUnknownFields().Decode(&shape); err != nil {
		return fmt.Errorf("TOML 配置格式或字段类型无效")
	}

	enabled, ok := doc.Values["enable"].(bool)
	if !ok {
		return fmt.Errorf("enable 必须是开关值")
	}
	mode := doc.Values["mode"]
	if mode != "blacklist" && mode != "whitelist" {
		return fmt.Errorf("mode 只能是 blacklist 或 whitelist")
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	current, err := s.mockPluginTextLocked(doc.ID)
	if err != nil {
		return err
	}
	if revision != fmt.Sprintf("%x", sha256.Sum256([]byte(current))) {
		return fmt.Errorf("插件配置已变化，请重新读取后再保存")
	}
	next := cloneState(s.store.state)
	for i := range next.Plugins {
		if next.Plugins[i].ID == doc.ID {
			next.Plugins[i].Enabled = enabled
		}
	}
	next.Revision++
	tx, err := s.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO documents(id,body) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", "plugin-config:"+doc.ID, doc.Text); err != nil {
		return err
	}
	if err = s.store.persistState(tx, next, s.store.state); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.store.state = next
	return nil
}

// 使用说明按目录动态提供，不写入账户状态或插件配置文档。
type pluginCatalogItem struct {
	Plugin
	Usage *control.PluginUsage `json:"usage,omitempty"`
}

func builtinPluginUsage(id string, mock bool) *control.PluginUsage {
	if isBusinessPlugin(id) {
		return businessUsage(id)
	}
	var steps []string
	switch id {
	case "prompt_guard":
		steps = []string{"默认检查 AI 请求中的指令覆盖和越权尝试，命中原因保存在处理记录中。", "配置支持观察、标准及严格模式；验证入口可离线测试规则及误判，不发送微信消息。", "自动参与 AI 处理，无需聊天触发词；不替代后端权限检查，也不能保证识别媒体中的全部攻击。"}
	case "context_cleaner":
		steps = []string{"默认整理模型上下文副本，保留原始聊天归档。", "条数与记忆期限继承现有对话设置；每个模型可覆盖上下文条数。", "验证入口展示处理前后差异；代码、报告和提示词编辑不应用聊天风格清理。"}
	case "dashboard":
		steps = []string{"从侧栏进入微信、通讯录、模型中心和智能洞察，管理会话与任务。", "在系统设置中调整品牌、界面和管理选项，也可通过插件的配置按钮编辑控制台配置。", "通过网页操作，无需输入聊天触发词。"}
	case "ai":
		steps = []string{"在模型中心添加模型厂商，配置各类模型流水线及默认人设；先在模型页测试，确认模型可以返回内容。", "启用 AI 插件，自动回复总开关默认开启；目标会话仍需设为自动，并通过黑白名单、私聊触发及静默时段等规则。已有手动关闭设置保留。", "群聊真实 @ 本人会触发回复，手写昵称不算；普通群消息按会话或全局概率处理。100% 时逐条排队，仍需满足自动模式和名单。", "语音回复需要配置 TTS 流水线，画图需要生图模型并允许相应能力；输入图片、语音或视频时，也需对应识别能力可用。", "自然语言标签仅为用法示例，没有统一固定聊天口令。关键词模式需命中你自己配置的词；手动回复、主动说一句和智能洞察从相应页面操作。模型全部失败后自动完整重试一轮，再失败可在微信页面点击重试。"}
	case "welcome", "digest":
		if mock {
			steps = []string{"这是脱机模式的演示插件，用于验证插件开关和配置编辑。", "点击配置可切换可视化或 TOML 编辑，保存后重新打开可检查持久化结果。", "演示模式不执行真实欢迎或定时摘要，也没有可发送的聊天触发词。"}
		}
	}
	if len(steps) == 0 {
		return nil
	}
	u := &control.PluginUsage{Steps: steps}
	if id == "ai" {
		u.Commands = []control.PluginCommandHelp{
			usageExamples("私聊自然对话", "会话需处于自动模式并通过触发规则；这些是普通消息示例，不是固定命令。", "你好，今天怎么样？", "帮我整理一下刚才讨论的待办"),
			usageExamples("群聊提问", "先通过微信选择真实 @ 本人再发送；复制标签不会自动创建 @。", "刚才大家讨论的结论是什么？", "帮我们想三个活动主题"),
			usageExamples("语音与图片", "需要已配置并授权相应生成能力，仍受会话策略限制。", "用语音说一句晚安", "画一只正在看书的小猫"),
		}
	}
	return u
}

func (s *Server) pluginList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "请求方法不支持")
		return
	}
	items := []pluginCatalogItem{}
	for _, p := range s.store.snapshot().Plugins {
		items = append(items, pluginCatalogItem{Plugin: p, Usage: builtinPluginUsage(p.ID, s.cfg.Mock)})
	}
	if !s.cfg.Mock && s.host != nil {
		result, err := s.host.Execute(r.Context(), control.Command{Operation: "plugin-list"})
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		raw, err := json.Marshal(result)
		if err != nil {
			writeError(w, 500, "插件目录读取失败")
			return
		}
		var extensions []pluginCatalogItem
		if err = json.Unmarshal(raw, &extensions); err != nil {
			writeError(w, 500, "插件目录读取失败")
			return
		}
		items = append(items, extensions...)
	}
	writeJSON(w, 200, items)
}
