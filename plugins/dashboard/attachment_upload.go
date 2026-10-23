package dashboard

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

func attachmentLimit(st State) int64 {
	n := settingNumber(st, "attachments.max_file_mb")
	if n < 1 || n > 1024 {
		n = 500
	}
	return int64(n) << 20
}

func (s *Server) openAttachment(ref string) (*os.File, int64, error) {
	name := mediaName(ref)
	if name == "" {
		return nil, 0, fmt.Errorf("附件引用无效")
	}
	p := filepath.Join(s.cfg.DataDir, "media", name)
	info, e := os.Lstat(p)
	if e != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return nil, 0, fmt.Errorf("原附件不可读取")
	}
	if info.Size() > attachmentLimit(s.store.snapshot()) {
		return nil, 0, fmt.Errorf("附件超过当前配置的 %d MB 上限", attachmentLimit(s.store.snapshot())>>20)
	}
	f, e := os.Open(p)
	return f, info.Size(), e
}

// 普通文件使用二进制请求体逐块落盘，不放进 Base64 JSON；与小媒体限制独立。
func (s *Server) sendAttachment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "请求方法不支持")
		return
	}
	if !s.attachmentMu.TryLock() {
		writeError(w, 429, "正在处理其他文件附件，请稍后重试")
		return
	}
	defer s.attachmentMu.Unlock()
	st := s.store.snapshot()
	target, name := r.URL.Query().Get("target"), r.URL.Query().Get("name")
	if st.Account.Status != "online" || !slices.ContainsFunc(st.Contacts, func(c Contact) bool { return c.ID == target }) {
		writeError(w, 400, "微信离线或目标会话不存在")
		return
	}
	if name == "" || len(name) > 255 || strings.ContainsAny(name, "/\\\x00\r\n") {
		writeError(w, 400, "附件文件名无效")
		return
	}
	limit := attachmentLimit(st)
	if r.ContentLength > limit {
		writeError(w, 413, fmt.Sprintf("附件超过配置的 %d MB 上限", limit>>20))
		return
	}
	// 已知大小先检查实际发送能力，避免上传数百 MB 后才发现底层不支持。
	if !s.cfg.Mock && s.host != nil && r.ContentLength > 0 {
		if _, e := s.host.Execute(r.Context(), control.Command{Operation: "file-send-check", File: &control.FileInput{Size: r.ContentLength}}); e != nil {
			writeError(w, 400, e.Error())
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Minute)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(30 * time.Minute))
	closeBody := context.AfterFunc(ctx, func() { _ = controller.SetReadDeadline(time.Now()); _ = r.Body.Close() })
	defer closeBody()
	f, e := os.CreateTemp(s.cfg.DataDir, ".download-upload-*.part")
	if e != nil {
		writeError(w, 500, "无法保存上传附件")
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()
	n, e := io.Copy(f, io.LimitReader(r.Body, limit+1))
	if e != nil || ctx.Err() != nil {
		writeError(w, 400, "附件上传中断，未发送微信消息")
		return
	}
	if n == 0 || n > limit {
		writeError(w, 413, fmt.Sprintf("附件不能为空，且不得超过 %d MB", limit>>20))
		return
	}
	if r.ContentLength >= 0 && n != r.ContentLength {
		writeError(w, 400, "附件未完整上传，未发送微信消息")
		return
	}
	if e = f.Close(); e != nil {
		writeError(w, 500, "附件保存失败")
		return
	}
	if s.store.snapshot().Account.ID != st.Account.ID {
		writeError(w, 409, "上传期间账号已切换，未发送微信消息")
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "media")
	if e = os.MkdirAll(dir, 0700); e != nil {
		writeError(w, 500, "无法保存上传附件")
		return
	}
	ref := randomID() + ".bin"
	path := filepath.Join(dir, ref)
	if e = os.Rename(f.Name(), path); e != nil {
		writeError(w, 500, "附件保存失败")
		return
	}
	e = s.send(ctx, actionRequest{Target: target, Content: name, Group: "file", fileMedia: "/media/" + ref, fileAccount: st.Account.ID}, "manual")
	if e != nil {
		// 完整文件若已进入发送记录，保留用于人工核对与重发。
		var refs int
		refErr := s.store.db.QueryRow("SELECT count(*) FROM state_rows WHERE kind='messages' AND instr(CAST(body AS TEXT),?)>0", "/media/"+ref).Scan(&refs)
		if refErr == nil && refs == 0 {
			_ = os.Remove(path)
		}
		writeError(w, 400, e.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
