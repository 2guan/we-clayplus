package gateway_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/gateway"
)

func TestIndependentCoreDeviceTypeArgumentAndReuse(t *testing.T) {
	for _, typ := range []string{"mac", "win"} {
		t.Run(typ, func(t *testing.T) {
			cfg := managedFixture(t, "normal")
			cfg.Core.KeepAlive = true
			cfg.Core.DeviceType = typ
			cleanupIndependent(t, cfg)
			independentRound(t, cfg)
			pid, token := independentRecord(t, cfg)
			raw, err := os.ReadFile(filepath.Join(cfg.Core.DataDir, "fixture-device-type"))
			if err != nil || string(raw) != typ {
				t.Fatalf("core received type %q: %v", raw, err)
			}
			recordPath := filepath.Join(cfg.Core.DataDir, "gateway-core.json")
			raw, err = os.ReadFile(recordPath)
			if err != nil {
				t.Fatal(err)
			}
			var record map[string]any
			if err := json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			if record["device_type"] != typ {
				t.Fatal("process type not recorded")
			}
			if typ == "mac" {
				// Records written before device selection always launched Mac.
				delete(record, "device_type")
				raw, err = json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(recordPath, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			changed := cfg
			core := *cfg.Core
			changed.Core = &core
			changed.Core.DeviceType = "mac"
			if typ == "mac" {
				changed.Core.DeviceType = "win"
			}
			if err := gateway.Run(context.Background(), changed, gateway.WithoutPlugins()); err == nil || !strings.Contains(err.Error(), "设备类型") {
				t.Fatalf("different device type reused core: %v", err)
			}
			if err := gateway.StopCore(context.Background(), changed); err == nil || !strings.Contains(err.Error(), "设备类型") {
				t.Fatalf("different device type stopped core: %v", err)
			}
			after, err := os.ReadFile(recordPath)
			if err != nil || string(after) != string(raw) {
				t.Fatal("mismatch changed process record")
			}
			independentRound(t, cfg)
			nextPID, nextToken := independentRecord(t, cfg)
			if nextPID != pid || nextToken != token {
				t.Fatal("reconnect replaced process or token")
			}
			if err := gateway.StopCore(context.Background(), cfg); err != nil {
				t.Fatal(err)
			}
		})
	}
}
