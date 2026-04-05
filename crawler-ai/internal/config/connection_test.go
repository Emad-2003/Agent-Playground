package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crawler-ai/internal/oauth"
)

func TestProviderConfigTestConnectionOpenAIUsesResolvedKeyAndHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("expected authorization header, got %q", got)
		}
		if got := r.Header.Get("X-Test"); got != "value" {
			t.Fatalf("expected extra header, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer server.Close()

	if err := (ProviderConfig{
		Provider: "openai",
		BaseURL:  server.URL,
		APIKey:   "secret",
		ExtraHeaders: map[string]string{
			"X-Test": "value",
		},
	}).TestConnection(context.Background(), oauth.DefaultKeyStore()); err != nil {
		t.Fatalf("TestConnection() error: %v", err)
	}
}

func TestProviderConfigTestConnectionMockIsNoOp(t *testing.T) {
	t.Parallel()

	if err := (ProviderConfig{Provider: "mock", Model: "mock-orchestrator-v1"}).TestConnection(context.Background(), oauth.DefaultKeyStore()); err != nil {
		t.Fatalf("expected mock validation to succeed, got %v", err)
	}
}
