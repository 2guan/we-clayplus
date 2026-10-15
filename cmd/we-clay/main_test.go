package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrepareReusesExplicitAccountDirectoryAndKeepsCoreAlive(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "existing-account")
	if err := os.Mkdir(old, 0700); err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(root, "license.key")
	testKey := "wca_" + strings.Repeat("a", 64)
	if err := os.WriteFile(keyFile, []byte(testKey+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := arguments{dataDir: root, coreDataDir: old, licenseFile: keyFile, coreVersion: defaultCoreVersion, coreAddress: "127.0.0.1:8088", loginAddress: "127.0.0.1:8201", dashboardAddress: "127.0.0.1:8208"}
	cfg, key, err := prepare(a)
	if err != nil {
		t.Fatal(err)
	}
	if key != testKey || !cfg.Core.KeepAlive || cfg.Core.DataDir != old || cfg.Console.Address != "127.0.0.1:8208" {
		t.Fatalf("existing account or dashboard settings changed: %+v", cfg)
	}
	missing := a
	missing.coreDataDir = filepath.Join(root, "misspelt-account")
	if _, _, err := prepare(missing); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("a misspelt existing account path would start a new device: %v", err)
	}
}

func TestLicenseFileMustBePrivateRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	if err := os.WriteFile(path, []byte("wca_"+strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLicense(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("first\nsecond"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLicense(path); err == nil {
		t.Fatal("accepted multiline authorization key")
	}
	if err := os.WriteFile(path, []byte("not-an-authorization-key"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readLicense(path); err == nil {
		t.Fatal("accepted invalid authorization key format")
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := readLicense(path); err == nil {
			t.Fatal("accepted world-readable authorization key")
		}
	}
}
