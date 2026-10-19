package plugin

import (
	"slices"
	"testing"
)

func TestExternalPluginEnvironmentExcludesParentCredentials(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin", "HOME=/tmp/plugin-home", "LANG=zh_CN.UTF-8",
		"WE_CLAY_AUTH_KEY=license-value", "OSS_ACCESS_KEY_SECRET=oss-value",
		"OPENAI_API_KEY=model-value", "CUSTOM_LICENSE_VARIABLE=custom-value",
		"WE_CLAY_CORE_TOKEN=local-token", "WE_CLAY_GITHUB_TOKEN=github-token", "LC_CUSTOM=hidden-value",
		"CUSTOM_SETTING=ordinary-value", "LC_TIME=en_US.UTF-8",
	}
	got := pluginProcessEnv(parent, "LC_CUSTOM")
	for _, expected := range parent[:3] {
		if !slices.Contains(got, expected) {
			t.Fatalf("required OS environment missing: %s", expected)
		}
	}
	for _, secret := range []string{parent[3], parent[4], parent[7], parent[8], parent[9]} {
		if slices.Contains(got, secret) {
			t.Fatal("parent credential leaked into plugin environment")
		}
	}
	for _, ordinary := range []string{parent[5], parent[6], parent[10], parent[11]} {
		if !slices.Contains(got, ordinary) {
			t.Fatalf("ordinary plugin environment missing: %s", ordinary)
		}
	}
	if slices.Contains(pluginProcessEnv([]string{"LANG=license-value"}, "LANG"), "LANG=license-value") {
		t.Fatal("custom authorization variable escaped the plugin environment filter")
	}
}
