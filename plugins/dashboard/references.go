// 文件名：references.go
// 主要功能：读取已保存的垫图与音色样本，自拍无垫图时使用 SDK 返回的当前微信头像。
// 作者：2guan
package dashboard

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/we-clay/we-clay/plugins/ai"
)

func (s *Server) prepareReference(ctx context.Context, st State, q *ai.Request) error {
	if q.Kind == "image" && q.Size == "" {
		aspect, _ := st.Settings["image.aspect"].(string)
		resolution, _ := st.Settings["image.resolution"].(string)
		if aspect != "" && resolution != "" {
			var a, b, n int
			_, _ = fmt.Sscanf(aspect, "%d:%d", &a, &b)
			_, _ = fmt.Sscanf(resolution, "%d", &n)
			if a > 0 && b > 0 && n > 0 {
				w, h := n, n
				if a > b {
					h = (n*b/a + 7) / 8 * 8
				} else if b > a {
					w = (n*a/b + 7) / 8 * 8
				}
				q.Size = fmt.Sprintf("%dx%d", w, h)
			}
		}
	}
	if q.Media != "" {
		return nil
	}
	path := ""
	if q.Kind == "image" && q.Selfie {
		path, _ = st.Settings["image.reference"].(string)
	}
	if q.Kind == "video_generation" {
		path, _ = st.Settings["video_generation.reference"].(string)
	}
	if q.Kind == "tts" && q.SpeechMode == "clone" {
		path, _ = st.Settings["tts.reference"].(string)
	}
	var data []byte
	var err error
	if strings.HasPrefix(path, "/media/") && filepath.Base(path) == strings.TrimPrefix(path, "/media/") {
		file, e := os.Open(filepath.Join(s.cfg.DataDir, "media", filepath.Base(path)))
		if e != nil {
			return fmt.Errorf("保存的参考媒体不可用，请重新上传")
		}
		defer file.Close()
		data, err = io.ReadAll(io.LimitReader(file, (16<<20)+1))
	} else if q.Kind == "image" && q.Selfie {
		// 头像 URL 只来自 SDK 的当前账号资料，不能由请求方指定任意下载地址。
		u, e := url.Parse(st.Account.Photo)
		if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return fmt.Errorf("当前微信头像尚不可用，请登录微信同步头像或上传默认垫图")
		}
		fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(fetchCtx, http.MethodGet, u.String(), nil)
		client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, e := client.Do(req)
		if e != nil {
			return fmt.Errorf("微信头像下载失败，请上传默认垫图")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("微信头像暂不可用，请上传默认垫图")
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	} else {
		return nil
	}
	if err != nil || len(data) == 0 || len(data) > 16<<20 {
		return fmt.Errorf("参考媒体读取失败或超过 16 MB")
	}
	q.Media = base64.StdEncoding.EncodeToString(data)
	q.MIME = http.DetectContentType(data)
	if (q.Kind == "image" || q.Kind == "video_generation") && !strings.HasPrefix(q.MIME, "image/") {
		return fmt.Errorf("头像或垫图不是有效图片")
	}
	return nil
}
