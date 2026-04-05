package provider

import (
	"context"
	"strings"

	"crawler-ai/internal/config"
	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/oauth"
	"crawler-ai/internal/providercatalog"
)

type Role string

const (
	RoleOrchestrator Role = "orchestrator"
	RoleWorker       Role = "worker"
)

type Message struct {
	Role          string         `json:"role"`
	Content       string         `json:"content,omitempty"`
	ContentBlocks []ContentBlock `json:"-"`
	ToolCallID    string         `json:"-"`
}

type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type ContentBlockKind string

const (
	ContentBlockText       ContentBlockKind = "text"
	ContentBlockReasoning  ContentBlockKind = "reasoning"
	ContentBlockToolCall   ContentBlockKind = "tool_call"
	ContentBlockToolResult ContentBlockKind = "tool_result"
)

type ToolCall struct {
	ID       string
	Name     string
	Input    string
	Finished bool
}

type ToolResult struct {
	ToolCallID string
	Name       string
	Content    string
	IsError    bool
}

type ContentBlock struct {
	Kind       ContentBlockKind
	Text       string
	ToolCall   *ToolCall
	ToolResult *ToolResult
}

func TextBlock(text string) ContentBlock {
	return ContentBlock{Kind: ContentBlockText, Text: text}
}

func ReasoningBlock(text string) ContentBlock {
	return ContentBlock{Kind: ContentBlockReasoning, Text: text}
}

func ToolCallBlock(call ToolCall) ContentBlock {
	cloned := call
	return ContentBlock{Kind: ContentBlockToolCall, ToolCall: &cloned}
}

func ToolResultBlock(result ToolResult) ContentBlock {
	cloned := result
	return ContentBlock{Kind: ContentBlockToolResult, ToolResult: &cloned}
}

type Request struct {
	Model           string
	Messages        []Message
	Tools           []ToolDefinition
	SystemPrompt    string
	MaxTokens       int
	Temperature     float64
	Headers         map[string]string
	Body            map[string]any
	ProviderOptions map[string]any
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

type Response struct {
	Text         string
	Reasoning    string
	Content      []ContentBlock
	Usage        Usage
	FinishReason string
}

func (r Response) ContentBlocks() []ContentBlock {
	return normalizeContentBlocks(r.Content, r.Text, r.Reasoning)
}

type Provider interface {
	Name() string
	Complete(ctx context.Context, request Request) (Response, error)
}

type Router struct {
	orchestrator Provider
	worker       Provider
}

type Factory func(config.ProviderConfig) Provider

var builtinFactories = map[string]Factory{
	"mock": func(cfg config.ProviderConfig) Provider {
		return NewMock(cfg.Model)
	},
	"openai": func(cfg config.ProviderConfig) Provider {
		return NewOpenAICompatible(cfg)
	},
	"anthropic": func(cfg config.ProviderConfig) Provider {
		return NewAnthropic(cfg)
	},
}

func NewRouter(cfg config.ModelConfig) (*Router, error) {
	// Resolve API keys from env → stored keys → config fallback.
	store := oauth.DefaultKeyStore()
	_ = store.Load() // best-effort; missing file is fine
	configStore, _ := config.OpenStore("")

	orchCfg := cfg.Orchestrator
	orchestrator, err := buildRoleProvider(RoleOrchestrator, orchCfg, configStore, store)
	if err != nil {
		return nil, err
	}

	workerCfg := cfg.Worker
	worker, err := buildRoleProvider(RoleWorker, workerCfg, configStore, store)
	if err != nil {
		return nil, err
	}

	return &Router{orchestrator: orchestrator, worker: worker}, nil
}

func (r *Router) ForRole(role Role) (Provider, error) {
	switch role {
	case RoleOrchestrator:
		return r.orchestrator, nil
	case RoleWorker:
		return r.worker, nil
	default:
		return nil, apperrors.New("provider.Router.ForRole", apperrors.CodeInvalidArgument, "unknown provider role")
	}
}

func NewFromConfig(cfg config.ProviderConfig) (Provider, error) {
	providerID := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if _, ok := providercatalog.Get(providerID); !ok {
		return nil, apperrors.New("provider.NewFromConfig", apperrors.CodeInvalidConfig, "provider is not registered: "+cfg.Provider)
	}

	factory, ok := builtinFactories[providerID]
	if !ok {
		return nil, apperrors.New("provider.NewFromConfig", apperrors.CodeInvalidConfig, "provider has no runtime factory: "+cfg.Provider)
	}

	return factory(cfg), nil
}

func buildRoleProvider(role Role, cfg config.ProviderConfig, cfgStore *config.Store, keyStore *oauth.KeyStore) (Provider, error) {
	providerID := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if _, ok := providercatalog.Get(providerID); !ok {
		return nil, apperrors.New("provider.buildRoleProvider", apperrors.CodeInvalidConfig, "provider is not registered: "+cfg.Provider)
	}

	factory, ok := builtinFactories[providerID]
	if !ok {
		return nil, apperrors.New("provider.buildRoleProvider", apperrors.CodeInvalidConfig, "provider has no runtime factory: "+cfg.Provider)
	}

	return newAuthProvider(role, cfg, factory, cfgStore, keyStore)
}

func prepareRuntimeConfig(store *oauth.KeyStore, cfg config.ProviderConfig) (config.ProviderConfig, error) {
	providerID := strings.ToLower(strings.TrimSpace(cfg.Provider))
	definition, ok := providercatalog.Get(providerID)
	if !ok {
		return config.ProviderConfig{}, apperrors.New("provider.prepareRuntimeConfig", apperrors.CodeInvalidConfig, "provider is not registered: "+cfg.Provider)
	}

	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = definition.DefaultBaseURL
	}
	if cfg.OAuthToken != nil && strings.TrimSpace(cfg.OAuthToken.AccessToken) != "" {
		cfg.APIKey = cfg.OAuthToken.AccessToken
	}
	cfg.APIKey = oauth.ResolveProviderKey(store, providerID, cfg.APIKey)

	if definition.RequiresBaseURL && strings.TrimSpace(cfg.BaseURL) == "" {
		return config.ProviderConfig{}, apperrors.New("provider.prepareRuntimeConfig", apperrors.CodeInvalidConfig, "provider base URL must not be empty: "+providerID)
	}
	if definition.RequiresAPIKey && strings.TrimSpace(cfg.APIKey) == "" {
		return config.ProviderConfig{}, apperrors.New("provider.prepareRuntimeConfig", apperrors.CodeInvalidConfig, "provider API key must not be empty: "+providerID)
	}

	return cfg, nil
}

func normalizeContentBlocks(explicit []ContentBlock, text, reasoning string) []ContentBlock {
	if len(explicit) > 0 {
		cloned := make([]ContentBlock, len(explicit))
		copy(cloned, explicit)
		return cloned
	}

	blocks := make([]ContentBlock, 0, 2)
	if strings.TrimSpace(reasoning) != "" {
		blocks = append(blocks, ReasoningBlock(reasoning))
	}
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, TextBlock(text))
	}
	return blocks
}
