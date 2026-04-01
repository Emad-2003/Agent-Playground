package app

import (
	"context"
	"testing"

	"crawler-ai/internal/config"
	apperrors "crawler-ai/internal/errors"
)

func TestNewRejectsInvalidConfig(t *testing.T) {
	_, err := New(config.Config{})
	if err == nil {
		t.Fatal("expected error for invalid config")
	}

	if !apperrors.IsCode(err, apperrors.CodeInvalidConfig) {
		t.Fatalf("expected invalid config error, got %v", err)
	}
}

func TestRunRejectsNilContext(t *testing.T) {
	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "debug",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected new app error: %v", err)
	}

	err = application.Run(nil)
	if err == nil {
		t.Fatal("expected nil context error")
	}

	if !apperrors.IsCode(err, apperrors.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument error, got %v", err)
	}
}

func TestRunSucceeds(t *testing.T) {
	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: t.TempDir(),
		Models:        config.DefaultModelConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected new app error: %v", err)
	}

	if err := application.Run(context.Background()); err != nil {
		t.Fatalf("unexpected run error: %v", err)
	}
}
