package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"crawler-ai/internal/config"
	"crawler-ai/internal/oauth"
	"crawler-ai/internal/providercatalog"
)

func TestProvidersMutationCommandsPersistConfig(t *testing.T) {
	workspaceDir := t.TempDir()
	t.Setenv("APPDATA", t.TempDir())

	stdout, _, err := executeRootCommandForTest(t,
		"--cwd", workspaceDir,
		"providers", "headers", "set",
		"--role", "orchestrator",
		"--scope", "workspace",
		"--key", "X-Test",
		"--value", "value",
	)
	if err != nil {
		t.Fatalf("providers headers set error: %v", err)
	}
	if !strings.Contains(stdout, "orchestrator: set header X-Test in workspace config") {
		t.Fatalf("expected header set output, got %q", stdout)
	}

	stdout, _, err = executeRootCommandForTest(t,
		"--cwd", workspaceDir,
		"providers", "body", "set",
		"--role", "orchestrator",
		"--scope", "workspace",
		"--key", "metadata.trace_id",
		"--value", "req-1",
	)
	if err != nil {
		t.Fatalf("providers body set error: %v", err)
	}
	if !strings.Contains(stdout, "orchestrator: set extra body metadata.trace_id in workspace config") {
		t.Fatalf("expected body set output, got %q", stdout)
	}

	stdout, _, err = executeRootCommandForTest(t,
		"--cwd", workspaceDir,
		"providers", "options", "set",
		"--role", "orchestrator",
		"--scope", "workspace",
		"--key", "reasoning.effort",
		"--value", "medium",
	)
	if err != nil {
		t.Fatalf("providers options set error: %v", err)
	}
	if !strings.Contains(stdout, "orchestrator: set provider option reasoning.effort in workspace config") {
		t.Fatalf("expected options set output, got %q", stdout)
	}

	loaded, err := config.LoadForWorkingDir(workspaceDir)
	if err != nil {
		t.Fatalf("LoadForWorkingDir() error: %v", err)
	}
	if loaded.Models.Orchestrator.ExtraHeaders["X-Test"] != "value" {
		t.Fatalf("expected persisted header, got %#v", loaded.Models.Orchestrator.ExtraHeaders)
	}
	metadata, _ := loaded.Models.Orchestrator.ExtraBody["metadata"].(map[string]any)
	if metadata["trace_id"] != "req-1" {
		t.Fatalf("expected persisted body value, got %#v", loaded.Models.Orchestrator.ExtraBody)
	}
	reasoning, _ := loaded.Models.Orchestrator.ProviderOptions["reasoning"].(map[string]any)
	if reasoning["effort"] != "medium" {
		t.Fatalf("expected persisted option value, got %#v", loaded.Models.Orchestrator.ProviderOptions)
	}

	for _, args := range [][]string{
		{"--cwd", workspaceDir, "providers", "headers", "unset", "--role", "orchestrator", "--scope", "workspace", "--key", "X-Test"},
		{"--cwd", workspaceDir, "providers", "body", "unset", "--role", "orchestrator", "--scope", "workspace", "--key", "metadata.trace_id"},
		{"--cwd", workspaceDir, "providers", "options", "unset", "--role", "orchestrator", "--scope", "workspace", "--key", "reasoning.effort"},
	} {
		if _, _, err := executeRootCommandForTest(t, args...); err != nil {
			t.Fatalf("mutation unset command %v error: %v", args, err)
		}
	}

	loaded, err = config.LoadForWorkingDir(workspaceDir)
	if err != nil {
		t.Fatalf("LoadForWorkingDir() error after unset: %v", err)
	}
	if len(loaded.Models.Orchestrator.ExtraHeaders) != 0 {
		t.Fatalf("expected headers to be removed, got %#v", loaded.Models.Orchestrator.ExtraHeaders)
	}
	if len(loaded.Models.Orchestrator.ExtraBody) != 0 {
		t.Fatalf("expected body to be removed, got %#v", loaded.Models.Orchestrator.ExtraBody)
	}
	if len(loaded.Models.Orchestrator.ProviderOptions) != 0 {
		t.Fatalf("expected options to be removed, got %#v", loaded.Models.Orchestrator.ProviderOptions)
	}
}

func TestProviderAliasesSupportCompactMutationCommands(t *testing.T) {
	workspaceDir := t.TempDir()
	t.Setenv("APPDATA", t.TempDir())

	stdout, _, err := executeRootCommandForTest(t,
		"--cwd", workspaceDir,
		"provider", "header", "set",
		"--role", "orchestrator",
		"--scope", "workspace",
		"--key", "X-Alias",
		"--value", "alias-value",
	)
	if err != nil {
		t.Fatalf("provider alias mutation error: %v", err)
	}
	if !strings.Contains(stdout, "orchestrator: set header X-Alias in workspace config") {
		t.Fatalf("expected alias mutation output, got %q", stdout)
	}

	loaded, err := config.LoadForWorkingDir(workspaceDir)
	if err != nil {
		t.Fatalf("LoadForWorkingDir() error: %v", err)
	}
	if loaded.Models.Orchestrator.ExtraHeaders["X-Alias"] != "alias-value" {
		t.Fatalf("expected persisted alias header, got %#v", loaded.Models.Orchestrator.ExtraHeaders)
	}
}

func TestUpdateProvidersCommandRefreshesCatalogOverlay(t *testing.T) {
	workspaceDir := t.TempDir()
	t.Setenv("APPDATA", t.TempDir())

	sourcePath := filepath.Join(t.TempDir(), "providers.json")
	data := `[
	  {
	    "id": "openai",
	    "display_name": "OpenAI Updated",
	    "auth_mode": "api_key",
	    "requires_base_url": true,
	    "requires_api_key": true,
	    "default_base_url": "https://example.com/v1",
	    "capabilities": {"streaming": true, "system_prompt": true},
	    "known_models": ["gpt-test"]
	  },
	  {
	    "id": "unsupported",
	    "display_name": "Unsupported",
	    "auth_mode": "none"
	  }
	]`
	if err := os.WriteFile(sourcePath, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	stdout, _, err := executeRootCommandForTest(t, "--cwd", workspaceDir, "update-providers", sourcePath)
	if err != nil {
		t.Fatalf("update-providers error: %v", err)
	}
	if !strings.Contains(stdout, "updated provider catalog from "+sourcePath) {
		t.Fatalf("expected source in output, got %q", stdout)
	}
	if !strings.Contains(stdout, "applied 1 provider definitions") {
		t.Fatalf("expected applied count in output, got %q", stdout)
	}
	if !strings.Contains(stdout, "ignored unsupported providers: unsupported") {
		t.Fatalf("expected ignored providers in output, got %q", stdout)
	}

	definition, ok := providercatalog.Get("openai")
	if !ok {
		t.Fatal("expected updated openai definition")
	}
	if definition.DisplayName != "OpenAI Updated" {
		t.Fatalf("expected updated display name, got %s", definition.DisplayName)
	}
	if definition.DefaultBaseURL != "https://example.com/v1" {
		t.Fatalf("expected updated base url, got %s", definition.DefaultBaseURL)
	}
	if _, err := os.Stat(providercatalog.OverlayPath()); err != nil {
		t.Fatalf("expected overlay file to exist: %v", err)
	}
}

func executeRootCommandForTest(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	resetCommandGlobalsForTest()
	defer oauth.SetDefaultConfigDir("")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	return stdout.String(), stderr.String(), err
}

func resetCommandGlobalsForTest() {
	debug = false
	yolo = false
	cwd = ""
	dataDir = ""
	runSessionID = ""
	runContinue = false
	runFormat = "text"
	runAutonomous = false
	oauth.SetDefaultConfigDir("")
	if flag := rootCmd.PersistentFlags().Lookup("data-dir"); flag != nil {
		flag.Changed = false
	}
	if flag := runCmd.Flags().Lookup("session"); flag != nil {
		flag.Changed = false
	}
	if flag := runCmd.Flags().Lookup("continue"); flag != nil {
		flag.Changed = false
	}
	if flag := runCmd.Flags().Lookup("format"); flag != nil {
		flag.Changed = false
	}
	if flag := runCmd.Flags().Lookup("autonomous"); flag != nil {
		flag.Changed = false
	}
	if flag := rootCmd.PersistentFlags().Lookup("debug"); flag != nil {
		flag.Changed = false
	}
	if flag := rootCmd.PersistentFlags().Lookup("yolo"); flag != nil {
		flag.Changed = false
	}
	if flag := rootCmd.PersistentFlags().Lookup("cwd"); flag != nil {
		flag.Changed = false
	}
	statsFormat = "text"
	statsWorkspace = ""
	statsLimit = 10
	sessionsShowFormat = "text"
	sessionsTasksFormat = "text"
	sessionsTasksStatus = ""
	sessionsUsageFormat = "text"
	sessionsHistoryFormat = "text"
	sessionsHistorySection = "all"
	sessionsHistoryKind = ""
	sessionsTranscriptFormat = "text"
	sessionsDiagnoseFormat = "text"
	sessionsDiagnoseSection = "all"
	sessionsDeleteYes = false
	sessionsExportFormat = "json"
	sessionsExportOutput = ""
	sessionsExportFilter = "full"
	providersRole = ""
	providersAll = false
	providerMutationRole = ""
	providerMutationScope = string(config.ScopeUser)
	providerHeaderKey = ""
	providerBodyKeyPath = ""
	providerOptionKeyPath = ""
	providerMutationValue = ""
}
