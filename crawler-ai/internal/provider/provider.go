package provider

import (
	"context"

	"crawler-ai/internal/config"
	apperrors "crawler-ai/internal/errors"
)

type Role string

const (
	RoleOrchestrator Role = "orchestrator"
	RoleWorker       Role = "worker"
)

type Message struct {
	Role    string
	Content string
}

type Request struct {
	Model        string
	Messages     []Message
	SystemPrompt string
	MaxTokens    int
	Temperature  float64
}

type Usage struct {
	InputTokens  int
	OutputTokens int
}

type Response struct {
	Text  string
	Usage Usage
}

type Provider interface {
	Name() string
	Complete(ctx context.Context, request Request) (Response, error)
}

type Router struct {
	orchestrator Provider
	worker       Provider
}

func NewRouter(cfg config.ModelConfig) (*Router, error) {
	orchestrator, err := NewFromConfig(cfg.Orchestrator)
	if err != nil {
		return nil, err
	}
	worker, err := NewFromConfig(cfg.Worker)
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
	switch cfg.Provider {
	case "mock":
		return NewMock(cfg.Model), nil
	case "openai":
		return NewOpenAICompatible(cfg), nil
	case "anthropic":
		return NewAnthropic(cfg), nil
	default:
		return nil, apperrors.New("provider.NewFromConfig", apperrors.CodeInvalidConfig, "unsupported provider: "+cfg.Provider)
	}
}
