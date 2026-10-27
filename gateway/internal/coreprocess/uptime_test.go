package coreprocess

import (
	"context"
	"net"
	"runtime"
	"testing"
	"time"
)

func TestProcessElapsed(t *testing.T) {
	for raw, want := range map[string]time.Duration{"00:01": time.Second, "01:02:03": time.Hour + 2*time.Minute + 3*time.Second, "2-03:04:05": 51*time.Hour + 4*time.Minute + 5*time.Second} {
		got, err := processElapsed(raw)
		if err != nil || got != want {
			t.Fatal(raw, got, err)
		}
	}
	for _, raw := range []string{"", "bad", "1:60", "-1:00", "1:00:00:00", "2147483647-00:00:00"} {
		if _, err := processElapsed(raw); err == nil {
			t.Fatal("接受了无效时长", raw)
		}
	}
}

func TestCoreStartedAtUsesListeningProcess(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("平台不支持进程指标")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	before := time.Now()
	start, err := CoreStartedAt(ctx, l.Addr().String())
	if err != nil {
		t.Skipf("系统进程查询工具不可用：%v", err)
	}
	if start.After(before.Add(time.Second)) || time.Since(start) > time.Hour {
		t.Fatal("监听进程时间不可信", start)
	}
	for _, address := range []string{"example.com:8088", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:8088;echo", "invalid"} {
		if _, err := CoreStartedAt(ctx, address); err == nil {
			t.Fatal("接受了外部或无效地址", address)
		}
	}
}
