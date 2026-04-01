package config

import (
	"path/filepath"
	"testing"

	apperrors "crawler-ai/internal/errors"
)

func TestLoadFromEnvDefaults(t *testing.T) {
	t.Setenv("CRAWLER_AI_ENV", "")
	t.Setenv("CRAWLER_AI_LOG_LEVEL", "")
	t.Setenv("CRAWLER_AI_WORKSPACE_ROOT", t.TempDir())

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if cfg.Env != EnvDevelopment {
		t.Fatalf("expected development env, got %s", cfg.Env)
	}

	if cfg.LogLevel != "info" {
		t.Fatalf("expected info log level, got %s", cfg.LogLevel)
	}
}

func TestLoadFromEnvRejectsInvalidLevel(t *testing.T) {
	t.Setenv("CRAWLER_AI_ENV", "development")
	t.Setenv("CRAWLER_AI_LOG_LEVEL", "trace")
	t.Setenv("CRAWLER_AI_WORKSPACE_ROOT", t.TempDir())

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected invalid config error")
	}

	if !apperrors.IsCode(err, apperrors.CodeInvalidConfig) {
		t.Fatalf("expected invalid config code, got %v", err)
	}
}

func TestLoadFromEnvCleansWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CRAWLER_AI_ENV", "test")
	t.Setenv("CRAWLER_AI_LOG_LEVEL", "debug")
	t.Setenv("CRAWLER_AI_WORKSPACE_ROOT", filepath.Join(root, ".", "subdir", ".."))

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if cfg.WorkspaceRoot != root {
		t.Fatalf("expected cleaned workspace root %s, got %s", root, cfg.WorkspaceRoot)
	}
}

func TestValidateRejectsMissingWorkspaceRoot(t *testing.T) {
	err := Config{Env: EnvDevelopment, LogLevel: "info"}.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestLoadFromEnvUsesWorkingDirectoryFallback(t *testing.T) {
	root := t.TempDir()
	previous := lookupWorkingDirectory
	lookupWorkingDirectory = func() (string, error) {
		return root, nil
	}
	t.Cleanup(func() {
		lookupWorkingDirectory = previous
	})

	t.Setenv("CRAWLER_AI_ENV", "development")
	t.Setenv("CRAWLER_AI_LOG_LEVEL", "warn")
	t.Setenv("CRAWLER_AI_WORKSPACE_ROOT", "")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if cfg.WorkspaceRoot != root {
		t.Fatalf("expected workspace root %s, got %s", root, cfg.WorkspaceRoot)
	}
}
