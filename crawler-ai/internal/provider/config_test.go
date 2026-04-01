package provider

import (
	"testing"

	"crawler-ai/internal/config"
	apperrors "crawler-ai/internal/errors"
)

func TestConfigValidationRequiresProviderCredentialsForRemoteModels(t *testing.T) {
	t.Parallel()

	err := (config.Config{
		Env:           config.EnvDevelopment,
		LogLevel:      "info",
		WorkspaceRoot: "/workspace",
		Models: config.ModelConfig{
			Orchestrator: config.ProviderConfig{Provider: "openai", Model: "gpt-5.4"},
			Worker:       config.ProviderConfig{Provider: "mock", Model: "worker"},
		},
	}).Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !apperrors.IsCode(err, apperrors.CodeInvalidConfig) {
		t.Fatalf("expected invalid config error, got %v", err)
	}
}
