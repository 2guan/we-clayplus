// 文件名：media_wav_test.go
// 主要功能：验证 WAV 缓存扩展名、旧缓存播放响应和普通附件隔离。
// 作者：2guan
package dashboard

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWAVCachePlayback(t *testing.T) {
	s := newServer(t, true)
	// 合成空 WAV 文件头；测试不读取真实语音，也不连接微信。
	wav := []byte("RIFF\x24\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\xc0\x5d\x00\x00\x80\xbb\x00\x00\x02\x00\x10\x00data\x00\x00\x00\x00")
	path, err := s.saveMedia(wav, http.DetectContentType(wav))
	if err != nil || !strings.HasSuffix(path, ".wav") {
		t.Fatalf("WAV 扩展名异常: %s %v", path, err)
	}
	for _, item := range []struct {
		name              string
		data              []byte
		mime, disposition string
	}{
		{"legacy.bin", wav, "audio/wav", ""},
		{"attachment.bin", []byte("<html>test</html>"), "application/octet-stream", "attachment"},
	} {
		if err := os.WriteFile(filepath.Join(s.cfg.DataDir, "media", item.name), item.data, 0600); err != nil {
			t.Fatal(err)
		}
		w := call(t, s, "/media/"+item.name, nil, nil, "")
		if w.Code != 200 || w.Header().Get("Content-Type") != item.mime || w.Header().Get("Content-Disposition") != item.disposition {
			t.Fatalf("%s 响应异常: %d %v", item.name, w.Code, w.Header())
		}
	}
}
