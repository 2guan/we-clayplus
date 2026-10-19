// 文件名：emoji_test.go
// 主要功能：核对 core 的表情 multipart 契约，引用发送不携带空文件或专辑字段。
// 作者：2guan
package messageapi

import (
	"context"
	"github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
	"io"
	"net/http"
	"testing"
)

func TestEmojiMultipartReference(t *testing.T) {
	const digest = "0123456789abcdef0123456789abcdef"
	for _, data := range [][]byte{nil, []byte("original-gif")} {
		server := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/message/emoji" {
				t.Error("wrong endpoint")
			}
			if e := r.ParseMultipartForm(1 << 20); e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			if len(r.MultipartForm.Value) != 2 || r.FormValue("receiver") != "synthetic" || r.FormValue("md5") != digest {
				t.Error("unexpected protocol fields")
			}
			if len(data) == 0 {
				if len(r.MultipartForm.File) != 0 {
					t.Error("MD5 引用携带空文件")
				}
			} else {
				f, _, e := r.FormFile("emoji")
				if e != nil {
					t.Error(e)
				} else {
					b, _ := io.ReadAll(f)
					f.Close()
					if string(b) != string(data) {
						t.Error("原文件被改变")
					}
				}
			}
			io.WriteString(w, `{"code":0,"data":{"base_response":{"code":0},"result":[{"code":0,"new_id":"9007199254740993","md5":"`+digest+`"}]}}`)
		}))
		client, e := api.NewGRPCClient(context.Background(), server.Address, "")
		if e != nil {
			t.Fatal(e)
		}
		r := remote{client: client}
		if _, e = r.SendEmoji("synthetic", digest, data); e != nil {
			t.Fatal(e)
		}
		server.Close()
	}
}
