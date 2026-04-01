package config

import (
	"os"
	"path/filepath"
	"strings"

	apperrors "crawler-ai/internal/errors"
)

type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvProduction  Environment = "production"
	EnvTest        Environment = "test"
)

type Config struct {
	Env           Environment
	LogLevel      string
	WorkspaceRoot string
	Models        ModelConfig
}

type ModelConfig struct {
	Orchestrator ProviderConfig
	Worker       ProviderConfig
}

type ProviderConfig struct {
	Provider string
	Model    string
	BaseURL  string
	APIKey   string
}

func DefaultModelConfig() ModelConfig {
	return ModelConfig{
		Orchestrator: ProviderConfig{
			Provider: "mock",
			Model:    "mock-orchestrator-v1",
		},
		Worker: ProviderConfig{
			Provider: "mock",
			Model:    "mock-worker-v1",
		},
	}
}

var lookupWorkingDirectory = os.Getwd

func LoadFromEnv() (Config, error) {
	workspaceRoot := strings.TrimSpace(os.Getenv("CRAWLER_AI_WORKSPACE_ROOT"))
	if workspaceRoot == "" {
		cwd, err := lookupWorkingDirectory()
		if err != nil {
			return Config{}, apperrors.Wrap("config.LoadFromEnv", apperrors.CodeInvalidConfig, err, "resolve current working directory")
		}
		workspaceRoot = cwd
	}

	workspaceRoot = filepath.Clean(workspaceRoot)

	cfg := Config{
		Env:           parseEnvironment(os.Getenv("CRAWLER_AI_ENV")),
		LogLevel:      parseLogLevel(os.Getenv("CRAWLER_AI_LOG_LEVEL")),
		WorkspaceRoot: workspaceRoot,
		Models:        DefaultModelConfig(),
	}

	cfg.Models = ModelConfig{
		Orchestrator: ProviderConfig{
			Provider: parseProviderName(os.Getenv("CRAWLER_AI_ORCHESTRATOR_PROVIDER"), "mock"),
			Model:    defaultString(os.Getenv("CRAWLER_AI_ORCHESTRATOR_MODEL"), "mock-orchestrator-v1"),
			BaseURL:  strings.TrimSpace(os.Getenv("CRAWLER_AI_ORCHESTRATOR_BASE_URL")),
			APIKey:   strings.TrimSpace(os.Getenv("CRAWLER_AI_ORCHESTRATOR_API_KEY")),
		},
		Worker: ProviderConfig{
			Provider: parseProviderName(os.Getenv("CRAWLER_AI_WORKER_PROVIDER"), "mock"),
			Model:    defaultString(os.Getenv("CRAWLER_AI_WORKER_MODEL"), "mock-worker-v1"),
			BaseURL:  strings.TrimSpace(os.Getenv("CRAWLER_AI_WORKER_BASE_URL")),
			APIKey:   strings.TrimSpace(os.Getenv("CRAWLER_AI_WORKER_API_KEY")),
		},
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.Env != EnvDevelopment && c.Env != EnvProduction && c.Env != EnvTest {
		return apperrors.New("config.Validate", apperrors.CodeInvalidConfig, "environment must be development, production, or test")
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return apperrors.New("config.Validate", apperrors.CodeInvalidConfig, "log level must be debug, info, warn, or error")
	}

	if strings.TrimSpace(c.WorkspaceRoot) == "" {
		return apperrors.New("config.Validate", apperrors.CodeInvalidConfig, "workspace root must not be empty")
	}

	if err := validateProviderConfig("orchestrator", c.Models.Orchestrator); err != nil {
		return err
	}
	if err := validateProviderConfig("worker", c.Models.Worker); err != nil {
		return err
	}

	return nil
}

func parseEnvironment(raw string) Environment {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "dev", "development":
		return EnvDevelopment
	case "prod", "production":
		return EnvProduction
	case "test", "testing":
		return EnvTest
	default:
		return Environment(strings.ToLower(strings.TrimSpace(raw)))
	}
}

func parseLogLevel(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "info"
	}

	return value
}

func validateProviderConfig(role string, cfg ProviderConfig) error {
	provider := strings.TrimSpace(cfg.Provider)
	model := strings.TrimSpace(cfg.Model)
	if provider == "" {
		return apperrors.New("config.validateProviderConfig", apperrors.CodeInvalidConfig, role+" provider must not be empty")
	}
	if model == "" {
		return apperrors.New("config.validateProviderConfig", apperrors.CodeInvalidConfig, role+" model must not be empty")
	}

	switch provider {
	case "mock":
		return nil
	case "anthropic", "openai":
		if strings.TrimSpace(cfg.BaseURL) == "" {
			return apperrors.New("config.validateProviderConfig", apperrors.CodeInvalidConfig, role+" base URL must not be empty for provider "+provider)
		}
		if strings.TrimSpace(cfg.APIKey) == "" {
			return apperrors.New("config.validateProviderConfig", apperrors.CodeInvalidConfig, role+" API key must not be empty for provider "+provider)
		}
		return nil
	default:
		return apperrors.New("config.validateProviderConfig", apperrors.CodeInvalidConfig, role+" provider must be mock, anthropic, or openai")
	}
}

func parseProviderName(raw, fallback string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return fallback
	}
	return value
}

func defaultString(raw, fallback string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	return value
}
