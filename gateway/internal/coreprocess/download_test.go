package coreprocess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func fixtureDownload(t *testing.T, manifest bool, broken bool, selectedName ...string) (downloader, config.CoreConfig, *atomic.Int32) {
	t.Helper()
	c, err := (config.CoreConfig{Version: "v1.2.3", CacheDir: t.TempDir(), DataDir: t.TempDir(), GitHubTokenEnv: "WE_CLAY_TEST_DOWNLOAD_TOKEN"}).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(c.GitHubTokenEnv, "synthetic-token")
	content := []byte("synthetic binary bytes")
	name := "we-clay-core-darwin-arm64"
	if len(selectedName) != 0 {
		name = selectedName[0]
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Error("missing API authentication")
		}
		switch r.URL.Path {
		case "/repos/we-clay/core-release/releases/tags/v1.2.3":
			a := asset{ID: 1, Name: name, Size: int64(len(content)), State: "uploaded", Digest: "sha256:" + digest}
			if manifest {
				a.Digest = ""
			}
			json.NewEncoder(w).Encode(release{Tag: c.Version, Assets: []asset{a, {ID: 2, Name: "SHA256SUMS", State: "uploaded"}}})
		case "/repos/we-clay/core-release/releases/assets/1":
			if r.Header.Get("Accept") != "application/octet-stream" {
				t.Error("not an asset request")
			}
			if broken {
				w.Write([]byte("bad"))
				return
			}
			w.Write(content)
		case "/repos/we-clay/core-release/releases/assets/2":
			fmt.Fprintf(w, "%s  %s\n", digest, name)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return downloader{client: server.Client(), baseURL: server.URL, goos: "darwin", goarch: "arm64"}, c, &calls
}

func TestVersionedReleaseAssets(t *testing.T) {
	for _, platform := range [][2]string{{"darwin", "arm64"}, {"windows", "amd64"}} {
		for _, manifest := range []bool{false, true} {
			name := "we-clay-core_v1.2.3_" + platform[0] + "_" + platform[1]
			if platform[0] == "windows" {
				name += ".exe"
			}
			t.Run(fmt.Sprintf("%s/manifest=%t", name, manifest), func(t *testing.T) {
				d, c, calls := fixtureDownload(t, manifest, false, name)
				d.goos, d.goarch = platform[0], platform[1]
				path, err := d.ensure(context.Background(), c)
				if err != nil || filepath.Base(path) != name {
					t.Fatalf("versioned download: %s %v", path, err)
				}
				initial := calls.Load()
				d.baseURL = "http://127.0.0.1:1"
				if again, err := d.ensure(context.Background(), c); err != nil || again != path || calls.Load() != initial {
					t.Fatalf("versioned cache: %s %v", again, err)
				}
			})
		}
	}
	for _, wrong := range []string{"we-clay-core_v9.9.9_darwin_arm64", "we-clay-core_v1.2.3_linux_arm64", "we-clay-core_v1.2.3_darwin_arm64.tar.gz"} {
		d, c, _ := fixtureDownload(t, false, false, wrong)
		if _, err := d.ensure(context.Background(), c); err == nil {
			t.Errorf("accepted wrong release/platform/format %q", wrong)
		}
	}
}

func TestDownloadVerifyOfflineReuseAndRejectTampering(t *testing.T) {
	for _, manifest := range []bool{false, true} {
		t.Run(fmt.Sprint(manifest), func(t *testing.T) {
			d, c, calls := fixtureDownload(t, manifest, false)
			path, err := d.ensure(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			initial := calls.Load()
			if initial < 2 {
				t.Fatal("did not download")
			}
			d.baseURL = "http://127.0.0.1:1" // 再次启动必须不访问网络。
			if again, err := d.ensure(context.Background(), c); err != nil || again != path {
				t.Fatalf("cache: %s %v", again, err)
			}
			if calls.Load() != initial {
				t.Fatal("cache hit used network")
			}
			if err = os.WriteFile(path, []byte("modified binary bytes"), 0700); err != nil {
				t.Fatal(err)
			}
			if _, err = d.ensure(context.Background(), c); err == nil || !strings.Contains(err.Error(), "缓存无效") {
				t.Fatalf("accepted corrupt cache: %v", err)
			}
		})
	}
}

func TestIncompleteDownloadNeverPublishesExecutable(t *testing.T) {
	d, c, _ := fixtureDownload(t, false, true)
	if _, err := d.ensure(context.Background(), c); err == nil {
		t.Fatal("accepted bad bytes")
	}
	entries, err := os.ReadDir(c.CacheDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial cache left: %v %v", entries, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = d.ensure(ctx, c); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestMissingPlatformAndCacheNamespace(t *testing.T) {
	d, c, _ := fixtureDownload(t, false, false)
	if _, err := d.ensure(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	d.goos = "linux"
	if _, err := d.ensure(context.Background(), c); err == nil || !strings.Contains(err.Error(), "we-clay-core-linux-arm64") {
		t.Fatalf("wrong platform reused: %v", err)
	}
	d.goos = "darwin"
	c.Version = "v9.9.9"
	if _, err := d.ensure(context.Background(), c); err == nil {
		t.Fatal("wrong version reused")
	}
	if assetName("windows", "amd64") != "we-clay-core-windows-amd64.exe" {
		t.Fatal("windows asset suffix")
	}
}

func TestRedirectStripsTokenAndRefusesDowngrade(t *testing.T) {
	var received atomic.Bool
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Store(true)
		if r.Header.Get("Authorization") != "" {
			t.Error("token leaked to attachment server")
		}
		w.Write([]byte("ok"))
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/signed?signature=synthetic", http.StatusFound)
	}))
	defer source.Close()
	client := target.Client()
	client.CheckRedirect = checkRedirect
	d := downloader{client: client, baseURL: source.URL}
	res, err := d.get(context.Background(), "/asset", "synthetic-token", true)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if !received.Load() {
		t.Fatal("redirect not followed")
	}
	req, _ := http.NewRequest("GET", "http://example.test/file", nil)
	if checkRedirect(req, nil) == nil {
		t.Fatal("accepted plaintext redirect")
	}
}

func TestCacheReceiptCannotEscapeDirectory(t *testing.T) {
	dir := t.TempDir()
	want := receipt{Repository: "example/release", Version: "v1.0.0", Asset: "expected"}
	digest := sha256.Sum256([]byte("synthetic"))
	got := want
	got.Asset = "../escape"
	got.Size = 9
	got.SHA256 = hex.EncodeToString(digest[:])
	raw, _ := json.Marshal(got)
	os.WriteFile(filepath.Join(dir, "receipt.json"), raw, 0600)
	if _, err := cached(context.Background(), dir, want); err == nil {
		t.Fatal("accepted escaped path")
	}
}
