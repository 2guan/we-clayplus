package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/we-clay/we-clay/sdk/control"
)

type uptimeFixtureHost struct {
	control.Host
	duration time.Duration
	err      error
}

func (h uptimeFixtureHost) CoreUptime(context.Context) (time.Duration, error) {
	return h.duration, h.err
}

func TestDashboardUptimeUsesCoreAcrossInstances(t *testing.T) {
	for i := 0; i < 2; i++ {
		s := newServer(t, false)
		s.host = debugHost{Host: uptimeFixtureHost{duration: 49*time.Hour + 3*time.Minute}, s: s}
		seconds := s.coreUptime(context.Background())
		if seconds == nil || *seconds != 176580 {
			t.Fatal("读取了dashboard启动时间", seconds)
		}
		s.host = debugHost{Host: uptimeFixtureHost{err: fmt.Errorf("core已停止")}, s: s}
		if s.coreUptime(context.Background()) != nil {
			t.Fatal("core不可用时仍显示其他进程时间")
		}
	}
	s := newServer(t, true)
	s.host = uptimeFixtureHost{duration: time.Hour}
	if s.coreUptime(context.Background()) != nil {
		t.Fatal("Mock没有真实core却显示运行时间")
	}
	w := call(t, s, "/api/core-uptime", nil, nil, "")
	var out struct {
		Seconds *int64 `json:"uptime_seconds"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out.Seconds != nil {
		t.Fatal("轻量运行指标响应错误", w.Code, err)
	}
	if call(t, s, "/api/core-uptime", map[string]string{}, nil, "").Code != 405 {
		t.Fatal("指标入口允许非只读请求")
	}
}
