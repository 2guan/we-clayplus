// 主要功能：仅在任务启用 DEBUG 时记录模型 HTTP 原文，排除认证信息和媒体二进制。
package ai

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/we-clay/we-clay/plugins/internal/debuglog"
)

var debugRequestID atomic.Uint64

type debugTransport struct{}

func (debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	sink := debuglog.From(req.Context())
	if sink == nil {
		return http.DefaultTransport.RoundTrip(req)
	}
	id := debugRequestID.Add(1)
	secrets := []string{strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer "), req.Header.Get("x-api-key"), req.Header.Get("x-goog-api-key")}
	fields := map[string]any{"request_id": id, "method": req.Method, "endpoint": req.URL.Scheme + "://" + req.URL.Host + req.URL.EscapedPath(), "content_type": req.Header.Get("Content-Type")}
	if req.GetBody != nil && debugTextType(req.Header.Get("Content-Type")) {
		body, err := req.GetBody()
		if err == nil {
			raw, e := io.ReadAll(body)
			_ = body.Close()
			if e == nil {
				fields["body"] = debugBody(raw, secrets)
			}
		}
	} else if req.Body != nil {
		fields["body"] = "[媒体或 multipart 数据已省略]"
		fields["bytes"] = req.ContentLength
	}
	sink("模型请求原文", fields)
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		sink("模型请求失败", map[string]any{"request_id": id, "error": safeConnectionError(err).Error()})
		return resp, err
	}
	resp.Body = &debugResponseBody{ReadCloser: resp.Body, sink: sink, id: id, status: resp.StatusCode, contentType: resp.Header.Get("Content-Type"), secrets: secrets}
	return resp, nil
}

func debugTextType(kind string) bool {
	return strings.HasPrefix(kind, "text/") || strings.Contains(kind, "json") || strings.Contains(kind, "xml")
}

// JSON/SSE 在没有敏感字段时逐字保留；仅调试副本发生脱敏。
func debugBody(raw []byte, secrets []string) string {
	text := string(raw)
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[凭据已脱敏]")
		}
	}
	cleanJSON := func(text string) string {
		var value any
		if json.Unmarshal([]byte(text), &value) != nil {
			return text
		}
		changed := false
		var clean func(any) any
		clean = func(v any) any {
			switch v := v.(type) {
			case map[string]any:
				for k, item := range v {
					switch strings.ToLower(k) {
					case "api_key", "apikey", "access_token", "authorization", "private_key":
						v[k], changed = "[凭据已脱敏]", true
					case "b64_json", "bytesbase64encoded":
						v[k], changed = "[媒体数据已省略]", true
					default:
						if k == "data" {
							if _, ok := item.(string); ok {
								v[k], changed = "[媒体数据已省略]", true
								continue
							}
						}
						v[k] = clean(item)
					}
				}
			case []any:
				for i := range v {
					v[i] = clean(v[i])
				}
			case string:
				if strings.HasPrefix(v, "data:") && strings.Contains(v, ";base64,") {
					changed = true
					return "[媒体数据已省略]"
				}
			}
			return v
		}
		value = clean(value)
		if !changed {
			return text
		}
		b, _ := json.Marshal(value)
		return string(b)
	}
	if json.Valid([]byte(text)) {
		return cleanJSON(text)
	}
	lines := strings.SplitAfter(text, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "data:") {
			original := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			cleaned := cleanJSON(original)
			if cleaned == original {
				continue
			}
			ending := ""
			if strings.HasSuffix(line, "\n") {
				ending = "\n"
			}
			lines[i] = "data: " + cleaned + ending
		}
	}
	return strings.Join(lines, "")
}

type debugResponseBody struct {
	io.ReadCloser
	sink              debuglog.Sink
	id                uint64
	status            int
	contentType       string
	secrets           []string
	body              bytes.Buffer
	read              int64
	complete, emitted bool
}

func (b *debugResponseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.read += int64(n)
	if debugTextType(b.contentType) {
		// 与现有流式响应上限一致；超限明确标记，避免无界调试缓存。
		remaining := maxStreamResponseBytes - int64(b.body.Len())
		if remaining > 0 {
			b.body.Write(p[:min(int64(n), remaining)])
		}
	}
	if err == io.EOF {
		b.complete = true
		b.emit()
	}
	return n, err
}

func (b *debugResponseBody) Close() error {
	// 业务层在 HTTP 失败时不解析响应；DEBUG 仍读取有上限的错误正文。
	if b.status >= 300 && b.read == 0 {
		_, _ = io.Copy(io.Discard, io.LimitReader(b, 32<<20))
	}
	err := b.ReadCloser.Close()
	b.emit()
	return err
}

func (b *debugResponseBody) emit() {
	if b.emitted {
		return
	}
	b.emitted = true
	fields := map[string]any{"request_id": b.id, "status": b.status, "content_type": b.contentType, "complete": b.complete, "bytes": b.read}
	if debugTextType(b.contentType) {
		fields["body"] = debugBody(b.body.Bytes(), b.secrets)
		fields["truncated"] = b.read > int64(b.body.Len())
	} else {
		fields["body"] = "[媒体二进制已省略]"
	}
	b.sink("模型响应原文", fields)
}
