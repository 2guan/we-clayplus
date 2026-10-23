package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/we-clay/we-clay/gateway"
)

func TestConsoleControlsIndependentCoreWithoutLosingDashboard(t *testing.T) {
	cfg := managedFixture(t, "normal")
	cfg.Core.KeepAlive = true
	cfg.CoreToken = ""
	cfg.CoreTokenEnv = "-"
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Console.Address = listener.Addr().String()
	listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- gateway.Run(ctx, cfg, gateway.WithCoreConsoleControl()) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("gateway did not stop")
		}
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer stopCancel()
		if err := gateway.StopCore(stopCtx, cfg); err != nil {
			t.Error(err)
		}
	})
	base := "http://" + cfg.Console.Address
	client := &http.Client{Timeout: 3 * time.Second}
	var cookie *http.Cookie
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		body, _ := json.Marshal(map[string]string{"name": "fixture-admin", "password": "fixture-password-123"})
		resp, err := client.Post(base+"/api/auth/setup", "application/json", bytes.NewReader(body))
		if err == nil {
			if resp.StatusCode == 200 && len(resp.Cookies()) > 0 {
				cookie = resp.Cookies()[0]
			}
			resp.Body.Close()
		}
		if cookie != nil {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if cookie == nil {
		t.Fatal("Dashboard did not accept administrator setup")
	}
	coreRequest := func(method, action string) (map[string]any, error) {
		var data []byte
		if method == http.MethodPost {
			data, _ = json.Marshal(map[string]string{"action": action})
		}
		req, _ := http.NewRequest(method, base+"/api/core", bytes.NewReader(data))
		req.AddCookie(cookie)
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("core API status %d: %v", resp.StatusCode, result)
		}
		return result, nil
	}
	waitState := func(want string) {
		t.Helper()
		until := time.Now().Add(12 * time.Second)
		for time.Now().Before(until) {
			view, err := coreRequest(http.MethodGet, "")
			if err == nil && view["state"] == want {
				return
			}
			time.Sleep(40 * time.Millisecond)
		}
		view, err := coreRequest(http.MethodGet, "")
		t.Fatalf("core never reached %s: %v %v", want, view, err)
	}
	waitState("online")
	contactSyncCount := func() int {
		content, err := os.ReadFile(filepath.Join(cfg.Core.DataDir, "fixture-contact-sync-count"))
		if os.IsNotExist(err) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		return bytes.Count(content, []byte("x\n"))
	}
	initialSyncDeadline := time.Now().Add(3 * time.Second)
	for contactSyncCount() == 0 && time.Now().Before(initialSyncDeadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if contactSyncCount() == 0 {
		t.Fatal("initial core session did not start contact sync")
	}
	if _, err := coreRequest(http.MethodPost, "stop"); err != nil {
		t.Fatal(err)
	}
	waitState("stopped")
	stoppedSyncCount := contactSyncCount()
	select {
	case err := <-done:
		t.Fatalf("stopping core also stopped Dashboard: %v", err)
	default:
	}
	if _, err := os.Stat(cfg.Core.DataDir); err != nil {
		t.Fatal("device directory was removed", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.Core.DataDir, "gateway-core.json")); !os.IsNotExist(err) {
		t.Fatal("stopped process record remains")
	}
	if _, err := coreRequest(http.MethodPost, "start"); err != nil {
		t.Fatal(err)
	}
	waitState("online")
	restartedSyncDeadline := time.Now().Add(3 * time.Second)
	for contactSyncCount() <= stoppedSyncCount && time.Now().Before(restartedSyncDeadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if contactSyncCount() <= stoppedSyncCount {
		t.Fatal("restarted core session did not start contact sync")
	}
}

func TestCoreConsoleControlRequiresLocalIndependentCore(t *testing.T) {
	cfg := gateway.Config{CoreAddress: "127.0.0.1:8088"}
	if err := gateway.Run(context.Background(), cfg, gateway.WithCoreConsoleControl()); err == nil {
		t.Fatal("external core accepted lifecycle control")
	}
}

// A paused QR flow is resumed explicitly from the same Dashboard. The fake
// core records the retry and then completes its usual event subscription.
func TestConsoleRetryLoginWithoutOpeningCorePage(t *testing.T) {
	cfg := managedFixture(t, "paused")
	cfg.Core.KeepAlive = true
	cfg.CoreToken, cfg.CoreTokenEnv = "", "-"
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Console.Address = listener.Addr().String()
	listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- gateway.Run(ctx, cfg, gateway.WithCoreConsoleControl()) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("gateway did not stop")
		}
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer stopCancel()
		if err := gateway.StopCore(stopCtx, cfg); err != nil {
			t.Error(err)
		}
	})
	base := "http://" + cfg.Console.Address
	client, cookie := loginConsoleAdmin(t, base)
	waitConsoleCoreState(t, client, cookie, base, "waiting_login")
	request, _ := http.NewRequest(http.MethodPost, base+"/api/wechat-login", bytes.NewReader([]byte(`{}`)))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("retry status %d", response.StatusCode)
	}
	waitConsoleCoreState(t, client, cookie, base, "online")
}

func TestConsoleSurvivesWechatSessionLoss(t *testing.T) {
	cfg := managedFixture(t, "offline")
	cfg.Core.KeepAlive = true
	cfg.CoreToken, cfg.CoreTokenEnv = "", "-"
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Console.Address = listener.Addr().String()
	listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- gateway.Run(ctx, cfg, gateway.WithCoreConsoleControl()) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("gateway did not stop")
		}
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer stopCancel()
		if err := gateway.StopCore(stopCtx, cfg); err != nil {
			t.Error(err)
		}
	})
	base := "http://" + cfg.Console.Address
	client, cookie := loginConsoleAdmin(t, base)
	waitConsoleCoreState(t, client, cookie, base, "online")
	waitConsoleCoreState(t, client, cookie, base, "error")
	select {
	case err := <-done:
		t.Fatalf("session loss stopped Dashboard: %v", err)
	default:
	}
}

func TestConsoleVerificationRoutesAndTicketIsolation(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		mode         string
		operation    string
		payload      string
		requestFiles []string
		wantBody     map[string]string
	}{
		{"pin", "verification-pin", "/api/wechat-login/verify-code", `{"code":"123456"}`, []string{"verification-code"}, map[string]string{"code": "123456"}},
		{"device", "verification-device", "/api/wechat-login/device-confirm", `{}`, []string{"device-confirm-get", "device-confirm-ok"}, map[string]string{"url": fixtureVerificationURL}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := managedFixture(t, scenario.mode)
			cfg.Core.KeepAlive = true
			cfg.CoreToken, cfg.CoreTokenEnv = "", "-"
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			cfg.Console.Address = listener.Addr().String()
			listener.Close()
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- gateway.Run(ctx, cfg, gateway.WithCoreConsoleControl()) }()
			t.Cleanup(func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(10 * time.Second):
					t.Error("gateway did not stop")
				}
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 12*time.Second)
				defer stopCancel()
				if err := gateway.StopCore(stopCtx, cfg); err != nil {
					t.Error(err)
				}
			})
			base := "http://" + cfg.Console.Address
			client, cookie := loginConsoleAdmin(t, base)
			waitConsoleCoreState(t, client, cookie, base, "waiting_login")
			loginView := consoleVerificationRequest(t, client, cookie, http.MethodGet, base+"/api/wechat-login", "")
			if strings.Contains(string(loginView), fixtureVerificationURL) || strings.Contains(string(loginView), "verify_url") {
				t.Fatal("Dashboard exposed core verification URL")
			}
			var view map[string]any
			if err := json.Unmarshal(loginView, &view); err != nil || view["phase"] != "verification_required" {
				t.Fatalf("verification view missing: %s, %v", loginView, err)
			}
			if scenario.name == "pin" && view["pin_required"] != true {
				t.Fatalf("PIN prompt missing: %s", loginView)
			}
			if scenario.name == "device" && view["device_confirm_available"] != true {
				t.Fatalf("device confirmation prompt missing: %s", loginView)
			}
			response := consoleVerificationRequest(t, client, cookie, http.MethodPost, base+scenario.operation, scenario.payload)
			if strings.Contains(string(response), fixtureVerificationURL) {
				t.Fatal("verification response exposed core verification URL")
			}
			for _, name := range scenario.requestFiles {
				record, err := os.ReadFile(filepath.Join(cfg.Core.DataDir, "fixture-"+name+".json"))
				if err != nil {
					t.Fatal("fake core did not receive "+name, err)
				}
				var actual struct {
					Method string            `json:"method"`
					Path   string            `json:"path"`
					Body   map[string]string `json:"body"`
				}
				if err := json.Unmarshal(record, &actual); err != nil {
					t.Fatal(err)
				}
				wantPath := "/api/login/" + name
				if strings.HasPrefix(name, "device-confirm-") {
					wantPath = "/api/login/device-confirm/" + strings.TrimPrefix(name, "device-confirm-")
				}
				if actual.Method != http.MethodPost || actual.Path != wantPath || !reflect.DeepEqual(actual.Body, scenario.wantBody) {
					t.Fatalf("unexpected core request for %s: %#v", name, actual)
				}
			}
		})
	}
}

func consoleVerificationRequest(t *testing.T, client *http.Client, cookie *http.Cookie, method, url, payload string) []byte {
	t.Helper()
	request, err := http.NewRequest(method, url, strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Dashboard status %d: %s", response.StatusCode, raw)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("verification response may be cached")
	}
	return raw
}

func loginConsoleAdmin(t *testing.T, base string) (*http.Client, *http.Cookie) {
	t.Helper()
	client := &http.Client{Timeout: 3 * time.Second}
	until := time.Now().Add(8 * time.Second)
	for time.Now().Before(until) {
		body, _ := json.Marshal(map[string]string{"name": "fixture-admin", "password": "fixture-password-123"})
		response, err := client.Post(base+"/api/auth/setup", "application/json", bytes.NewReader(body))
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 && len(response.Cookies()) > 0 {
				return client, response.Cookies()[0]
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("Dashboard did not accept administrator setup")
	return nil, nil
}

func waitConsoleCoreState(t *testing.T, client *http.Client, cookie *http.Cookie, base, want string) {
	t.Helper()
	until := time.Now().Add(12 * time.Second)
	for time.Now().Before(until) {
		request, _ := http.NewRequest(http.MethodGet, base+"/api/core", nil)
		request.AddCookie(cookie)
		response, err := client.Do(request)
		if err == nil {
			var view struct {
				State string `json:"state"`
			}
			_ = json.NewDecoder(response.Body).Decode(&view)
			response.Body.Close()
			if response.StatusCode == 200 && view.State == want {
				return
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("core did not reach state %s", want)
}
