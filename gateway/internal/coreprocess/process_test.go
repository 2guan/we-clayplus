package coreprocess

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/we-clay/we-clay/gateway/internal/config"
)

func TestSessionStageDoesNotDiscardExistingState(t *testing.T) {
	for _, name := range []string{"session.json", "session.info"} {
		c := config.CoreConfig{DataDir: t.TempDir(), Stage: "auto"}
		if stage, err := loginStage(c); err != nil || stage != "login" {
			t.Fatal(stage, err)
		}
		if err := os.WriteFile(filepath.Join(c.DataDir, name), []byte("opaque saved state"), 0600); err != nil {
			t.Fatal(err)
		}
		if stage, err := loginStage(c); err != nil || stage != "resume" {
			t.Fatal(stage, err)
		}
		c.Stage = "login"
		if stage, err := loginStage(c); err != nil || stage != "login" {
			t.Fatal(stage, err)
		}
	}
}
