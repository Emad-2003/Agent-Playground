package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"crawler-ai/internal/config"
	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/oauth"
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

func TestAuthProviderCompleteWithoutStreamingNormalizesContentBlocks(t *testing.T) {
	t.Parallel()

	wrapper := &authProvider{
		provider: providerStubForAuthTest{response: Response{Text: "Answer body.", Reasoning: "Inspecting repository state.", FinishReason: "stop", Usage: Usage{InputTokens: 3, OutputTokens: 2}}},
	}

	var chunks []StreamChunk
	response, err := wrapper.completeWithoutStreaming(context.Background(), Request{}, func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("completeWithoutStreaming() error: %v", err)
	}
	if len(response.ContentBlocks()) != 2 {
		t.Fatalf("expected normalized content blocks, got %#v", response.ContentBlocks())
	}
	if len(chunks) != 3 {
		t.Fatalf("expected reasoning, text, and done chunks, got %#v", chunks)
	}
	if chunks[0].Reasoning != "Inspecting repository state." || len(chunks[0].Content) != 1 || chunks[0].Content[0].Kind != ContentBlockReasoning {
		t.Fatalf("unexpected reasoning chunk: %#v", chunks[0])
	}
	if chunks[1].Text != "Answer body." || len(chunks[1].Content) != 1 || chunks[1].Content[0].Kind != ContentBlockText {
		t.Fatalf("unexpected text chunk: %#v", chunks[1])
	}
	if !chunks[2].Done || chunks[2].FinishReason != "stop" {
		t.Fatalf("unexpected final chunk: %#v", chunks[2])
	}
}

type providerStubForAuthTest struct {
	response Response
	err      error
}

func (p providerStubForAuthTest) Name() string { return "stub" }

func (p providerStubForAuthTest) Complete(context.Context, Request) (Response, error) {
	if p.err != nil {
		return Response{}, p.err
	}
	return p.response, nil
}

func TestOpenAICompatibleCompletes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		messages, ok := body["messages"].([]any)
		if !ok || len(messages) != 1 {
			t.Fatalf("expected one message in request body, got %#v", body["messages"])
		}
		message, ok := messages[0].(map[string]any)
		if !ok {
			t.Fatalf("expected message object, got %#v", messages[0])
		}
		if message["role"] != "user" || message["content"] != "hello" {
			t.Fatalf("expected lowercase role/content keys, got %#v", message)
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
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		messages, ok := body["messages"].([]any)
		if !ok || len(messages) != 1 {
			t.Fatalf("expected one message in request body, got %#v", body["messages"])
		}
		message, ok := messages[0].(map[string]any)
		if !ok {
			t.Fatalf("expected message object, got %#v", messages[0])
		}
		if message["role"] != "user" || message["content"] != "hello" {
			t.Fatalf("expected lowercase role/content keys, got %#v", message)
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

func TestOpenAICompatibleMergesHeadersAndBodyOptions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Provider"); got != "request" {
			t.Fatalf("expected request header override, got %q", got)
		}
		if got := r.Header.Get("X-Request"); got != "present" {
			t.Fatalf("expected request header, got %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		metadata, _ := body["metadata"].(map[string]any)
		if metadata["provider"] != "yes" || metadata["request"] != "yes" {
			t.Fatalf("expected merged metadata, got %#v", metadata)
		}
		if body["reasoning_effort"] != "medium" {
			t.Fatalf("expected merged provider option, got %#v", body["reasoning_effort"])
		}
		if body["user"] != "alice" {
			t.Fatalf("expected extra body field, got %#v", body["user"])
		}
		if body["trace_id"] != "req-1" {
			t.Fatalf("expected request body field, got %#v", body["trace_id"])
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
		ExtraHeaders: map[string]string{
			"X-Provider": "provider",
		},
		ExtraBody: map[string]any{
			"user": "alice",
		},
		ProviderOptions: map[string]any{
			"reasoning_effort": "medium",
			"metadata":         map[string]any{"provider": "yes"},
		},
	})

	response, err := provider.Complete(context.Background(), Request{
		Model:    "gpt-test",
		Messages: []Message{{Role: "user", Content: "hello"}},
		Headers: map[string]string{
			"X-Provider": "request",
			"X-Request":  "present",
		},
		Body: map[string]any{
			"trace_id": "req-1",
		},
		ProviderOptions: map[string]any{
			"metadata": map[string]any{"request": "yes"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected completion error: %v", err)
	}
	if response.Text != "openai reply" {
		t.Fatalf("expected openai reply, got %s", response.Text)
	}
}

func TestOpenAICompatibleStreamRequestsUsageAccounting(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if stream, ok := body["stream"].(bool); !ok || !stream {
			t.Fatalf("expected stream=true, got %#v", body["stream"])
		}
		streamOptions, ok := body["stream_options"].(map[string]any)
		if !ok {
			t.Fatalf("expected stream_options object, got %#v", body["stream_options"])
		}
		if includeUsage, ok := streamOptions["include_usage"].(bool); !ok || !includeUsage {
			t.Fatalf("expected include_usage=true, got %#v", streamOptions)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAICompatible(config.ProviderConfig{
		Provider: "openai",
		Model:    "gpt-test",
		BaseURL:  server.URL,
		APIKey:   "secret",
	})

	response, err := provider.CompleteStream(context.Background(), Request{
		Model:    "gpt-test",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}, func(StreamChunk) {})
	if err != nil {
		t.Fatalf("unexpected streaming error: %v", err)
	}
	if response.Usage.InputTokens != 7 || response.Usage.OutputTokens != 2 {
		t.Fatalf("expected streamed usage totals, got %#v", response.Usage)
	}
}

func TestOpenAICompatibleIncludesStatusBodyDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"type":    "invalid_request_error",
				"message": "messages must not be empty",
			},
		})
	}))
	defer server.Close()

	provider := NewOpenAICompatible(config.ProviderConfig{
		Provider: "openai",
		Model:    "gpt-test",
		BaseURL:  server.URL,
		APIKey:   "secret",
	})

	_, err := provider.Complete(context.Background(), Request{Model: "gpt-test"})
	if err == nil {
		t.Fatal("expected status error")
	}
	statusErr, ok := err.(*StatusError)
	if !ok {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 status, got %#v", statusErr)
	}
	if !strings.Contains(statusErr.Message, "messages must not be empty") {
		t.Fatalf("expected extracted api message, got %#v", statusErr)
	}
	if !strings.Contains(statusErr.Body, `"messages must not be empty"`) {
		t.Fatalf("expected compact response body, got %#v", statusErr)
	}
	if !strings.Contains(statusErr.Error(), "status 400") || !strings.Contains(statusErr.Error(), "invalid_request_error") {
		t.Fatalf("expected formatted error with status and message, got %q", statusErr.Error())
	}
}

func TestOpenAICompatibleStreamIncludesPlainTextStatusBodyDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limit exceeded"))
	}))
	defer server.Close()

	provider := NewOpenAICompatible(config.ProviderConfig{
		Provider: "openai",
		Model:    "gpt-test",
		BaseURL:  server.URL,
		APIKey:   "secret",
	})

	_, err := provider.CompleteStream(context.Background(), Request{
		Model:    "gpt-test",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}, func(StreamChunk) {})
	if err == nil {
		t.Fatal("expected status error")
	}
	statusErr, ok := err.(*StatusError)
	if !ok {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 status, got %#v", statusErr)
	}
	if statusErr.Body != "rate limit exceeded" {
		t.Fatalf("expected plain-text body, got %#v", statusErr)
	}
	if !strings.Contains(statusErr.Error(), "status 429") || !strings.Contains(statusErr.Error(), "rate limit exceeded") {
		t.Fatalf("expected formatted error with status and body, got %q", statusErr.Error())
	}
}

func TestAnthropicMergesHeadersAndProviderOptions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Trace"); got != "request" {
			t.Fatalf("expected request header override, got %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		thinking, _ := body["thinking"].(map[string]any)
		if thinking["budget_tokens"] != float64(2000) {
			t.Fatalf("expected provider option in body, got %#v", thinking)
		}
		meta, _ := body["metadata"].(map[string]any)
		if meta["request"] != "yes" {
			t.Fatalf("expected request metadata in body, got %#v", meta)
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
		ExtraHeaders: map[string]string{
			"X-Trace": "provider",
		},
		ProviderOptions: map[string]any{
			"thinking": map[string]any{"budget_tokens": 2000},
		},
	})

	response, err := provider.Complete(context.Background(), Request{
		Model:    "claude-test",
		Messages: []Message{{Role: "user", Content: "hello"}},
		Headers: map[string]string{
			"X-Trace": "request",
		},
		ProviderOptions: map[string]any{
			"metadata": map[string]any{"request": "yes"},
		},
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

func TestNewFromConfigRejectsUnregisteredProvider(t *testing.T) {
	t.Parallel()

	_, err := NewFromConfig(config.ProviderConfig{
		Provider: "gemini",
		Model:    "gemini-2.5-pro",
	})
	if err == nil {
		t.Fatal("expected provider construction error")
	}
	if !apperrors.IsCode(err, apperrors.CodeInvalidConfig) {
		t.Fatalf("expected invalid config error, got %v", err)
	}
}

func TestNewRouterUsesProviderDefaultBaseURLAndResolvedKey(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "secret")

	_, err := NewRouter(config.ModelConfig{
		Orchestrator: config.ProviderConfig{Provider: "openai", Model: "gpt-4o"},
		Worker:       config.ProviderConfig{Provider: "mock", Model: "worker"},
	})
	if err != nil {
		t.Fatalf("unexpected router error: %v", err)
	}
}

func TestNewRouterRejectsMissingResolvedKey(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")

	_, err := NewRouter(config.ModelConfig{
		Orchestrator: config.ProviderConfig{Provider: "openai", Model: "gpt-4o"},
		Worker:       config.ProviderConfig{Provider: "mock", Model: "worker"},
	})
	if err == nil {
		t.Fatal("expected router error")
	}
	if !apperrors.IsCode(err, apperrors.CodeInvalidConfig) {
		t.Fatalf("expected invalid config error, got %v", err)
	}
}

func TestRouterRefreshesExpiredOAuthTokenBeforeRequest(t *testing.T) {
	workspaceDir := t.TempDir()
	t.Setenv("APPDATA", t.TempDir())
	t.Chdir(workspaceDir)

	store, err := config.OpenStore(workspaceDir)
	if err != nil {
		t.Fatalf("OpenStore() error: %v", err)
	}
	if err := store.UpdateProviderConfig(config.ScopeWorkspace, "orchestrator", config.ProviderConfig{Provider: "openai", Model: "gpt-4o"}); err != nil {
		t.Fatalf("UpdateProviderConfig() error: %v", err)
	}
	expired := &oauth.Token{AccessToken: "old-access", RefreshToken: "refresh", ExpiresIn: 60, ExpiresAt: time.Now().Add(-time.Minute).Unix()}
	if err := store.SetProviderOAuthToken(config.ScopeWorkspace, "orchestrator", expired); err != nil {
		t.Fatalf("SetProviderOAuthToken() error: %v", err)
	}

	unregister := oauth.RegisterRefresher("openai", func(ctx context.Context, token *oauth.Token) (*oauth.Token, error) {
		return &oauth.Token{AccessToken: "new-access", RefreshToken: token.RefreshToken, ExpiresIn: 120}, nil
	})
	defer unregister()

	originalFactory := builtinFactories["openai"]
	defer func() { builtinFactories["openai"] = originalFactory }()
	var calls int
	builtinFactories["openai"] = func(cfg config.ProviderConfig) Provider {
		return providerFunc(func(ctx context.Context, request Request) (Response, error) {
			calls++
			if cfg.APIKey != "new-access" {
				return Response{}, apperrors.New("provider.test", apperrors.CodeProviderFailed, "expected refreshed access token")
			}
			return Response{Text: "ok"}, nil
		})
	}

	router, err := NewRouter(config.ModelConfig{
		Orchestrator: config.ProviderConfig{Provider: "openai", Model: "gpt-4o", OAuthToken: expired.Clone()},
		Worker:       config.ProviderConfig{Provider: "mock", Model: "worker"},
	})
	if err != nil {
		t.Fatalf("NewRouter() error: %v", err)
	}

	provider, err := router.ForRole(RoleOrchestrator)
	if err != nil {
		t.Fatalf("ForRole() error: %v", err)
	}
	if _, err := provider.Complete(context.Background(), Request{Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hello"}}}); err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one provider call after pre-refresh, got %d", calls)
	}
}

func TestRouterRetriesUnauthorizedWithRefreshedOAuthToken(t *testing.T) {
	workspaceDir := t.TempDir()
	t.Setenv("APPDATA", t.TempDir())
	t.Chdir(workspaceDir)

	store, err := config.OpenStore(workspaceDir)
	if err != nil {
		t.Fatalf("OpenStore() error: %v", err)
	}
	if err := store.UpdateProviderConfig(config.ScopeWorkspace, "orchestrator", config.ProviderConfig{Provider: "openai", Model: "gpt-4o"}); err != nil {
		t.Fatalf("UpdateProviderConfig() error: %v", err)
	}
	token := &oauth.Token{AccessToken: "old-access", RefreshToken: "refresh", ExpiresIn: 120, ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if err := store.SetProviderOAuthToken(config.ScopeWorkspace, "orchestrator", token); err != nil {
		t.Fatalf("SetProviderOAuthToken() error: %v", err)
	}

	unregister := oauth.RegisterRefresher("openai", func(ctx context.Context, token *oauth.Token) (*oauth.Token, error) {
		return &oauth.Token{AccessToken: "new-access", RefreshToken: token.RefreshToken, ExpiresIn: 120}, nil
	})
	defer unregister()

	originalFactory := builtinFactories["openai"]
	defer func() { builtinFactories["openai"] = originalFactory }()
	var factoryKeys []string
	builtinFactories["openai"] = func(cfg config.ProviderConfig) Provider {
		factoryKeys = append(factoryKeys, cfg.APIKey)
		if cfg.APIKey == "old-access" {
			return providerFunc(func(ctx context.Context, request Request) (Response, error) {
				return Response{}, &StatusError{Operation: "test", StatusCode: http.StatusUnauthorized, Message: "unauthorized"}
			})
		}
		return providerFunc(func(ctx context.Context, request Request) (Response, error) {
			return Response{Text: "ok"}, nil
		})
	}

	router, err := NewRouter(config.ModelConfig{
		Orchestrator: config.ProviderConfig{Provider: "openai", Model: "gpt-4o", OAuthToken: token.Clone()},
		Worker:       config.ProviderConfig{Provider: "mock", Model: "worker"},
	})
	if err != nil {
		t.Fatalf("NewRouter() error: %v", err)
	}

	provider, err := router.ForRole(RoleOrchestrator)
	if err != nil {
		t.Fatalf("ForRole() error: %v", err)
	}
	resp, err := provider.Complete(context.Background(), Request{Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp.Text != "ok" {
		t.Fatalf("expected retry response, got %q", resp.Text)
	}
	if len(factoryKeys) != 2 || factoryKeys[0] != "old-access" || factoryKeys[1] != "new-access" {
		t.Fatalf("unexpected factory keys: %v", factoryKeys)
	}
}

func TestRouterRetriesUnauthorizedAfterCredentialReresolution(t *testing.T) {
	workspaceDir := t.TempDir()
	t.Setenv("APPDATA", t.TempDir())
	t.Chdir(workspaceDir)
	t.Setenv("OPENAI_API_KEY", "old-access")

	originalFactory := builtinFactories["openai"]
	defer func() { builtinFactories["openai"] = originalFactory }()
	var factoryKeys []string
	builtinFactories["openai"] = func(cfg config.ProviderConfig) Provider {
		factoryKeys = append(factoryKeys, cfg.APIKey)
		if cfg.APIKey == "old-access" {
			return providerFunc(func(ctx context.Context, request Request) (Response, error) {
				if err := os.Setenv("OPENAI_API_KEY", "new-access"); err != nil {
					return Response{}, err
				}
				return Response{}, &StatusError{Operation: "test", StatusCode: http.StatusUnauthorized, Message: "unauthorized"}
			})
		}
		return providerFunc(func(ctx context.Context, request Request) (Response, error) {
			return Response{Text: "ok"}, nil
		})
	}

	router, err := NewRouter(config.ModelConfig{
		Orchestrator: config.ProviderConfig{Provider: "openai", Model: "gpt-4o"},
		Worker:       config.ProviderConfig{Provider: "mock", Model: "worker"},
	})
	if err != nil {
		t.Fatalf("NewRouter() error: %v", err)
	}

	provider, err := router.ForRole(RoleOrchestrator)
	if err != nil {
		t.Fatalf("ForRole() error: %v", err)
	}
	resp, err := provider.Complete(context.Background(), Request{Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if resp.Text != "ok" {
		t.Fatalf("expected retry response, got %q", resp.Text)
	}
	if len(factoryKeys) != 2 || factoryKeys[0] != "old-access" || factoryKeys[1] != "new-access" {
		t.Fatalf("unexpected factory keys: %v", factoryKeys)
	}
}

type providerFunc func(ctx context.Context, request Request) (Response, error)

func (f providerFunc) Name() string {
	return "test"
}

func (f providerFunc) Complete(ctx context.Context, request Request) (Response, error) {
	return f(ctx, request)
}
