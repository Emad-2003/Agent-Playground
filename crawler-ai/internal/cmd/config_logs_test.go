package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigAliasesPrintConfiguration(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	workspaceDir := t.TempDir()

	stdout, _, err := executeRootCommandForTest(t, "--cwd", workspaceDir, "cfg")
	if err != nil {
		t.Fatalf("cfg command error: %v", err)
	}
	if !strings.Contains(stdout, "crawler-ai configuration") {
		t.Fatalf("expected config output, got %q", stdout)
	}

	stdout, _, err = executeRootCommandForTest(t, "--cwd", workspaceDir, "cfg", "j")
	if err != nil {
		t.Fatalf("cfg j command error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("expected json output from cfg j, got error %v and output %q", err, stdout)
	}
}

func TestLogAliasPrintsFormattedLogLines(t *testing.T) {
	dataDir := t.TempDir()
	logPath := filepath.Join(dataDir, "logs", "crawler-ai.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	line := `{"time":"2026-04-04T12:00:00Z","level":"info","msg":"hello logs","session":"session-1"}`
	if err := os.WriteFile(logPath, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "--data-dir", dataDir, "log")
	if err != nil {
		t.Fatalf("log alias error: %v", err)
	}
	if !strings.Contains(stdout, "INFO") || !strings.Contains(stdout, "hello logs") {
		t.Fatalf("expected formatted log output, got %q", stdout)
	}
}
