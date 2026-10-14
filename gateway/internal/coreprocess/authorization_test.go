package coreprocess

import (
	"strings"
	"testing"
)

func TestAuthorizationEnvironmentUsesSelectedKeyWithoutPersistingIt(t *testing.T) {
	t.Setenv("SYNTHETIC_ACCOUNT_AUTH", "synthetic-authorization-value")
	t.Setenv("WE_CLAY_AUTH_KEY", "wrong-default")
	env := authorizationEnv([]string{"SYNTHETIC_ACCOUNT_AUTH=synthetic-authorization-value", "WE_CLAY_AUTH_KEY=wrong-default", "PATH=/bin"}, "SYNTHETIC_ACCOUNT_AUTH")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "SYNTHETIC_ACCOUNT_AUTH=") || strings.Contains(joined, "wrong-default") || !strings.Contains(joined, "WE_CLAY_AUTH_KEY=synthetic-authorization-value") {
		t.Fatal("authorization env selection failed")
	}
	t.Setenv("SYNTHETIC_ACCOUNT_AUTH", "")
	env = authorizationEnv(env, "SYNTHETIC_ACCOUNT_AUTH")
	if strings.Contains(strings.Join(env, "\n"), "WE_CLAY_AUTH_KEY=") {
		t.Fatal("stale key retained")
	}
}
