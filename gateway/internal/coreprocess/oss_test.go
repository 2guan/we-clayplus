package coreprocess

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/we-clay/we-clay/gateway/internal/config"
)

func ossFixture(t *testing.T, mode string) (downloader, config.CoreConfig, *atomic.Int32) {
	t.Helper()
	t.Setenv("OSS_ACCESS_KEY_ID", "synthetic-id")
	t.Setenv("OSS_ACCESS_KEY_SECRET", "synthetic-secret")
	t.Setenv("OSS_SESSION_TOKEN", "synthetic-sts")
	t.Setenv("WE_CLAY_TEST_DOWNLOAD_TOKEN", "synthetic-github-token")
	content := []byte("synthetic OSS binary")
	name := "we-clay-core_v1.2.3_darwin_arm64"
	a := map[string]any{"name": name, "goos": "darwin", "goarch": "arm64", "size": len(content), "sha256": fmt.Sprintf("%x", sha256.Sum256(content))}
	m := map[string]any{"schema_version": 1, "protocol_version": 1, "version": "v1.2.3", "assets": []any{a}}
	switch mode {
	case "wrong-version":
		m["version"] = "v2.0.0"
	case "wrong-protocol":
		m["protocol_version"] = 2
	case "wrong-platform":
		a["goos"] = "linux"
	case "duplicate":
		m["assets"] = []any{a, a}
	case "escape":
		a["name"] = "../outside"
	case "size":
		a["size"] = maxBinarySize + 1
	}
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.HasPrefix(r.Header.Get("Authorization"), "OSS4-HMAC-SHA256 ") || r.Header.Get("X-Oss-Security-Token") != "synthetic-sts" {
			t.Error("OSS V4/STS authentication missing")
		}
		if strings.Contains(r.Header.Get("Authorization"), "synthetic-github-token") {
			t.Error("GitHub credential sent to OSS")
		}
		if mode == "forbidden" {
			w.WriteHeader(403)
			fmt.Fprint(w, "<Error><Code>AccessDenied</Code><Message>synthetic-secret</Message></Error>")
			return
		}
		if mode == "redirect" {
			http.Redirect(w, r, "https://unrelated.invalid/private", 302)
			return
		}
		switch strings.TrimPrefix(r.URL.Path, "/synthetic-bucket") {
		case "/core/v1.2.3/manifest.json":
			json.NewEncoder(w).Encode(m)
		case "/core/v1.2.3/" + name:
			if mode == "corrupt" {
				w.Write([]byte("invalid binary"))
				return
			}
			w.Write(content)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	cfg, err := (config.CoreConfig{Version: "v1.2.3", CacheDir: t.TempDir(), DataDir: t.TempDir(), GitHubTokenEnv: "WE_CLAY_TEST_DOWNLOAD_TOKEN", OSS: &config.OSSConfig{Bucket: "synthetic-bucket", Region: "cn-beijing", Endpoint: server.URL, CName: true}}).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return downloader{client: server.Client(), baseURL: "https://api.github.com", goos: "darwin", goarch: "arm64"}, cfg, &calls
}

func TestOSSPrivateDownloadAndOfflineReuse(t *testing.T) {
	d, c, calls := ossFixture(t, "")
	path, err := d.ensure(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("unexpected requests: %d", calls.Load())
	}
	t.Setenv("OSS_ACCESS_KEY_ID", "")
	t.Setenv("OSS_ACCESS_KEY_SECRET", "")
	if again, err := d.ensure(context.Background(), c); err != nil || again != path || calls.Load() != 2 {
		t.Fatal("offline reuse failed", err)
	}
	if err = os.WriteFile(path, []byte("modified binary"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = d.ensure(context.Background(), c); err == nil {
		t.Fatal("accepted corrupted cached binary")
	}
}

func TestOSSRejectsInvalidPublicationWithoutExecutable(t *testing.T) {
	for _, mode := range []string{"wrong-version", "wrong-protocol", "wrong-platform", "duplicate", "escape", "size", "corrupt", "forbidden", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			d, c, _ := ossFixture(t, mode)
			if _, err := d.ensure(context.Background(), c); err == nil || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("must reject without leaking response: %v", err)
			}
			entries, _ := os.ReadDir(c.CacheDir)
			if len(entries) != 0 {
				t.Fatal("left executable or partial cache")
			}
		})
	}
}

func TestOSSDownloadSourceAndCredentialsIsolation(t *testing.T) {
	_, c, _ := ossFixture(t, "")
	original := cacheDirectory(c, "asset")
	r := independentRecord{Repository: c.Repository, Source: ossSource(c), Version: c.Version, Address: "127.0.0.1:8088", LoginAddress: c.LoginAddress}
	copyOSS := *c.OSS
	copyOSS.Bucket = "another-bucket"
	c.OSS = &copyOSS
	if original == cacheDirectory(c, "asset") {
		t.Fatal("different bucket shared cache")
	}
	if matchIndependent(&r, config.Config{Core: &c, CoreAddress: r.Address}) == nil {
		t.Fatal("different bucket reused running core")
	}
	c.OSS = nil
	if original == cacheDirectory(c, "asset") {
		t.Fatal("GitHub and OSS shared cache")
	}
	env := childEnv([]string{"OSS_ACCESS_KEY_ID=synthetic-id", "OSS_ACCESS_KEY_SECRET=synthetic-secret", "OSS_SESSION_TOKEN=synthetic-sts", "SDK_SOURCE_TOKEN=synthetic-source", "PATH=/synthetic/bin"}, "local-api-token", "WE_CLAY_GITHUB_TOKEN")
	for _, s := range env {
		if strings.HasPrefix(s, "OSS_") || strings.HasPrefix(s, "SDK_SOURCE_TOKEN=") {
			t.Fatal("download credential inherited by core")
		}
	}
	if len(env) != 2 {
		t.Fatal("unexpected child environment")
	}
	if filepath.Base(original) == "" {
		t.Fatal("missing cache namespace")
	}
}
