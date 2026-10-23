package console

import (
	"context"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/sdk/control"
)

func TestFileInputPreflightRejectsCurrentCoreLimitAndForgedInput(t *testing.T) {
	b := &Bridge{}
	for _, n := range []int64{0, (32 << 20) + 1, 500 << 20} {
		if _, e := b.Execute(context.Background(), control.Command{Operation: "file-send-check", File: &control.FileInput{Size: n}}); e == nil {
			t.Fatal("不支持的大小未提前拒绝", n)
		}
	}
	if _, e := b.Execute(context.Background(), control.Command{Operation: "file-send-check", File: &control.FileInput{Size: 32 << 20}}); e != nil {
		t.Fatal(e)
	}
	if _, e := b.sendFileInput(context.Background(), control.Command{Target: "test-peer", Content: "a.bin", Params: []byte(`{"kind":"file"}`), File: &control.FileInput{Size: 10, Reader: strings.NewReader("short")}}); e == nil {
		t.Fatal("声明大小不一致仍进入发送")
	}
}
