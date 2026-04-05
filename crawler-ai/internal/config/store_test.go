package config

import (
	"context"
	"path/filepath"
	"testing"

	"crawler-ai/internal/oauth"
)

func TestOpenStoreUsesUserAndWorkspacePaths(t *testing.T) {
	workspaceDir := t.TempDir()
	userDir := t.TempDir()
	t.Setenv("APPDATA", userDir)

	store, err := OpenStore(workspaceDir)
	if err != nil {
		t.Fatalf("OpenStore() error: %v", err)
	}

	if store.Path(ScopeUser) != filepath.Join(userDir, "crawler-ai", "config.json") {
		t.Fatalf("unexpected user path: %s", store.Path(ScopeUser))
	}
	if store.Path(ScopeWorkspace) != filepath.Join(workspaceDir, ".crawler-ai.json") {
		t.Fatalf("unexpected workspace path: %s", store.Path(ScopeWorkspace))
	}
}

func TestOpenStoreUsesDataDirOverrideForUserPath(t *testing.T) {
	workspaceDir := t.TempDir()
	override := filepath.Join(t.TempDir(), "crawler-data")
	oauth.SetDefaultConfigDir(override)
	defer oauth.SetDefaultConfigDir("")

	store, err := OpenStore(workspaceDir)
	if err != nil {
		t.Fatalf("OpenStore() error: %v", err)
	}

	if store.Path(ScopeUser) != filepath.Join(override, "config.json") {
		t.Fatalf("unexpected overridden user path: %s", store.Path(ScopeUser))
	}
}

func TestUpdateProviderConfigWritesScopedConfig(t *testing.T) {
	workspaceDir := t.TempDir()
	userDir := t.TempDir()
	t.Setenv("APPDATA", userDir)

	store, err := OpenStore(workspaceDir)
	if err != nil {
		t.Fatalf("OpenStore() error: %v", err)
	}

	err = store.UpdateProviderConfig(ScopeWorkspace, "orchestrator", ProviderConfig{
		Provider: "openai",
		Model:    "gpt-4o",
	})
	if err != nil {
		t.Fatalf("UpdateProviderConfig() error: %v", err)
	}

	loaded, err := LoadForWorkingDir(workspaceDir)
	if err != nil {
		t.Fatalf("LoadForWorkingDir() error: %v", err)
	}
	if loaded.Models.Orchestrator.Provider != "openai" {
		t.Fatalf("expected orchestrator provider openai, got %s", loaded.Models.Orchestrator.Provider)
	}
	if loaded.Models.Orchestrator.Model != "gpt-4o" {
		t.Fatalf("expected orchestrator model gpt-4o, got %s", loaded.Models.Orchestrator.Model)
	}
}

func TestParseScope(t *testing.T) {
	tests := map[string]Scope{
		"":          ScopeUser,
		"user":      ScopeUser,
		"global":    ScopeUser,
		"workspace": ScopeWorkspace,
		"project":   ScopeWorkspace,
	}

	for input, want := range tests {
		got, err := ParseScope(input)
		if err != nil {
			t.Fatalf("ParseScope(%q) error: %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseScope(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSetProviderOAuthTokenPersistsToken(t *testing.T) {
	workspaceDir := t.TempDir()
	userDir := t.TempDir()
	t.Setenv("APPDATA", userDir)

	store, err := OpenStore(workspaceDir)
	if err != nil {
		t.Fatalf("OpenStore() error: %v", err)
	}

	err = store.UpdateProviderConfig(ScopeWorkspace, "orchestrator", ProviderConfig{
		Provider: "openai",
		Model:    "gpt-4o",
	})
	if err != nil {
		t.Fatalf("UpdateProviderConfig() error: %v", err)
	}

	token := &oauth.Token{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 60}
	token.SetExpiresAt()
	if err := store.SetProviderOAuthToken(ScopeWorkspace, "orchestrator", token); err != nil {
		t.Fatalf("SetProviderOAuthToken() error: %v", err)
	}

	loaded, err := LoadForWorkingDir(workspaceDir)
	if err != nil {
		t.Fatalf("LoadForWorkingDir() error: %v", err)
	}
	if loaded.Models.Orchestrator.OAuthToken == nil {
		t.Fatal("expected oauth token to persist")
	}
	if loaded.Models.Orchestrator.OAuthToken.AccessToken != "access" {
		t.Fatalf("expected persisted access token, got %s", loaded.Models.Orchestrator.OAuthToken.AccessToken)
	}
}

func TestRefreshProviderOAuthTokenUsesRegisteredRefresher(t *testing.T) {
	workspaceDir := t.TempDir()
	userDir := t.TempDir()
	t.Setenv("APPDATA", userDir)

	store, err := OpenStore(workspaceDir)
	if err != nil {
		t.Fatalf("OpenStore() error: %v", err)
	}

	err = store.UpdateProviderConfig(ScopeWorkspace, "orchestrator", ProviderConfig{
		Provider: "openai",
		Model:    "gpt-4o",
	})
	if err != nil {
		t.Fatalf("UpdateProviderConfig() error: %v", err)
	}

	unregister := oauth.RegisterRefresher("openai", func(ctx context.Context, token *oauth.Token) (*oauth.Token, error) {
		return &oauth.Token{AccessToken: "new-access", RefreshToken: token.RefreshToken, ExpiresIn: 120}, nil
	})
	defer unregister()

	if err := store.SetProviderOAuthToken(ScopeWorkspace, "orchestrator", &oauth.Token{AccessToken: "old-access", RefreshToken: "refresh", ExpiresIn: 30}); err != nil {
		t.Fatalf("SetProviderOAuthToken() error: %v", err)
	}

	if err := store.RefreshProviderOAuthToken(context.Background(), ScopeWorkspace, "orchestrator"); err != nil {
		t.Fatalf("RefreshProviderOAuthToken() error: %v", err)
	}

	loaded, err := LoadForWorkingDir(workspaceDir)
	if err != nil {
		t.Fatalf("LoadForWorkingDir() error: %v", err)
	}
	if loaded.Models.Orchestrator.OAuthToken == nil {
		t.Fatal("expected oauth token after refresh")
	}
	if loaded.Models.Orchestrator.OAuthToken.AccessToken != "new-access" {
		t.Fatalf("expected refreshed access token, got %s", loaded.Models.Orchestrator.OAuthToken.AccessToken)
	}
}

func TestUpdatePreferredModelRecordsRecentModels(t *testing.T) {
	workspaceDir := t.TempDir()
	userDir := t.TempDir()
	t.Setenv("APPDATA", userDir)

	store, err := OpenStore(workspaceDir)
	if err != nil {
		t.Fatalf("OpenStore() error: %v", err)
	}

	if err := store.UpdatePreferredModel(ScopeWorkspace, "orchestrator", ProviderConfig{Provider: "openai", Model: "gpt-4o"}); err != nil {
		t.Fatalf("UpdatePreferredModel() error: %v", err)
	}
	if err := store.UpdatePreferredModel(ScopeWorkspace, "orchestrator", ProviderConfig{Provider: "anthropic", Model: "claude-sonnet-4-20250514"}); err != nil {
		t.Fatalf("UpdatePreferredModel() error: %v", err)
	}
	if err := store.UpdatePreferredModel(ScopeWorkspace, "orchestrator", ProviderConfig{Provider: "openai", Model: "gpt-4o"}); err != nil {
		t.Fatalf("UpdatePreferredModel() error: %v", err)
	}

	loaded, err := LoadForWorkingDir(workspaceDir)
	if err != nil {
		t.Fatalf("LoadForWorkingDir() error: %v", err)
	}
	if len(loaded.RecentModels.Orchestrator) != 2 {
		t.Fatalf("expected 2 recent models, got %d", len(loaded.RecentModels.Orchestrator))
	}
	if loaded.RecentModels.Orchestrator[0].Provider != "openai" || loaded.RecentModels.Orchestrator[0].Model != "gpt-4o" {
		t.Fatalf("expected most recent model to be openai/gpt-4o, got %+v", loaded.RecentModels.Orchestrator[0])
	}
	if loaded.RecentModels.Orchestrator[1].Provider != "anthropic" {
		t.Fatalf("expected second recent model to be anthropic, got %+v", loaded.RecentModels.Orchestrator[1])
	}
	current, err := store.RecentModelsForRole("orchestrator")
	if err != nil {
		t.Fatalf("RecentModelsForRole() error: %v", err)
	}
	if len(current) != 2 {
		t.Fatalf("expected 2 in-memory recent models, got %d", len(current))
	}
	if current[0].Provider != "openai" {
		t.Fatalf("expected in-memory recent model to be openai, got %+v", current[0])
	}
}

func TestProviderMutationHelpersPersistHeadersBodyAndOptions(t *testing.T) {
	workspaceDir := t.TempDir()
	userDir := t.TempDir()
	t.Setenv("APPDATA", userDir)

	store, err := OpenStore(workspaceDir)
	if err != nil {
		t.Fatalf("OpenStore() error: %v", err)
	}

	if err := store.UpdateProviderConfig(ScopeWorkspace, "orchestrator", ProviderConfig{Provider: "openai", Model: "gpt-4o"}); err != nil {
		t.Fatalf("UpdateProviderConfig() error: %v", err)
	}
	if err := store.SetProviderHeader(ScopeWorkspace, "orchestrator", "X-Test", "value"); err != nil {
		t.Fatalf("SetProviderHeader() error: %v", err)
	}
	if err := store.SetProviderBodyValue(ScopeWorkspace, "orchestrator", "metadata.trace_id", "req-1"); err != nil {
		t.Fatalf("SetProviderBodyValue() error: %v", err)
	}
	if err := store.SetProviderOptionValue(ScopeWorkspace, "orchestrator", "reasoning.effort", "medium"); err != nil {
		t.Fatalf("SetProviderOptionValue() error: %v", err)
	}

	loaded, err := LoadForWorkingDir(workspaceDir)
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
		t.Fatalf("expected persisted provider option, got %#v", loaded.Models.Orchestrator.ProviderOptions)
	}

	if err := store.RemoveProviderHeader(ScopeWorkspace, "orchestrator", "X-Test"); err != nil {
		t.Fatalf("RemoveProviderHeader() error: %v", err)
	}
	if err := store.RemoveProviderBodyValue(ScopeWorkspace, "orchestrator", "metadata.trace_id"); err != nil {
		t.Fatalf("RemoveProviderBodyValue() error: %v", err)
	}
	if err := store.RemoveProviderOptionValue(ScopeWorkspace, "orchestrator", "reasoning.effort"); err != nil {
		t.Fatalf("RemoveProviderOptionValue() error: %v", err)
	}

	loaded, err = LoadForWorkingDir(workspaceDir)
	if err != nil {
		t.Fatalf("LoadForWorkingDir() error: %v", err)
	}
	if len(loaded.Models.Orchestrator.ExtraHeaders) != 0 {
		t.Fatalf("expected headers to be removed, got %#v", loaded.Models.Orchestrator.ExtraHeaders)
	}
	if len(loaded.Models.Orchestrator.ExtraBody) != 0 {
		t.Fatalf("expected extra body to be removed, got %#v", loaded.Models.Orchestrator.ExtraBody)
	}
	if len(loaded.Models.Orchestrator.ProviderOptions) != 0 {
		t.Fatalf("expected provider options to be removed, got %#v", loaded.Models.Orchestrator.ProviderOptions)
	}
}
