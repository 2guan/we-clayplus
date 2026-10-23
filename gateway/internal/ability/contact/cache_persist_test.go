package contactability

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteContactCacheReplacesWithPrivateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contact.json")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeContactCache(path, []byte("new"), os.Rename); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new" {
		t.Fatalf("联系人缓存替换失败：%q, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("联系人缓存权限不是 0600：%o", info.Mode().Perm())
	}
	assertNoContactTempFiles(t, dir)
}

func TestWriteContactCacheFailedReplacePreservesOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contact.json")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	replaceErr := errors.New("replace failed")
	err := writeContactCache(path, []byte("new"), func(temporary, destination string) error {
		if destination != path || filepath.Dir(temporary) != dir {
			t.Fatal("临时文件和旧缓存不在同一目录")
		}
		data, readErr := os.ReadFile(temporary)
		if readErr != nil || string(data) != "new" {
			t.Fatalf("替换前临时文件不完整：%q, %v", data, readErr)
		}
		return replaceErr
	})
	if !errors.Is(err, replaceErr) {
		t.Fatalf("替换错误未返回：%v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "old" {
		t.Fatalf("替换失败损坏旧缓存：%q, %v", got, err)
	}
	assertNoContactTempFiles(t, dir)
}

func assertNoContactTempFiles(t *testing.T, dir string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, ".contact-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("缓存临时文件未清理：%v", files)
	}
}
