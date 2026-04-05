package cmd

import (
	"strings"
	"testing"
)

func TestModelAliasInvokesModelsCommand(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())

	stdout, _, err := executeRootCommandForTest(t, "model")
	if err != nil {
		t.Fatalf("model alias error: %v", err)
	}
	if !strings.Contains(stdout, "Configured Models") {
		t.Fatalf("expected models output from alias, got %q", stdout)
	}
}
