// 文件名：wechat_image.go
// 主要功能：通过认证后的同源入口读取微信头像和朋友圈图片，限制来源、大小及内容类型。
// 作者：2guan
package dashboard

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func wechatImageURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Port() != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, false
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".qlogo.cn") && !strings.HasSuffix(host, ".qpic.cn") && host != "wxapp.tc.qq.com" && host != "vweixinf.tc.qq.com" {
		return nil, false
	}
	return u, true
}

func (s *Server) wechatImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "请求方法不支持")
		return
	}
	u, ok := wechatImageURL(r.URL.Query().Get("url"))
	if !ok {
		writeError(w, 400, "不支持的微信图片地址")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		writeError(w, 400, "图片地址无效")
		return
	}
	client := &http.Client{CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if _, ok := wechatImageURL(next.URL.String()); !ok || len(via) >= 3 {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, 502, "暂时无法获取微信图片")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		writeError(w, 502, "微信图片暂不可用")
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(body) > 8<<20 {
		writeError(w, 502, "图片过大或读取失败")
		return
	}
	// CDN 明确标记加密时才解码，原始密钥保持十进制字符串以免精度损失。
	if resp.Header.Get("X-Enc") == "1" {
		key, err := strconv.ParseUint(r.URL.Query().Get("key"), 10, 64)
		if err != nil {
			writeError(w, 502, "图片缺少解密信息，请刷新朋友圈")
			return
		}
		decryptWechatImage(body, key)
	}
	contentType := http.DetectContentType(body)
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/gif" && contentType != "image/webp" {
		writeError(w, 502, "返回内容不是受支持的图片")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Write(body)
}
