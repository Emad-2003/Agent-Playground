package provider

import (
	"testing"

	"crawler-ai/internal/config"
)

func TestConfigValidationAllowsRemoteProvidersWithoutInlineCredentials(t *testing.T) {
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
	if err != nil {
		t.Fatalf("expected config validation to pass, got %v", err)
	}
}
