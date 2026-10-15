// 文件名：managed_test.go
// 主要功能：验证受管进程重启、数据保留、条件拒绝和关闭回收。
// 作者：2guan
package coreprocess

import (
	"context"
	"errors"
	"github.com/we-clay/we-clay/gateway/internal/config"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestManagedRestartKeepsHostAndData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("本测试使用 POSIX 进程夹具")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "fixture-core")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "session.info")
	os.WriteFile(state, []byte("fixture-session"), 0600)
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	m, err := StartManaged(ctx, binary, config.Config{CoreAddress: "127.0.0.1:0", Core: &config.CoreConfig{DataDir: dir, LoginAddress: "-", Stage: "auto"}}, cancel)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	old := m.process
	rejected := errors.New("已在线")
	if err := m.RestartLogin(ctx, func(context.Context) error { return rejected }); err != rejected || m.process != old {
		t.Fatal("拒绝重启时改变了当前进程")
	}
	if err := m.RestartLogin(ctx, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if old == m.process {
		t.Fatal("没有替换子进程")
	}
	select {
	case <-old.Done():
	default:
		t.Fatal("旧进程未回收")
	}
	select {
	case <-ctx.Done():
		t.Fatal("重启取消了宿主")
	case <-time.After(50 * time.Millisecond):
	}
	raw, err := os.ReadFile(state)
	if err != nil || string(raw) != "fixture-session" {
		t.Fatal("重启修改了账号数据")
	}
	current := m.process
	m.Close()
	select {
	case <-current.Done():
	default:
		t.Fatal("关闭后子进程仍运行")
	}
	if err := m.RestartLogin(ctx, func(context.Context) error { return nil }); err == nil {
		t.Fatal("关闭后不应再启动")
	}
}
