package messageability

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	coreapi "github.com/we-clay/we-clay/gateway/internal/api"
	"github.com/we-clay/we-clay/gateway/internal/coretest"
)

func TestFileReaderPreservesMultipartAndOriginalReceipt(t *testing.T) {
	calls := 0
	srv := coretest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/message/file" || r.Method != "POST" {
			t.Error("文件路由变化")
		}
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			t.Error(e)
			return
		}
		defer r.MultipartForm.RemoveAll()
		f, meta, e := r.FormFile("file")
		if e != nil {
			t.Error(e)
			return
		}
		defer f.Close()
		body, _ := io.ReadAll(f)
		if r.FormValue("receiver") != "test-peer" || meta.Filename != "原文件.bin" || string(body) != "synthetic file" {
			t.Error("文件名、目标或正文丢失")
		}
		_, _ = fmt.Fprint(w, `{"code":0,"data":{"base_response":{"code":0},"new_id":"9007199254740993","client_id":"original_file_receipt","create_time":1791774000}}`)
	}))
	defer srv.Close()
	client, e := coreapi.NewGRPCClient(context.Background(), srv.Address, "")
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	old := coreapi.GetCore
	coreapi.GetCore = func() *coreapi.Client { return client }
	t.Cleanup(func() { coreapi.GetCore = old })
	r, e := SendFileReader(context.Background(), "test-peer", "原文件.bin", strings.NewReader("synthetic file"))
	if e != nil || r.GetNewId() != 9007199254740993 || calls != 1 {
		t.Fatal("流式入口未保留发送回执或重复发送", e, calls)
	}
	_, original, created, ok := ReceiptFields("test-peer", r.GetNewId())
	if !ok || original != "original_file_receipt" || created != 1791774000 {
		t.Fatal("原始撤回回执未传递")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = SendFileReader(ctx, "test-peer", "cancel.bin", strings.NewReader("synthetic")); e == nil || calls != 1 {
		t.Fatal("取消后仍提交发送")
	}
}
