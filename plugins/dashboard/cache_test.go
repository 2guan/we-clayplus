package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCacheClearingBoundaries(t *testing.T) {
	s := newServer(t, true)
	dir := filepath.Join(s.cfg.DataDir, "media")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	old := time.Now().Add(-40 * 24 * time.Hour)
	write := func(name string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if e := os.WriteFile(p, []byte("fixture"), 0600); e != nil {
			t.Fatal(e)
		}
		if e := os.Chtimes(p, old, old); e != nil {
			t.Fatal(e)
		}
	}
	for _, name := range []string{"inbound.bin", "outbound.bin", "reference.wav", "orphan.bin", "other.bin", "new.bin"} {
		write(name)
	}
	if e := os.Chtimes(filepath.Join(dir, "new.bin"), time.Now(), time.Now()); e != nil {
		t.Fatal(e)
	}
	outside := filepath.Join(t.TempDir(), "outside.bin")
	if e := os.WriteFile(outside, []byte("protected"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(dir, "escape.bin")); e != nil {
		t.Fatal(e)
	}
	if e := s.store.change(func(v *State) error {
		v.Tasks = nil
		v.Messages = []Message{{ID: "in", Media: "/media/inbound.bin"}, {ID: "out", Self: true, Media: "/media/outbound.bin"}}
		v.Settings["tts.reference"] = "/media/reference.wav"
		addLog(v, "DEBUG", "debug", strings.Repeat("fixture-long-log ", 500))
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(Message{ID: "other", Media: "/media/other.bin"})
	if _, e := s.store.db.Exec("INSERT INTO state_rows VALUES('other-account','messages',0,?)", body); e != nil {
		t.Fatal(e)
	}
	if e := s.store.putSecret("insight-cache:fixture", "fixture-cache"); e != nil {
		t.Fatal(e)
	}
	if e := s.store.putSecret("report-source:fixture", "fixture-report"); e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"media", "logs", "insights", "avatars"} {
		w := call(t, s, "/api/cache", map[string]any{"kind": kind, "days": 30, "confirm": true}, nil, "")
		if w.Code != 200 {
			t.Fatal(kind, w.Code, w.Body.String())
		}
	}
	for _, name := range []string{"outbound.bin", "reference.wav", "other.bin", "new.bin", "escape.bin"} {
		if _, e := os.Lstat(filepath.Join(dir, name)); e != nil {
			t.Fatal("保护文件被清理", name, e)
		}
	}
	for _, name := range []string{"inbound.bin", "orphan.bin"} {
		if _, e := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(e) {
			t.Fatal("缓存未清理", name)
		}
	}
	if v, _ := os.ReadFile(outside); string(v) != "protected" {
		t.Fatal("清理越过目录边界")
	}
	if s.store.snapshot().Messages[0].Media != "" || s.store.snapshot().Messages[1].Media == "" {
		t.Fatal("媒体元数据未同步")
	}
	if value, e := s.store.getSecret("report-source:fixture"); e != nil || value != "fixture-report" {
		t.Fatal("报告快照被删除")
	}
	if _, e := s.store.getSecret("insight-cache:fixture"); e == nil {
		t.Fatal("复用缓存未清理")
	}
	if settingText(s.store.snapshot(), "cache.avatar_version") == "" {
		t.Fatal("头像未失效")
	}
	// 全部清理日志，保留一条清理操作记录；聊天归档不变。
	w := call(t, s, "/api/cache", map[string]any{"kind": "logs", "days": 0, "confirm": true}, nil, "")
	if w.Code != 200 || len(s.store.snapshot().Messages) != 2 || len(s.store.snapshot().Logs) != 1 {
		t.Fatal("日志清理影响其他数据", w.Code)
	}
}

func TestCacheRejectsMissingConfirmationAndBusyTask(t *testing.T) {
	s := newServer(t, true)
	for _, q := range []map[string]any{{"kind": "media", "days": 0}, {"kind": "../core", "days": 0, "confirm": true}, {"kind": "logs", "days": -1, "confirm": true}} {
		if w := call(t, s, "/api/cache", q, nil, ""); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	if e := s.store.change(func(v *State) error { v.Tasks = append(v.Tasks, Task{ID: "busy", Status: "running"}); return nil }); e != nil {
		t.Fatal(e)
	}
	if w := call(t, s, "/api/cache", map[string]any{"kind": "logs", "days": 0, "confirm": true}, nil, ""); w.Code != 409 {
		t.Fatal("活动任务期间清理", w.Code)
	}
}
