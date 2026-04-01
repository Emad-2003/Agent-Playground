package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crawler-ai/internal/config"
	apperrors "crawler-ai/internal/errors"
)

func TestNewRouterUsesMockProvidersByDefault(t *testing.T) {
	t.Parallel()

	router, err := NewRouter(config.ModelConfig{
		Orchestrator: config.ProviderConfig{Provider: "mock", Model: "orch"},
		Worker:       config.ProviderConfig{Provider: "mock", Model: "work"},
	})
	if err != nil {
		t.Fatalf("unexpected router error: %v", err)
	}

	provider, err := router.ForRole(RoleOrchestrator)
	if err != nil {
		t.Fatalf("unexpected role error: %v", err)
	}
	if provider.Name() != "mock" {
		t.Fatalf("expected mock provider, got %s", provider.Name())
	}
}

func TestMockProviderCompletes(t *testing.T) {
	t.Parallel()

	provider := NewMock("mock-v1")
	response, err := provider.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hello world"}},
	})
	if err != nil {
		t.Fatalf("unexpected completion error: %v", err)
	}
	if response.Text == "" {
		t.Fatal("expected response text")
	}
}

func TestOpenAICompatibleCompletes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"content": "openai reply"},
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 4},
		})
	}))
	defer server.Close()

	provider := NewOpenAICompatible(config.ProviderConfig{
		Provider: "openai",
		Model:    "gpt-test",
		BaseURL:  server.URL,
		APIKey:   "secret",
	})

	response, err := provider.Complete(context.Background(), Request{
		Model:    "gpt-test",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected completion error: %v", err)
	}
	if response.Text != "openai reply" {
		t.Fatalf("expected openai reply, got %s", response.Text)
	}
}

func TestAnthropicCompletes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "anthropic reply"}},
			"usage":   map[string]any{"input_tokens": 11, "output_tokens": 6},
		})
	}))
	defer server.Close()

	provider := NewAnthropic(config.ProviderConfig{
		Provider: "anthropic",
		Model:    "claude-test",
		BaseURL:  server.URL,
		APIKey:   "secret",
	})

	response, err := provider.Complete(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("unexpected completion error: %v", err)
	}
	if response.Text != "anthropic reply" {
		t.Fatalf("expected anthropic reply, got %s", response.Text)
	}
}

func TestRouterRejectsUnknownRole(t *testing.T) {
	t.Parallel()

	router, err := NewRouter(config.ModelConfig{
		Orchestrator: config.ProviderConfig{Provider: "mock", Model: "orch"},
		Worker:       config.ProviderConfig{Provider: "mock", Model: "work"},
	})
	if err != nil {
		t.Fatalf("unexpected router error: %v", err)
	}

	_, err = router.ForRole("invalid")
	if err == nil {
		t.Fatal("expected router role error")
	}
	if !apperrors.IsCode(err, apperrors.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument error, got %v", err)
	}
}
