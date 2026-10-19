// 文件名：wechat_thumbnail.go
// 主要功能：加密保存链接缩略图票据，按需通过 core 获取图片，避免阻塞消息接收。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

func (s *Server) prepareArticleThumbnails(m *Message) error {
	for i := range m.Articles {
		a := &m.Articles[i]
		if a.Image != "" || a.ThumbID == "" || a.ThumbKey == "" {
			continue
		}
		ref := randomID()
		secret, _ := json.Marshal(map[string]string{"id": a.ThumbID, "key": a.ThumbKey})
		if err := s.store.putSecret("message-thumbnail:"+ref, string(secret)); err != nil {
			return err
		}
		a.Image = "/api/message-thumbnail?id=" + ref
		a.ThumbID, a.ThumbKey = "", ""
	}
	return nil
}

func (s *Server) messageThumbnail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "请求方法不支持")
		return
	}
	ref := r.URL.Query().Get("id")
	// 必须属于当前账号归档，不能用任意 CDN 票据或旧账号引用下载图片。
	allowed := false
	for _, m := range s.store.snapshot().Messages {
		for _, a := range m.Articles {
			if a.Image == "/api/message-thumbnail?id="+ref && ref != "" {
				allowed = true
			}
		}
	}
	if !allowed || s.cfg.Mock {
		writeError(w, 404, "缩略图不存在")
		return
	}
	secret, err := s.store.getSecret("message-thumbnail:" + ref)
	if err != nil {
		writeError(w, 404, "缩略图不存在")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	result, err := s.host.Execute(ctx, control.Command{Operation: "message-thumbnail", Params: json.RawMessage(secret)})
	data, ok := result.([]byte)
	if err != nil || !ok || len(data) == 0 || len(data) > 8<<20 {
		writeError(w, 502, "暂时无法获取缩略图")
		return
	}
	// 只展示栅格图片，拒绝 HTML、SVG 和异常 CDN 响应。
	kind := http.DetectContentType(data)
	if !strings.Contains("|image/jpeg|image/png|image/gif|image/webp|", "|"+kind+"|") {
		writeError(w, 502, "缩略图格式无效")
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}
