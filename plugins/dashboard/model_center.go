// 文件名：model_center.go
// 主要功能：维护厂商、模型流水线、人设默认项及垫图，统一校验模型参数与凭据边界。
// 作者：2guan
package dashboard

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/we-clay/we-clay/plugins/ai"
)

func validateModel(m Model) error {
	if m.ContextTokens < 0 || m.ContextTokens > 2000000 {
		return fmt.Errorf("上下文预算应为 0–2000000 Token；0 继承全局")
	}
	if m.Timeout < 0 || m.Timeout > 600 || m.Context < 0 || m.Context > 500 || m.MaxOutput < 0 || m.MaxOutput > 32768 {
		return fmt.Errorf("超时应为 0–600 秒，上下文 0–500 条，最大输出 0–32768；0 继承全局")
	}
	if m.Temperature != nil && (*m.Temperature < 0 || *m.Temperature > 2) {
		return fmt.Errorf("温度应为 0–2")
	}
	if !slices.Contains([]string{"", "inherit", "on", "off"}, m.Thinking) {
		return fmt.Errorf("思考模式无效")
	}
	return nil
}
func validateVendor(v Vendor, mock bool) error {
	if strings.TrimSpace(v.Name) == "" {
		return fmt.Errorf("请输入厂商名称")
	}
	if !slices.Contains([]string{"openai", "mimo", "gemini", "anthropic"}, v.Provider) && !(mock && v.Provider == "mock") {
		return fmt.Errorf("厂商协议无效")
	}
	if mock && v.Provider == "mock" {
		return nil
	}
	u, err := url.Parse(v.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("请输入不含凭据或查询参数的 HTTP(S) API 地址")
	}
	return nil
}
func (s *Server) discoverModels(w http.ResponseWriter, r *http.Request, u User) {
	if requireAdmin(u) != nil {
		writeError(w, 403, "仅管理员可以管理厂商")
		return
	}
	var v Vendor
	if !decode(w, r, &v) {
		return
	}
	if err := validateVendor(v, s.cfg.Mock); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if v.Key == "" && v.ID != "" {
		// 只有当前存储的地址可复用已有密钥，防止编辑地址后无意把旧密钥发送到其他主机。
		for _, saved := range s.store.snapshot().Vendors {
			if saved.ID == v.ID && saved.Endpoint == v.Endpoint && saved.Provider == v.Provider {
				v.Key, _ = s.store.getSecret(v.ID)
			}
		}
	}
	models, err := ai.Discover(r.Context(), ai.Config{Endpoint: v.Endpoint, Key: v.Key, Provider: v.Provider}, s.cfg.Mock)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"models": models, "mock": s.cfg.Mock})
}
func (s *Server) modelAction(q actionRequest) error {
	switch q.Kind {
	case "vendor":
		if q.Vendor == nil {
			return fmt.Errorf("缺少厂商配置")
		}
		v := *q.Vendor
		if err := validateVendor(v, s.cfg.Mock); err != nil {
			return err
		}
		if v.ID == "" {
			v.ID = randomID()
		}
		snapshot := s.store.snapshot()
		for _, old := range snapshot.Vendors {
			if old.ID == v.ID {
				if (old.Endpoint != v.Endpoint || old.Provider != v.Provider) && old.Configured && v.Key == "" {
					return fmt.Errorf("修改 API 地址或协议时请重新输入密钥")
				}
				v.Configured = old.Configured
			}
		}
		models := q.Models
		for i := range models {
			m := &models[i]
			if strings.TrimSpace(m.Model) == "" || len(m.Capabilities) == 0 {
				return fmt.Errorf("请选择模型及能力")
			}
			if err := validateModel(*m); err != nil {
				return err
			}
			for _, cap := range m.Capabilities {
				if !slices.Contains([]string{"chat", "vision", "audio", "video", "tts", "image", "video_generation"}, cap) {
					return fmt.Errorf("未知模型能力")
				}
			}
			m.ID = randomID()
			m.VendorID = v.ID
			m.Provider = v.Provider
			m.Endpoint = v.Endpoint
			m.Key = ""
			if m.Name == "" {
				m.Name = m.Model
			}
		}
		if v.Key != "" {
			if err := s.store.putSecret(v.ID, v.Key); err != nil {
				return err
			}
			v.Configured = true
		}
		v.Key = ""
		return s.store.change(func(st *State) error {
			found := false
			for i := range st.Vendors {
				if st.Vendors[i].ID == v.ID {
					st.Vendors[i] = v
					found = true
				}
			}
			if !found {
				st.Vendors = append(st.Vendors, v)
			}
			for i := range st.Models {
				if st.Models[i].VendorID == v.ID {
					st.Models[i].Endpoint = v.Endpoint
					st.Models[i].Provider = v.Provider
					st.Models[i].Configured = v.Configured
				}
			}
			for _, m := range models {
				if !slices.ContainsFunc(st.Models, func(old Model) bool { return old.VendorID == v.ID && old.Model == m.Model }) {
					m.Configured = v.Configured
					st.Models = append(st.Models, m)
				}
			}
			return nil
		})
	case "delete-vendor":
		err := s.store.change(func(st *State) error {
			for _, m := range st.Models {
				if m.VendorID == q.ID {
					return fmt.Errorf("请先移除此厂商的模型")
				}
			}
			st.Vendors = slices.DeleteFunc(st.Vendors, func(v Vendor) bool { return v.ID == q.ID })
			return nil
		})
		if err == nil {
			_, err = s.store.db.Exec("DELETE FROM secrets WHERE id=?", q.ID)
		}
		return err
	case "pipeline":
		if q.Group == "" {
			q.Group = "chat"
		}
		if !pipelineCapability(q.Group) {
			return fmt.Errorf("流水线能力无效")
		}
		return s.store.change(func(st *State) error {
			seen := map[string]bool{}
			for _, id := range q.Pipeline {
				if seen[id] || !slices.ContainsFunc(st.Models, func(m Model) bool { return m.ID == id && m.Enabled && slices.Contains(m.Capabilities, q.Group) }) {
					return fmt.Errorf("流水线包含重复、停用或能力不匹配的模型")
				}
				seen[id] = true
			}
			if q.Group == "chat" {
				st.Pipeline = append([]string{}, q.Pipeline...)
			} else {
				if st.Pipelines == nil {
					st.Pipelines = map[string][]string{}
				}
				st.Pipelines[q.Group] = append([]string{}, q.Pipeline...)
			}
			st.Settings["routing."+q.Group] = ""
			if len(q.Pipeline) > 0 {
				st.Settings["routing."+q.Group] = q.Pipeline[0]
			}
			st.ConfigRevision++
			return nil
		})
	case "default-prompt":
		return s.store.change(func(st *State) error {
			i := slices.IndexFunc(st.Prompts, func(p Prompt) bool { return p.ID == q.ID })
			if i < 0 {
				return fmt.Errorf("人设不存在")
			}
			selected := st.Prompts[i]
			st.Prompts = append([]Prompt{selected}, append(st.Prompts[:i:i], st.Prompts[i+1:]...)...)
			return nil
		})
	case "reference-image", "reference-audio", "reference-video":
		path := ""
		if q.Media != "" {
			data, err := base64.StdEncoding.DecodeString(q.Media)
			allowed := []string{"image/png", "image/jpeg", "image/webp"}
			if q.Kind == "reference-audio" {
				allowed = []string{"audio/wave", "audio/wav", "audio/x-wav", "audio/mpeg"}
			}
			if err != nil || len(data) > 16<<20 || !slices.Contains(allowed, http.DetectContentType(data)) {
				return fmt.Errorf("参考媒体格式无效；图片用 PNG/JPEG/WebP，音频用 WAV/MP3，最大 16 MB")
			}
			path, err = s.saveMedia(data, http.DetectContentType(data))
			if err != nil {
				return err
			}
		}
		return s.store.change(func(st *State) error {
			key := "image.reference"
			if q.Kind == "reference-video" {
				key = "video_generation.reference"
			}
			if q.Kind == "reference-audio" {
				key = "tts.reference"
			}
			st.Settings[key] = path
			return nil
		})
	case "behavior-policy":
		return s.saveBehavior(q.Policy)
	}
	return fmt.Errorf("未知模型中心操作")
}

// modelPipeline 兼容旧路由；未配置时首选旧默认模型，明确保存空列表则停用该流水线。
func modelPipeline(st State, capability string) []string {
	if capability == "chat" && st.Pipeline != nil {
		return st.Pipeline
	}
	if ids, exists := st.Pipelines[capability]; exists {
		return ids
	}
	ids := []string{}
	for _, m := range st.Models {
		if m.Enabled && slices.Contains(m.Capabilities, capability) {
			ids = append(ids, m.ID)
		}
	}
	preferred, _ := st.Settings["routing."+capability].(string)
	if i := slices.Index(ids, preferred); i > 0 {
		ids = append([]string{preferred}, append(ids[:i:i], ids[i+1:]...)...)
	}
	return ids
}
func pipelineCapability(capability string) bool {
	return slices.Contains([]string{"chat", "tts", "image", "video", "video_generation"}, capability)
}

// 三类语音任务不混用；克隆或声音设计失败不能降级为普通音色。
func speechMode(model string) string {
	if strings.HasSuffix(model, "-voiceclone") {
		return "clone"
	}
	if strings.HasSuffix(model, "-voicedesign") {
		return "design"
	}
	return "tts"
}
