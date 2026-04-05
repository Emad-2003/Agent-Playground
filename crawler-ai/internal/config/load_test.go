package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".crawler-ai.json")

	content := `{
		"environment": "production",
		"log_level": "debug",
		"yolo": true,
		"models": {
			"orchestrator": {
				"provider": "mock",
				"model": "custom-model"
			}
		},
		"permissions": {
			"allowed_tools": ["read_file", "grep"],
			"disabled_tools": ["shell"]
		}
	}`

	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Override working directory
	origLookup := lookupWorkingDirectory
	lookupWorkingDirectory = func() (string, error) { return dir, nil }
	defer func() { lookupWorkingDirectory = origLookup }()

	// Clear env vars that might interfere
	t.Setenv("CRAWLER_AI_ENV", "")
	t.Setenv("CRAWLER_AI_LOG_LEVEL", "")
	t.Setenv("CRAWLER_AI_WORKSPACE_ROOT", "")
	t.Setenv("CRAWLER_AI_ORCHESTRATOR_PROVIDER", "")
	t.Setenv("CRAWLER_AI_ORCHESTRATOR_MODEL", "")
	t.Setenv("CRAWLER_AI_WORKER_PROVIDER", "")
	t.Setenv("CRAWLER_AI_WORKER_MODEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Env != EnvProduction {
		t.Errorf("Env = %q, want %q", cfg.Env, EnvProduction)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if !cfg.Yolo {
		t.Error("Yolo should be true")
	}
	if cfg.Models.Orchestrator.Model != "custom-model" {
		t.Errorf("Orchestrator.Model = %q, want %q", cfg.Models.Orchestrator.Model, "custom-model")
	}
	if len(cfg.Permissions.AllowedTools) != 2 {
		t.Errorf("AllowedTools = %v, want 2 items", cfg.Permissions.AllowedTools)
	}
	if len(cfg.Permissions.DisabledTools) != 1 {
		t.Errorf("DisabledTools = %v, want 1 item", cfg.Permissions.DisabledTools)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".crawler-ai.json")

	content := `{"log_level": "info", "models": {"orchestrator": {"provider": "mock", "model": "file-model"}}}`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	origLookup := lookupWorkingDirectory
	lookupWorkingDirectory = func() (string, error) { return dir, nil }
	defer func() { lookupWorkingDirectory = origLookup }()

	t.Setenv("CRAWLER_AI_LOG_LEVEL", "error")
	t.Setenv("CRAWLER_AI_ENV", "")
	t.Setenv("CRAWLER_AI_WORKSPACE_ROOT", "")
	t.Setenv("CRAWLER_AI_ORCHESTRATOR_PROVIDER", "")
	t.Setenv("CRAWLER_AI_ORCHESTRATOR_MODEL", "")
	t.Setenv("CRAWLER_AI_WORKER_PROVIDER", "")
	t.Setenv("CRAWLER_AI_WORKER_MODEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.LogLevel != "error" {
		t.Errorf("LogLevel = %q, want %q (env should override file)", cfg.LogLevel, "error")
	}
}

func TestLoadNoFileUsesDefaults(t *testing.T) {
	dir := t.TempDir()

	origLookup := lookupWorkingDirectory
	lookupWorkingDirectory = func() (string, error) { return dir, nil }
	defer func() { lookupWorkingDirectory = origLookup }()

	t.Setenv("CRAWLER_AI_ENV", "")
	t.Setenv("CRAWLER_AI_LOG_LEVEL", "")
	t.Setenv("CRAWLER_AI_WORKSPACE_ROOT", "")
	t.Setenv("CRAWLER_AI_ORCHESTRATOR_PROVIDER", "")
	t.Setenv("CRAWLER_AI_ORCHESTRATOR_MODEL", "")
	t.Setenv("CRAWLER_AI_WORKER_PROVIDER", "")
	t.Setenv("CRAWLER_AI_WORKER_MODEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Env != EnvDevelopment {
		t.Errorf("Env = %q, want %q", cfg.Env, EnvDevelopment)
	}
	if cfg.Models.Orchestrator.Provider != "mock" {
		t.Errorf("Orchestrator.Provider = %q, want %q", cfg.Models.Orchestrator.Provider, "mock")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, ".crawler-ai.json")

	if err := os.WriteFile(configPath, []byte("{invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	origLookup := lookupWorkingDirectory
	lookupWorkingDirectory = func() (string, error) { return dir, nil }
	defer func() { lookupWorkingDirectory = origLookup }()

	t.Setenv("CRAWLER_AI_ENV", "")
	t.Setenv("CRAWLER_AI_LOG_LEVEL", "")
	t.Setenv("CRAWLER_AI_WORKSPACE_ROOT", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() should fail on invalid JSON")
	}
}
