package config

import (
	"os"
	"testing"
)

func TestCoreDataDirMustBeExplicitAndStable(t *testing.T) {
	for _, dir := range []string{"", "./runtime/core", "relative"} {
		if _, e := (CoreConfig{Version: "v1", DataDir: dir}).Resolve(); e == nil {
			t.Fatal("accepted implicit profile directory")
		}
	}
	dir := t.TempDir()
	cfg := CoreConfig{Version: "v1", DataDir: dir}
	a, e := cfg.Resolve()
	if e != nil {
		t.Fatal(e)
	}
	old, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	defer os.Chdir(old)
	if e = os.Chdir(t.TempDir()); e != nil {
		t.Fatal(e)
	}
	b, e := cfg.Resolve()
	if e != nil || a.DataDir != b.DataDir {
		t.Fatal("profile moved with CWD", e)
	}
}
