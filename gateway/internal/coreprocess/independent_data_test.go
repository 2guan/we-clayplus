package coreprocess

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestIndependentDataDirMatchesOriginalLaunchPath(t *testing.T) {
	original := filepath.Join(t.TempDir(), "core account")
	r := &independentRecord{DataDir: original}
	legacy, err := matchIndependentDataDir(r, original)
	if err != nil || legacy {
		t.Fatalf("same directory rejected: legacy=%t err=%v", legacy, err)
	}
	_, err = matchIndependentDataDir(r, filepath.Join(t.TempDir(), "core account"))
	if err == nil || !strings.Contains(err.Error(), "数据目录") {
		t.Fatalf("moved record accepted: %v", err)
	}
}

func TestDataDirFromArgs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "core account")
	if got, err := dataDirFromArgs([]string{"core", "-data", dir, "-device-type", "mac"}); err != nil || got != dir {
		t.Fatalf("path with spaces: got=%q err=%v", got, err)
	}
	for _, args := range [][]string{{"core"}, {"core", "-data"}, {"core", "-data", "relative"}, {"core", "-data", dir, "-data", dir}} {
		if _, err := dataDirFromArgs(args); err == nil {
			t.Fatalf("accepted invalid argv: %q", args)
		}
	}
}
