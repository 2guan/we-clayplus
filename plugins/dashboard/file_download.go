package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

// 附件先流式写入私有临时文件，直到 EOF（包含底层长度、MD5 校验）后
// 才加入媒体缓存并响应下载。失败时删除临时文件，不发布半成品。
func (s *Server) cacheMessageFile(ctx context.Context, m Message) (string, error) {
	account := s.store.snapshot().Account.ID
	if s.cfg.Mock || s.host == nil {
		return "", fmt.Errorf("当前附件不可用")
	}
	secret, err := s.store.getSecret(s.messageMediaKey(m))
	if err != nil {
		secret, err = s.fileDescriptor(m)
	}
	if err != nil {
		return "", fmt.Errorf("此历史附件缺少可用下载信息，请在微信中查看或重新发送")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	result, err := s.host.Execute(ctx, control.Command{Operation: "message-file", ID: m.ID, Target: m.Target, Params: json.RawMessage(secret)})
	if err != nil {
		return "", fmt.Errorf("下载附件失败：%w", err)
	}
	reader, ok := result.(io.ReadCloser)
	if !ok || reader == nil {
		return "", fmt.Errorf("附件下载未返回有效数据流")
	}
	var closeOnce sync.Once
	closeReader := func() { closeOnce.Do(func() { _ = reader.Close() }) }
	defer closeReader()
	stop := context.AfterFunc(ctx, closeReader)
	defer stop()
	f, err := os.CreateTemp(s.cfg.DataDir, ".download-*.part")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(reader, (1<<30)+1))
	if err != nil {
		return "", fmt.Errorf("下载附件失败：%w", err)
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if n == 0 || n > 1<<30 {
		return "", fmt.Errorf("附件为空或超过 1 GiB")
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	dir := filepath.Join(s.cfg.DataDir, "media")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	name := randomID() + ".bin"
	path := filepath.Join(dir, name)
	if err = os.Rename(f.Name(), path); err != nil {
		return "", err
	}
	url := "/media/" + name
	err = s.store.change(func(st *State) error {
		if st.Account.ID != account {
			return fmt.Errorf("下载期间账号已切换")
		}
		for i := range st.Messages {
			if st.Messages[i].ID == m.ID && st.Messages[i].Target == m.Target {
				st.Messages[i].Media = url
				return nil
			}
		}
		return fmt.Errorf("附件消息已不在归档中")
	})
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return url, nil
}

func (s *Server) serveMessageFile(w http.ResponseWriter, r *http.Request, m Message) {
	url := m.Media
	var err error
	if url == "" {
		url, err = s.cacheMessageFile(r.Context(), m)
		if err != nil {
			writeError(w, 502, err.Error())
			return
		}
	}
	name := strings.TrimPrefix(url, "/media/")
	if name == url || name == "" || name != filepath.Base(name) || strings.Contains(name, "..") {
		writeError(w, 502, "附件尚未归档到本机")
		return
	}
	f, err := os.Open(filepath.Join(s.cfg.DataDir, "media", name))
	if err != nil {
		writeError(w, 502, "附件文件已不可用")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 1<<30 {
		writeError(w, 502, "附件文件无效")
		return
	}
	w.Header().Set("Content-Disposition", attachmentDisposition(m.Content))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, info.ModTime(), f)
}
