package coreprocess

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/we-clay/we-clay/gateway/internal/config"
)

func signedOSSFixture(t *testing.T, mode string) (downloader, config.CoreConfig, *atomic.Int32) {
	t.Helper()
	t.Setenv("OSS_ACCESS_KEY_ID", "")
	t.Setenv("OSS_ACCESS_KEY_SECRET", "")
	t.Setenv("SYNTHETIC_AUTH_KEY", "synthetic-key")
	content := []byte("synthetic signed OSS binary")
	name := "we-clay-core_v1.2.3_darwin_arm64"
	manifest := map[string]any{
		"schema_version": 1, "protocol_version": 1, "version": "v1.2.3",
		"assets": []any{map[string]any{"name": name, "goos": "darwin", "goarch": "arm64", "size": len(content), "sha256": fmt.Sprintf("%x", sha256.Sum256(content))}},
	}
	if mode == "wrong-manifest" {
		manifest["version"] = "v9.9.9"
	}
	var server *httptest.Server
	var calls atomic.Int32
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/we-clay-auth/v1/download":
			if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer synthetic-key" {
				t.Error("download authorization request is invalid")
			}
			var body struct {
				Product string `json:"product"`
				Version string `json:"version"`
				OS      string `json:"goos"`
				Arch    string `json:"goarch"`
				Asset   string `json:"asset"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Product != "core" || body.Version != "v1.2.3" || body.OS != "darwin" || body.Arch != "arm64" || body.Asset != "bundle" {
				t.Error("download authorization body is invalid")
			}
			if mode == "denied" {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, "synthetic-key")
				return
			}
			if mode == "auth-redirect" {
				http.Redirect(w, r, "https://unrelated.invalid/private", http.StatusFound)
				return
			}
			expiry := time.Now().Add(5 * time.Minute)
			if mode == "expired" {
				expiry = time.Now().Add(-time.Minute)
			}
			manifestURL := server.URL + "/core/v1.2.3/manifest.json?X-Oss-Signature=synthetic-signature"
			if mode == "wrong-host" {
				manifestURL = "https://unrelated.invalid/core/v1.2.3/manifest.json?X-Oss-Signature=synthetic-signature"
			}
			json.NewEncoder(w).Encode(signedDownload{
				ManifestURL: manifestURL,
				AssetURL:    server.URL + "/core/v1.2.3/" + name + "?X-Oss-Signature=synthetic-signature",
				ExpiresAt:   expiry.UTC().Format(time.RFC3339),
			})
		case "/core/v1.2.3/manifest.json", "/core/v1.2.3/" + name:
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || r.Header.Get("X-Oss-Security-Token") != "" || r.URL.Query().Get("X-Oss-Signature") != "synthetic-signature" {
				t.Error("credential leaked to OSS or signature missing")
			}
			if r.URL.Path == "/core/v1.2.3/manifest.json" {
				json.NewEncoder(w).Encode(manifest)
				return
			}
			if mode == "asset-redirect" {
				http.Redirect(w, r, "https://unrelated.invalid/private", http.StatusFound)
				return
			}
			if mode == "corrupt" {
				w.Write([]byte("corrupt"))
				return
			}
			w.Write(content)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	cfg, err := (config.CoreConfig{
		Version: "v1.2.3", CacheDir: t.TempDir(), DataDir: t.TempDir(),
		AuthKeyEnv: "SYNTHETIC_AUTH_KEY", DownloadAuthBaseURL: server.URL + "/we-clay-auth",
		OSS: &config.OSSConfig{Bucket: "synthetic-bucket", Region: "cn-beijing", Endpoint: server.URL, CName: true},
	}).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	return downloader{client: server.Client(), goos: "darwin", goarch: "arm64"}, cfg, &calls
}

func TestSignedOSSDownloadAndOfflineReuse(t *testing.T) {
	d, cfg, calls := signedOSSFixture(t, "")
	path, err := d.ensure(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected authorization, manifest, asset requests; got %d", calls.Load())
	}
	t.Setenv(cfg.AuthKeyEnv, "")
	if again, err := d.ensure(context.Background(), cfg); err != nil || again != path || calls.Load() != 3 {
		t.Fatal("offline cache reuse failed", err)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ensure(context.Background(), cfg); err == nil {
		t.Fatal("accepted corrupted cached binary")
	}
}

func TestSignedOSSRejectsInvalidLinksAndAssets(t *testing.T) {
	for _, mode := range []string{"denied", "auth-redirect", "expired", "wrong-host", "wrong-manifest", "asset-redirect", "corrupt"} {
		t.Run(mode, func(t *testing.T) {
			d, cfg, _ := signedOSSFixture(t, mode)
			_, err := d.ensure(context.Background(), cfg)
			if err == nil || strings.Contains(err.Error(), "synthetic-key") || strings.Contains(err.Error(), "synthetic-signature") {
				t.Fatalf("must reject without leaking credentials: %v", err)
			}
			entries, _ := os.ReadDir(cfg.CacheDir)
			if len(entries) != 0 {
				t.Fatal("left a partial executable")
			}
		})
	}
}

func TestSignedOSSRequiresCompleteCredentialsOrKey(t *testing.T) {
	d, cfg, _ := signedOSSFixture(t, "")
	t.Setenv(cfg.AuthKeyEnv, "")
	if _, err := d.ensure(context.Background(), cfg); err == nil {
		t.Fatal("accepted download without credentials")
	}
	t.Setenv("OSS_ACCESS_KEY_ID", "synthetic-id")
	if _, err := d.ensure(context.Background(), cfg); err == nil {
		t.Fatal("accepted partial OSS credentials")
	}
}

func TestSignedOSSURLBoundToBucketAndObject(t *testing.T) {
	oss, err := (config.OSSConfig{Bucket: "synthetic-bucket", Region: "cn-beijing", Prefix: "core"}).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	good := "https://synthetic-bucket.oss-cn-beijing.aliyuncs.com/core/v1.2.3/manifest.json?x-oss-signature=synthetic"
	if !signedOSSURLAllowed(good, &oss, "v1.2.3", "manifest.json") {
		t.Fatal("rejected expected Bucket URL")
	}
	for _, bad := range []string{
		"http://synthetic-bucket.oss-cn-beijing.aliyuncs.com/core/v1.2.3/manifest.json?x-oss-signature=synthetic",
		"https://other-bucket.oss-cn-beijing.aliyuncs.com/core/v1.2.3/manifest.json?x-oss-signature=synthetic",
		"https://synthetic-bucket.oss-cn-beijing.aliyuncs.com/core/v1.2.3/other?x-oss-signature=synthetic",
		"https://oss-cn-beijing.aliyuncs.com/core/v1.2.3/manifest.json?x-oss-signature=synthetic",
		"https://synthetic-bucket.oss-cn-beijing.aliyuncs.com/core/v1.2.3/manifest.json",
	} {
		if signedOSSURLAllowed(bad, &oss, "v1.2.3", "manifest.json") {
			t.Fatal("accepted unexpected signed URL")
		}
	}
}
