package control

import (
	"encoding/json"
	"os"
	"testing"
)

func TestEveryDeclaredCapabilityHasAnExplicitAIStatus(t *testing.T) {
	raw, e := os.ReadFile("../../gateway/capabilities.json")
	if e != nil {
		t.Fatal(e)
	}
	var caps map[string]Capability
	if e = json.Unmarshal(raw, &caps); e != nil {
		t.Fatal(e)
	}
	excluded := map[string]bool{"user.GetSafetyInfo": true, "user.GetBoundHardDevices": true}
	direct, backend, manual := 0, 0, 0
	for domain, cap := range caps {
		for _, name := range cap.Enabled {
			op := domain + "." + name
			_, ready := AIActions[op]
			support := AIBackendOperations[op]
			restricted := domain == "payment" || excluded[op] || AIDeferredOperations[op]
			count := 0
			if ready {
				count++
				direct++
			}
			if support {
				count++
				backend++
			}
			if restricted {
				count++
				manual++
			}
			if count != 1 {
				t.Error("能力状态缺失或重叠", op)
			}
		}
		for _, name := range cap.Placeholder {
			if _, ok := AIActions[domain+"."+name]; ok {
				t.Error("暴露了占位接口", domain, name)
			}
		}
	}
	if direct != 42 || backend != 11 || manual != 14 {
		t.Fatal(direct, backend, manual)
	}
	for op, action := range AIActions {
		if op != action.Operation || action.Parameters["additionalProperties"] != false {
			t.Error("工具没有严格参数边界", op)
		}
	}
}
