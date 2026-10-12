package gateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/we-clay/we-clay/gateway"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// 核对整个能力目录：新接口不能漏记；占位必须明确报错，不能偷偷请求 core。
func TestEveryCapabilityListedAndPlaceholdersMakeNoRequest(t *testing.T) {
	raw, err := os.ReadFile("capabilities.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]struct{ Enabled, Placeholder []string }
	if err = json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	s, cfg := directFixture(t)
	finished := errors.New("fixture complete")
	err = gateway.Run(context.Background(), cfg, gateway.WithoutPlugins(), gateway.WithHandlers(gateway.Handlers{OnReady: func(_ context.Context, c *gateway.Client) error {
		value := reflect.ValueOf(c).Elem()
		for domain, entry := range catalog {
			field := strings.ToUpper(domain[:1]) + domain[1:]
			if domain == "cdn" {
				field = "CDN"
			}
			ability := value.FieldByName(field)
			if !ability.IsValid() {
				return fmt.Errorf("missing domain %s", domain)
			}
			listed := map[string]bool{}
			for _, name := range append(append([]string{}, entry.Enabled...), entry.Placeholder...) {
				if listed[name] {
					return fmt.Errorf("duplicate %s.%s", domain, name)
				}
				listed[name] = true
				if !ability.MethodByName(name).IsValid() {
					return fmt.Errorf("missing method %s.%s", domain, name)
				}
			}
			if len(listed) != ability.NumMethod() {
				return fmt.Errorf("unlisted methods in %s", domain)
			}
			for _, name := range entry.Placeholder {
				method := ability.MethodByName(name)
				args := make([]reflect.Value, method.Type().NumIn())
				for i := range args {
					args[i] = reflect.Zero(method.Type().In(i))
				}
				out := method.Call(args)
				failure, ok := out[len(out)-1].Interface().(error)
				if !ok || !errors.Is(failure, gateway.ErrNotImplemented) || status.Code(failure) != codes.Unimplemented {
					return fmt.Errorf("%s.%s did not return placeholder error: %v", domain, name, failure)
				}
			}
		}
		return finished
	}}))
	if !errors.Is(err, finished) {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.writes) != 0 {
		t.Fatal("placeholder sent business requests")
	}
}
