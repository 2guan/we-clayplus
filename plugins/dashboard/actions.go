// 文件名：actions.go
// 主要功能：执行控制台写操作、配置版本检查、权限隔离和 Mock 场景切换。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
	"golang.org/x/crypto/bcrypt"
)

type actionRequest struct {
	Mentions  []string        `json:"mentions,omitempty"`
	RetryID   string          `json:"-"` // 仅内部重发链路赋值，不接受浏览器指定删除记录。
	StickerID string          `json:"sticker_id,omitempty"`
	QuoteID   string          `json:"quote_id,omitempty"`
	Vendor    *Vendor         `json:"vendor,omitempty"`
	Models    []Model         `json:"models,omitempty"`
	Pipeline  []string        `json:"pipeline,omitempty"`
	Policy    *BehaviorPolicy `json:"policy,omitempty"`
	Kind      string          `json:"kind"`
	ID        string          `json:"id,omitempty"`
	Target    string          `json:"target,omitempty"`
	Content   string          `json:"content,omitempty"`
	Group     string          `json:"group,omitempty"`
	Values    map[string]any  `json:"values,omitempty"`
	Revision  int64           `json:"revision,omitempty"`
	Enabled   bool            `json:"enabled,omitempty"`
	Model     *Model          `json:"model,omitempty"`
	Prompt    *Prompt         `json:"prompt,omitempty"`
	Name      string          `json:"name,omitempty"`
	Role      string          `json:"role,omitempty"`
	Password  string          `json:"password,omitempty"`
	Media     string          `json:"media,omitempty"`
	MIME      string          `json:"mime,omitempty"`
}

func (s *Server) action(w http.ResponseWriter, r *http.Request, u User) {
	var q actionRequest
	if !decode(w, r, &q) {
		return
	}
	var err error
	switch q.Kind {
	case "vendor", "delete-vendor", "pipeline", "behavior-policy", "default-prompt", "reference-image", "reference-audio", "reference-video":
		if err = requireAdmin(u); err == nil {
			err = s.modelAction(q)
		}

	case "send-voice":
		err = s.sendTaskVoice(r.Context(), q)
	case "retry-reply":
		err = s.retryReply(q.ID, q.Target)
	case "proactive-reply":
		err = s.proactiveReply(q.Target)
	case "resend":
		err = s.resend(r.Context(), q)
	case "send":
		err = s.send(r.Context(), q, "manual")
	case "delete-message":
		// 仅修改后台归档，不调用微信删除或撤回接口；发送中记录保留用于接收回执。
		err = s.store.change(func(v *State) error {
			index := slices.IndexFunc(v.Messages, func(m Message) bool { return m.ID == q.ID && m.Target == q.Target })
			if index < 0 {
				return fmt.Errorf("消息不存在")
			}
			if v.Messages[index].Status == "sending" {
				return fmt.Errorf("发送中的消息暂不能删除")
			}
			if _, e := s.store.db.Exec("INSERT OR IGNORE INTO memory_deleted_sources(account,target,message_id) VALUES(?,?,?)", v.Account.ID, q.Target, q.ID); e != nil {
				return e
			}
			v.Messages = slices.Delete(v.Messages, index, index+1)
			scrubQuotedMessage(v, q.Target, q.ID)
			if _, e := s.store.db.Exec("DELETE FROM memory_archive_events WHERE account=? AND target=? AND message_id=?", v.Account.ID, q.Target, q.ID); e != nil {
				return e
			}
			for i := range v.Contacts {
				if v.Contacts[i].ID == q.Target {
					v.Contacts[i].Preview = ""
					for j := len(v.Messages) - 1; j >= 0; j-- {
						if v.Messages[j].Target == q.Target {
							v.Contacts[i].Preview = v.Messages[j].Content
							break
						}
					}
				}
			}
			return nil
		})
	case "revoke", "forward", "remark", "group-name", "announcement":
		var revokeParams json.RawMessage
		// 先验证本地目标与回执，再进入真实写接口。未知消息绝不盲目撤回。
		state := s.store.snapshot()
		if !slices.ContainsFunc(state.Contacts, func(c Contact) bool { return c.ID == q.Target }) {
			err = fmt.Errorf("目标会话不存在")
			break
		}
		if q.Kind == "revoke" || q.Kind == "forward" {
			index := slices.IndexFunc(state.Messages, func(m Message) bool { return m.ID == q.ID && (q.Kind == "forward" || m.Target == q.Target) })
			if index < 0 {
				err = fmt.Errorf("消息不存在")
				break
			}
			m := state.Messages[index]
			if q.Kind == "revoke" && (!m.Self || m.Status != "sent") {
				err = fmt.Errorf("仅可撤回已确认发送的本账号消息")
				break
			}
			if q.Kind == "revoke" {
				if m.RevokeReceipt != nil {
					if m.RevokeReceipt.Account != state.Account.ID {
						err = fmt.Errorf("撤回回执与当前账号不匹配")
						break
					}
					revokeParams, _ = json.Marshal(m.RevokeReceipt)
				}
				// 以归档时间校验，客户端不能通过伪造时间绕过两分钟限制。
				sentAt, parseErr := time.Parse(time.RFC3339, m.Time)
				age := time.Since(sentAt)
				if parseErr != nil || age < 0 || age > 2*time.Minute {
					err = fmt.Errorf("消息已超过两分钟或发送时间无效，不能撤回")
					break
				}
			}
			if q.Kind == "forward" && s.cfg.Mock {
				err = s.send(r.Context(), actionRequest{Target: q.Target, Content: m.Content}, "manual")
				break
			}
		}
		if !s.cfg.Mock {
			if s.host == nil {
				err = fmt.Errorf("微信尚未就绪")
			} else {
				_, err = s.host.Execute(r.Context(), control.Command{Operation: q.Kind, ID: q.ID, Target: q.Target, Content: q.Content, Params: revokeParams})
			}
		}
		if err == nil {
			err = s.store.change(func(v *State) error {
				switch q.Kind {
				case "revoke":
					for i := range v.Messages {
						if v.Messages[i].ID == q.ID && v.Messages[i].Target == q.Target {
							if !v.Messages[i].Self {
								return fmt.Errorf("只能撤回本账号发送的消息")
							}
							v.Messages[i].Status = "revoked"
						}
					}
				case "remark", "group-name":
					for i := range v.Contacts {
						if v.Contacts[i].ID == q.Target {
							if q.Kind == "group-name" {
								v.Contacts[i].Name = q.Content
							} else {
								v.Contacts[i].Remark = q.Content
							}
						}
					}
				}
				addLog(v, "INFO", "wechat", "已执行 "+q.Kind)
				return nil
			})
		}
	case "settings":
		var group *Group
		for _, g := range Schema() {
			if g.ID == q.Group {
				copy := g
				group = &copy
				break
			}
		}
		if group == nil {
			err = fmt.Errorf("未知配置组")
			break
		}
		if group.Admin {
			err = requireAdmin(u)
		}
		if err == nil && !s.cfg.Mock {
			current := s.store.snapshot()
			for _, f := range group.Fields {
				if value, ok := q.Values[f.Key]; ok && f.Preview && value != current.Settings[q.Group+"."+f.Key] {
					err = fmt.Errorf("%s 当前仅提供 Mock 预览", f.Label)
				}
			}
		}
		if err == nil {
			err = validateSettings(q.Group, q.Values)
		}
		if err != nil {
			break
		}
		err = s.store.change(func(v *State) error {
			if q.Revision != v.ConfigRevision {
				return fmt.Errorf("配置已发生变化，请刷新后重试")
			}
			v.ConfigRevision++
			before := map[string]any{}
			for k, value := range q.Values {
				key := q.Group + "." + k
				before[key] = v.Settings[key]
				v.Settings[key] = value
			}
			raw, _ := json.Marshal(before)
			v.History = append(v.History, Change{randomID(), time.Now().Format(time.RFC3339), u.Name, q.Group, fmt.Sprintf("更新 %d 项设置", len(q.Values)), raw})
			addLog(v, "INFO", "settings", u.Name+" 更新了 "+group.Title)
			return nil
		})
	case "restore-settings":
		if err = requireAdmin(u); err != nil {
			break
		}
		err = s.store.change(func(v *State) error {
			for _, h := range v.History {
				if h.ID == q.ID {
					var old map[string]any
					if e := json.Unmarshal(h.Before, &old); e != nil {
						return e
					}
					v.ConfigRevision++
					for k, val := range old {
						v.Settings[k] = val
					}
					addLog(v, "INFO", "settings", "已恢复历史配置 "+h.Group)
					return nil
				}
			}
			return fmt.Errorf("配置版本不存在")
		})
	case "target":
		for k, val := range q.Values {
			switch k {
			case "prompt":
				// 中文 UTF-8 常占多个字节，提示词不应沿用模式与模型名称的短字段限制。
				// 与提示词优化入口保持 64 KB 容量；null 继续表示继承全局人设。
				if val != nil {
					if prompt, ok := val.(string); !ok {
						err = fmt.Errorf("专属提示词必须是文本")
					} else if len(prompt) > 64<<10 {
						err = fmt.Errorf("专属提示词过长，请控制在 64 KB 以内")
					}
				}
			case "probability":
				// 百分比允许显式为零；null 表示删除覆写并恢复全局策略。
				if val != nil {
					n, ok := val.(float64)
					if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 100 || math.Trunc(n) != n {
						err = fmt.Errorf("群回复概率必须为 0 至 100 的整数")
					}
				}
			case "mode", "model":
				if val != nil {
					if v, ok := val.(string); !ok || len(v) > 8000 {
						err = fmt.Errorf("会话设置格式无效")
					}
				}
			case "memory_enabled":
				if err = requireAdmin(u); err != nil {
					break
				}
				if _, ok := val.(bool); !ok {
					err = fmt.Errorf("会话记忆开关必须为布尔值")
				}
			case "pinned", "read":
				if _, ok := val.(bool); !ok {
					err = fmt.Errorf("会话设置格式无效")
				}
			default:
				err = fmt.Errorf("未知会话设置")
			}
		}
		if err != nil {
			break
		}
		if mode, ok := q.Values["mode"].(string); ok && !slices.Contains([]string{"auto", "manual", "paused"}, mode) {
			err = fmt.Errorf("会话模式无效")
			break
		}
		err = s.store.change(func(v *State) error {
			found := false
			for i := range v.Contacts {
				if v.Contacts[i].ID == q.Target {
					found = true
					if value, exists := q.Values["probability"]; exists && value != nil && v.Contacts[i].Kind != "group" {
						return fmt.Errorf("仅群聊支持单独设置回复概率")
					}
					if mode, ok := q.Values["mode"].(string); ok {
						v.Contacts[i].Mode = mode
					} else if mode, exists := q.Values["mode"]; exists && mode == nil {
						// 清除专属选择后重新跟随默认；不把当前有效模式固化到存储。
						v.Contacts[i].Mode = "paused"
					}
					if pin, ok := q.Values["pinned"].(bool); ok {
						if v.Contacts[i].WechatPinned != nil {
							return fmt.Errorf("置顶状态已跟随微信同步，请在微信中修改")
						}
						v.Contacts[i].Pinned = pin
					}
					if q.Values["read"] == true {
						v.Contacts[i].Unread = 0
					}
				}
			}
			if !found {
				return fmt.Errorf("会话不存在")
			}
			if v.Overrides[q.Target] == nil {
				v.Overrides[q.Target] = map[string]any{}
			}
			for k, val := range q.Values {
				if val == nil {
					delete(v.Overrides[q.Target], k)
				} else {
					v.Overrides[q.Target][k] = val
				}
			}
			return nil
		})
	case "clear-memory":
		err = s.store.change(func(v *State) error {
			if v.Overrides[q.Target] == nil {
				v.Overrides[q.Target] = map[string]any{}
			}
			v.Overrides[q.Target]["memory_since"] = time.Now().Format(time.RFC3339)
			addLog(v, "INFO", "ai", "重置近期上下文，保留聊天归档和长期记忆")
			return nil
		})
	case "plugin":
		if q.ID == "ai" && q.Enabled && s.cfg.DisableAI {
			err = fmt.Errorf("AI 已在启动配置中停用")
			break
		}
		if err = requireAdmin(u); err != nil {
			break
		}
		if q.ID != "ai" && q.ID != "dashboard" && !isSafetyPlugin(q.ID) && !isBusinessPlugin(q.ID) {
			var doc pluginDocument
			doc, err = s.readPluginDocument(r.Context(), q.ID)
			if err == nil {
				doc.Values["enable"] = q.Enabled
				var text []byte
				text, err = marshalPluginValues(doc.Values, doc.Text)
				if err == nil {
					doc.Text = string(text)
					err = s.savePluginDocument(r.Context(), doc, doc.Revision, u)
				}
			}
			break
		}
		err = s.store.change(func(v *State) error {
			for i := range v.Plugins {
				p := &v.Plugins[i]
				if p.ID == q.ID {
					if p.ID == "dashboard" {
						return fmt.Errorf("请通过部署配置关闭控制台")
					}
					if !s.cfg.Mock && p.ID != "ai" && !isSafetyPlugin(p.ID) && !isBusinessPlugin(p.ID) {
						return fmt.Errorf("扩展插件请通过现有插件管理命令配置")
					}
					p.Enabled = q.Enabled
					p.Status = "stopped"
					if p.Enabled {
						p.Status = "ready"
					}
					addLog(v, "INFO", "plugin", "更新插件状态："+p.Name)
					return nil
				}
			}
			return fmt.Errorf("插件不存在")
		})
	case "model":
		if err = requireAdmin(u); err != nil {
			break
		}
		if q.Model == nil || strings.TrimSpace(q.Model.Name) == "" || strings.TrimSpace(q.Model.Model) == "" || len(q.Model.Capabilities) == 0 {
			err = fmt.Errorf("请填写名称、模型 ID 并选择能力")
			break
		}
		m := *q.Model
		if m.VendorID != "" {
			vendors := s.store.snapshot().Vendors
			i := slices.IndexFunc(vendors, func(v Vendor) bool { return v.ID == m.VendorID })
			if i < 0 {
				err = fmt.Errorf("厂商不存在")
				break
			}
			v := vendors[i]
			m.Endpoint = v.Endpoint
			m.Provider = v.Provider
			m.Configured = v.Configured
			m.Key = ""
		}
		if err = validateModel(m); err != nil {
			break
		}
		if m.Provider != "mock" {
			endpoint, e := url.Parse(m.Endpoint)
			if e != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
				err = fmt.Errorf("请填写不含凭据或查询参数的 HTTP(S) Endpoint")
				break
			}
		}
		if !slices.Contains([]string{"openai", "gemini", "anthropic", "mimo"}, m.Provider) && !(s.cfg.Mock && m.Provider == "mock") {
			err = fmt.Errorf("模型协议无效")
			break
		}
		for _, cap := range m.Capabilities {
			if !slices.Contains([]string{"chat", "vision", "audio", "video", "tts", "image", "video_generation"}, cap) {
				err = fmt.Errorf("模型能力无效")
			}
		}
		if err != nil {
			break
		}
		if m.ID == "" {
			m.ID = randomID()
		}
		if m.Key != "" {
			if err = s.store.putSecret(m.ID, m.Key); err != nil {
				break
			}
			m.Configured = true
		}
		m.Key = ""
		legacyVendor := m.VendorID == ""
		if legacyVendor {
			m.VendorID = m.ID
		}
		err = s.store.change(func(v *State) error {
			if legacyVendor {
				vendor := Vendor{ID: m.ID, Name: m.Name, Provider: m.Provider, Endpoint: m.Endpoint, Configured: m.Configured}
				vi := slices.IndexFunc(v.Vendors, func(item Vendor) bool { return item.ID == m.ID })
				if vi < 0 {
					v.Vendors = append(v.Vendors, vendor)
				} else {
					vendor.Configured = vendor.Configured || v.Vendors[vi].Configured
					v.Vendors[vi] = vendor
				}
			}
			for i := range v.Models {
				if v.Models[i].ID == m.ID {
					m.Configured = m.Configured || v.Models[i].Configured
					v.Models[i] = m
					addLog(v, "INFO", "model", "模型配置已更新")
					return nil
				}
			}
			v.Models = append(v.Models, m)
			return nil
		})
	case "delete-model":
		state := s.store.snapshot()
		if slices.Contains(state.Pipeline, q.ID) {
			err = fmt.Errorf("请先从流水线移除此模型")
			break
		}
		for _, ids := range state.Pipelines {
			if slices.Contains(ids, q.ID) {
				err = fmt.Errorf("请先从流水线移除此模型")
			}
		}
		for k, v := range state.Settings {
			if strings.HasPrefix(k, "routing.") && v == q.ID {
				err = fmt.Errorf("请先解除默认模型引用")
			}
		}
		for _, v := range state.Overrides {
			if v["model"] == q.ID {
				err = fmt.Errorf("请先解除会话专属模型引用")
			}
		}
		if err != nil {
			break
		}
		if err = requireAdmin(u); err == nil {
			err = s.store.change(func(v *State) error {
				v.Models = slices.DeleteFunc(v.Models, func(m Model) bool { return m.ID == q.ID })
				return nil
			})
			if err == nil {
				if !slices.ContainsFunc(state.Vendors, func(v Vendor) bool { return v.ID == q.ID }) {
					_, err = s.store.db.Exec("DELETE FROM secrets WHERE id=?", q.ID)
				}
			}
		}
	case "prompt":
		if q.Prompt == nil || q.Prompt.Name == "" || q.Prompt.Content == "" {
			err = fmt.Errorf("请填写人设名称和提示词")
			break
		}
		p := *q.Prompt
		if p.ID == "" {
			p.ID = randomID()
		}
		err = s.store.change(func(v *State) error {
			for i := range v.Prompts {
				if v.Prompts[i].ID == p.ID {
					v.Prompts[i] = p
					return nil
				}
			}
			v.Prompts = append(v.Prompts, p)
			return nil
		})
	case "delete-prompt":
		err = s.store.change(func(v *State) error {
			v.Prompts = slices.DeleteFunc(v.Prompts, func(p Prompt) bool { return p.ID == q.ID })
			return nil
		})
	case "moment-refresh":
		if !s.cfg.Mock {
			_, err = s.host.Execute(r.Context(), control.Command{Operation: "moment-refresh", ID: q.ID})
		}
	case "moment-like", "moment-comment", "moment-post":
		state := s.store.snapshot()
		operation := q.Kind
		if q.Kind != "moment-post" {
			idx := slices.IndexFunc(state.Moments, func(m Moment) bool { return m.ID == q.ID })
			if idx < 0 {
				err = fmt.Errorf("动态不存在")
				break
			}
			if q.Kind == "moment-like" && state.Moments[idx].Liked {
				operation = "moment-unlike"
			}
		}
		if (q.Kind == "moment-comment" || q.Kind == "moment-post") && strings.TrimSpace(q.Content) == "" {
			err = fmt.Errorf("内容不能为空")
			break
		}
		if !s.cfg.Mock {
			if s.host == nil {
				err = fmt.Errorf("微信尚未就绪")
			} else {
				_, err = s.host.Execute(r.Context(), control.Command{Operation: operation, ID: q.ID, Content: q.Content})
			}
		}
		if err != nil {
			break
		}
		err = s.store.change(func(v *State) error {
			if q.Kind == "moment-post" {
				if strings.TrimSpace(q.Content) == "" {
					return fmt.Errorf("请输入动态内容")
				}
				v.Moments = append([]Moment{{AuthorID: v.Account.ID, ID: randomID(), Author: v.Account.Name, Avatar: v.Account.Avatar, Content: q.Content, Images: []string{}, Time: time.Now().Format(time.RFC3339), Comments: []Comment{}}}, v.Moments...)
				return nil
			}
			for i := range v.Moments {
				m := &v.Moments[i]
				if m.ID == q.ID {
					if q.Kind == "moment-like" {
						m.Liked = !m.Liked
						if m.Liked {
							m.Likes++
						} else {
							m.Likes--
						}
					} else {
						if strings.TrimSpace(q.Content) == "" {
							return fmt.Errorf("请输入评论内容")
						}
						m.Comments = append(m.Comments, Comment{v.Account.Name, q.Content})
					}
					return nil
				}
			}
			return fmt.Errorf("动态不存在")
		})
	case "user":
		err = s.editUser(q, u)
	case "password":
		if len(q.Password) < 6 || len(q.Password) > 72 {
			err = fmt.Errorf("新密码需为 6–72 字节")
			break
		}
		if s.cfg.Mock {
			break
		}
		var hash string
		err = s.store.db.QueryRow("SELECT password FROM users WHERE id=?", u.ID).Scan(&hash)
		if err == nil && bcrypt.CompareHashAndPassword([]byte(hash), []byte(q.Content)) != nil {
			err = fmt.Errorf("原密码不正确")
		}
		if err == nil {
			newHash, e := bcrypt.GenerateFromPassword([]byte(q.Password), bcrypt.DefaultCost)
			err = e
			if err == nil {
				_, err = s.store.db.Exec("UPDATE users SET password=? WHERE id=?", string(newHash), u.ID)
			}
		}
		if err == nil {
			s.authMu.Lock()
			for k, v := range s.sessions {
				if v.UserID == u.ID {
					delete(s.sessions, k)
				}
			}
			s.authMu.Unlock()
		}
	case "delete-report":
		err = s.deleteReport(q.ID)
	case "cancel-task":
		s.taskMu.Lock()
		if cancel := s.taskCancel[q.ID]; cancel != nil {
			cancel()
		}
		s.taskMu.Unlock()
		err = s.store.change(func(v *State) error {
			for i := range v.Tasks {
				if v.Tasks[i].ID == q.ID && (v.Tasks[i].Status == "queued" || v.Tasks[i].Status == "running") {
					v.Tasks[i].Status = "cancelled"
				}
			}
			return nil
		})
	case "mock-scenario", "mock-reset":
		if !s.cfg.Mock {
			err = fmt.Errorf("真实模式不能调用演示接口")
			break
		}
		if e := s.resetBusiness(); e != nil {
			err = e
			break
		}
		s.taskMu.Lock()
		for _, cancel := range s.taskCancel {
			cancel()
		}
		s.taskMu.Unlock()
		err = s.store.change(func(v *State) error {
			revision, configRevision := v.Revision, v.ConfigRevision
			if q.Kind == "mock-reset" {
				// 重置演示时同时清除独立资料缓存和快照，避免下一轮演示复用旧数据。
				if _, e := s.store.db.Exec("DELETE FROM secrets WHERE id LIKE 'report-source:%' OR id LIKE 'insight-cache:%' OR id LIKE 'insight-image:%'"); e != nil {
					return e
				}
				// 重置演示时同时清除独立保存的插件草稿配置。
				if _, e := s.store.db.Exec("DELETE FROM documents WHERE id LIKE 'plugin-config:%'"); e != nil {
					return e
				}
				*v = initialState(true)
				v.Revision = revision
				v.ConfigRevision = configRevision + 1
				return nil
			}
			if !slices.Contains([]string{"normal", "empty", "offline", "error"}, q.Content) {
				return fmt.Errorf("场景不存在")
			}
			if _, e := s.store.db.Exec("DELETE FROM documents WHERE id LIKE 'plugin-config:%'"); e != nil {
				return e
			}
			*v = initialState(true)
			v.Revision = revision
			v.ConfigRevision = configRevision + 1
			v.Scenario = q.Content
			switch q.Content {
			case "empty":
				v.Messages = []Message{}
				v.Contacts = []Contact{}
				v.Moments = []Moment{}
				v.Trend = []Point{}
				v.Tasks = []Task{}
			case "offline":
				v.Account.Status = "offline"
			case "error":
				addLog(v, "ERROR", "model", "模拟模型超时，请在任务列表验证错误状态")
			}
			return nil
		})
		// Mock 重置会替换整个状态，必须像启动时一样恢复默认行为策略。
		if err == nil {
			err = s.initBehaviors()
		}
	case "refresh":
		if !s.cfg.Mock && s.host != nil {
			_, err = s.host.Execute(r.Context(), control.Command{Operation: "refresh"})
		}
	default:
		err = fmt.Errorf("不支持此操作")
	}
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	s.notify()
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) editUser(q actionRequest, actor User) error {
	if err := requireAdmin(actor); err != nil {
		return err
	}
	if !slices.Contains([]string{"admin", "operator"}, q.Role) {
		return fmt.Errorf("角色无效")
	}
	if strings.TrimSpace(q.Name) == "" || len(q.Name) > 64 {
		return fmt.Errorf("请输入登录名")
	}
	if q.ID == actor.ID && (!q.Enabled || q.Role != "admin") {
		return fmt.Errorf("不能停用或降级当前管理员")
	}
	if q.ID != "" {
		var count int
		if err := s.store.db.QueryRow("SELECT count(*) FROM users WHERE role='admin' AND enabled=1 AND id!=?", q.ID).Scan(&count); err != nil {
			return err
		}
		var oldRole string
		_ = s.store.db.QueryRow("SELECT role FROM users WHERE id=?", q.ID).Scan(&oldRole)
		if oldRole == "admin" && count == 0 && (!q.Enabled || q.Role != "admin") {
			return fmt.Errorf("必须保留至少一个启用的管理员")
		}
	}
	hash := ""
	if q.Password != "" {
		if len(q.Password) < 6 || len(q.Password) > 72 {
			return fmt.Errorf("密码需为 6–72 字节")
		}
		b, e := bcrypt.GenerateFromPassword([]byte(q.Password), bcrypt.DefaultCost)
		if e != nil {
			return e
		}
		hash = string(b)
	}
	if q.ID == "" {
		if hash == "" {
			return fmt.Errorf("新用户必须设置密码")
		}
		_, err := s.store.db.Exec("INSERT INTO users VALUES(?,?,?,?,?,?)", randomID(), q.Name, q.Name, q.Role, q.Enabled, hash)
		if err != nil {
			return fmt.Errorf("创建用户失败，登录名可能已存在")
		}
		return nil
	}
	_, err := s.store.db.Exec("UPDATE users SET name=?,role=?,enabled=?,password=CASE WHEN ?='' THEN password ELSE ? END WHERE id=?", q.Name, q.Role, q.Enabled, hash, hash, q.ID)
	if err == nil {
		s.authMu.Lock()
		for token, v := range s.sessions {
			if v.UserID == q.ID {
				delete(s.sessions, token)
			}
		}
		s.authMu.Unlock()
	}
	return err
}
func (s *Server) send(ctx context.Context, q actionRequest, source string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// @ 成员必须显式携带 ID，不能从同名显示文本反推，也不允许注入全体提醒。
	if len(q.Mentions) > 50 {
		return fmt.Errorf("一次最多提醒 50 位群成员")
	}
	if len(q.Mentions) > 0 {
		if !strings.HasSuffix(q.Target, "@chatroom") || (q.Group != "" && q.Group != "text") || q.Media != "" || q.StickerID != "" {
			return fmt.Errorf("仅群聊文字支持 @ 成员")
		}
		seen := map[string]bool{}
		ids := []string{}
		for _, id := range q.Mentions {
			if id == "" || len(id) > 256 || strings.ContainsAny(id, ",&<>\r\n\t ") || id == "notify@all" {
				return fmt.Errorf("@ 成员 ID 无效")
			}
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
		q.Mentions = ids
	}
	var sticker *Sticker
	if q.StickerID != "" {
		v, e := s.sticker(q.StickerID, source == "ai")
		if e != nil {
			return e
		}
		sticker = &v
		if source == "ai" && s.lastSticker(q.Target) == v.ID {
			return fmt.Errorf("不能连续重复发送同一表情")
		}
		if q.QuoteID != "" || q.Media != "" {
			return fmt.Errorf("表情不能混合引用或其他附件")
		}
		q.Group = "emoji"
		q.Content = "[表情：" + v.Name + " " + v.Meaning + "]"
		if v.Local != "" {
			data, mime, e := s.replyMediaBytes(v.Local)
			if e != nil {
				return e
			}
			q.Media = base64.StdEncoding.EncodeToString(data)
			q.MIME = mime
		}
	}
	if q.Group == "emoji" && sticker == nil {
		return fmt.Errorf("请从表情库选择表情")
	}
	if strings.TrimSpace(q.Content) == "" && q.Media == "" {
		return fmt.Errorf("消息不能为空")
	}
	if len(q.Content) > 16000 {
		return fmt.Errorf("消息内容过长")
	}
	state := s.store.snapshot()
	if state.Account.Status != "online" {
		return fmt.Errorf("微信当前离线")
	}
	if !slices.ContainsFunc(state.Contacts, func(c Contact) bool { return c.ID == q.Target }) {
		return fmt.Errorf("目标会话不存在")
	}
	kind := q.Group
	if kind == "" {
		kind = "text"
	}
	var quoteXML string
	var rich *RichMessage
	if sticker != nil {
		rich = &RichMessage{StickerID: sticker.ID, Sticker: sticker.Kind, Title: sticker.Name, Image: sticker.Image}
	}
	if q.QuoteID != "" {
		if kind != "text" || q.Media != "" {
			return fmt.Errorf("引用回复只能发送文字正文")
		}
		var e error
		quoteXML, rich, e = s.buildQuote(state, q.Target, q.QuoteID, q.Content)
		if e != nil {
			return e
		}
		kind = "quote"
	}
	media := ""
	if q.Media != "" {
		data, e := base64.StdEncoding.DecodeString(q.Media)
		if e != nil || len(data) > 16<<20 {
			return fmt.Errorf("附件无效或超过 16 MB")
		}
		media, e = s.saveMedia(data, q.MIME)
		if e != nil {
			return e
		}
	}
	m := Message{Mentions: q.Mentions, ID: randomID(), Target: q.Target, Sender: state.Account.Name, Content: q.Content, Kind: kind, Rich: rich, Self: true, Source: source, Status: "sending", Time: time.Now().Format(time.RFC3339), Media: media}
	if err := s.Record(m); err != nil {
		return err
	}
	var sendErr error
	serverID := ""
	var revokeReceipt *control.RevokeReceipt
	var duration float64
	if !s.cfg.Mock {
		if s.host == nil {
			sendErr = fmt.Errorf("微信尚未就绪")
		} else {
			params, _ := json.Marshal(map[string]any{"media": q.Media, "mime": q.MIME, "kind": kind, "quote_xml": quoteXML, "emoji_md5": q.StickerID, "mentions": q.Mentions})
			result, e := s.host.Execute(ctx, control.Command{Operation: "send", Target: q.Target, Content: q.Content, Params: params})
			sendErr = e
			if result != nil {
				if receipt, ok := result.(map[string]string); ok {
					serverID = receipt["id"]
					if (receipt["client_id"] != "" || receipt["original_client_id"] != "") && receipt["create_time"] != "" && receipt["account"] == state.Account.ID {
						revokeReceipt = &control.RevokeReceipt{Account: receipt["account"], ClientID: receipt["client_id"], OriginalClientID: receipt["original_client_id"], Created: receipt["create_time"]}
					}
					duration, _ = strconv.ParseFloat(receipt["duration"], 64)
				}
			}
		}
	} else if state.Scenario == "error" {
		sendErr = fmt.Errorf("模拟发送失败：网络连接中断")
	}
	// 真实发送没有服务端消息 ID 时不能视为成功，也不能删除重发前的记录。
	if !s.cfg.Mock && sendErr == nil && serverID == "" {
		sendErr = fmt.Errorf("未收到有效发送回执，请核对微信")
	}
	err := s.store.change(func(v *State) error {
		if q.RetryID != "" && sendErr == nil {
			v.Messages = slices.DeleteFunc(v.Messages, func(item Message) bool {
				return item.Target == q.Target && item.ID == q.RetryID && item.Self && (item.Status == "unknown" || item.Status == "failed")
			})
		}
		// core 可能在发送 RPC 返回前推送本账号的同步消息，按正式 ID 合并这条回显。
		if serverID != "" && serverID != m.ID {
			v.Messages = slices.DeleteFunc(v.Messages, func(item Message) bool { return item.ID == serverID && item.Target == q.Target })
		}
		for i := range v.Messages {
			if v.Messages[i].ID == m.ID {
				v.Messages[i].Status = "sent"
				v.Messages[i].RevokeReceipt = revokeReceipt
				v.Messages[i].Duration = int(math.Ceil(duration))
				if sendErr != nil {
					v.Messages[i].Status = "unknown"
				}
				if serverID != "" {
					v.Messages[i].ID = serverID
				}
			}
		}
		// 发送来源只标记本条消息，不改变会话的手动/自动模式或默认值继承关系。
		if !v.Mock {
			updateMessagePoint(v, m.Time)
		}
		addLog(v, "INFO", "message", "发送请求已处理，来源："+source)
		return nil
	})
	s.notify()
	if err != nil {
		return err
	}
	return sendErr
}
