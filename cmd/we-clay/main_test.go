package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStopCoreDoesNotRequireLicenseFile(t *testing.T) {
	root := t.TempDir()
	coreDir := filepath.Join(root, "core")
	if err := os.Mkdir(coreDir, 0700); err != nil {
		t.Fatal(err)
	}
	a, err := parseArgs([]string{"--stop-core", "--data-dir", root, "--core-version", "v0.4.4"})
	if err != nil {
		t.Fatal(err)
	}
	if !a.stopCore || a.coreVersion != "v0.4.4" {
		t.Fatalf("stop arguments changed: %+v", a)
	}
	if err := stopCore(context.Background(), a); err != nil {
		t.Fatalf("no managed core should be safe to stop without a Key file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(coreDir, "gateway-core.json")); !os.IsNotExist(err) {
		t.Fatalf("stop created a core process record: %v", err)
	}
}

func TestStopCoreRejectsMissingAccountDirectoryWithoutCreatingIt(t *testing.T) {
	root := t.TempDir()
	a, err := parseArgs([]string{"--stop-core", "--data-dir", root})
	if err != nil {
		t.Fatal(err)
	}
	if err := stopCore(context.Background(), a); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("missing account directory was accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "core")); !os.IsNotExist(err) {
		t.Fatalf("stop created a core directory: %v", err)
	}
}

func TestStopCoreCannotCombineWithCheckOrVersion(t *testing.T) {
	for _, option := range []string{"--check", "--version", "--prepare-core"} {
		if _, err := parseArgs([]string{"--stop-core", option}); err == nil {
			t.Fatalf("accepted --stop-core with %s", option)
		}
	}
}

func TestPrepareCoreRequiresLicenseAndExclusiveAction(t *testing.T) {
	for _, option := range []string{"--check", "--version", "--stop-core"} {
		if _, err := parseArgs([]string{"--prepare-core", option}); err == nil {
			t.Fatalf("accepted --prepare-core with %s", option)
		}
	}
	a, err := parseArgs([]string{"--prepare-core", "--data-dir", t.TempDir(), "--core-version", "v0.4.6"})
	if err != nil {
		t.Fatal(err)
	}
	if !a.prepareCore || a.coreVersion != "v0.4.6" {
		t.Fatalf("prepare arguments changed: %+v", a)
	}
	if err := prepareCore(context.Background(), a); err == nil || !strings.Contains(err.Error(), "授权 Key 文件") {
		t.Fatalf("preparation proceeded without candidate Key: %v", err)
	}
}

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
