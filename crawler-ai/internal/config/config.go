package config

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/oauth"
	"crawler-ai/internal/providercatalog"
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
	RecentModels  RecentModelsConfig
	Yolo          bool
	Permissions   PermissionsConfig
}

type PermissionsConfig struct {
	AllowedTools  []string
	DisabledTools []string
}

type ModelConfig struct {
	Orchestrator ProviderConfig
	Worker       ProviderConfig
}

type RecentModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url,omitempty"`
}

type RecentModelsConfig struct {
	Orchestrator []RecentModel `json:"orchestrator,omitempty"`
	Worker       []RecentModel `json:"worker,omitempty"`
}

type ProviderConfig struct {
	Provider        string
	Model           string
	BaseURL         string
	APIKey          string
	OAuthToken      *oauth.Token
	ExtraHeaders    map[string]string
	ExtraBody       map[string]any
	ProviderOptions map[string]any
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

	definition, ok := providercatalog.Get(provider)
	if !ok {
		return apperrors.New("config.validateProviderConfig", apperrors.CodeInvalidConfig, role+" provider is not registered: "+provider)
	}

	if definition.RequiresBaseURL && strings.TrimSpace(cfg.BaseURL) == "" {
		if strings.TrimSpace(definition.DefaultBaseURL) == "" {
			return apperrors.New("config.validateProviderConfig", apperrors.CodeInvalidConfig, role+" base URL must not be empty for provider "+provider)
		}
	}

	return nil
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

func (cfg ProviderConfig) TestConnection(ctx context.Context, store *oauth.KeyStore) error {
	providerID := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if providerID == "" {
		return apperrors.New("config.ProviderConfig.TestConnection", apperrors.CodeInvalidConfig, "provider must not be empty")
	}

	definition, ok := providercatalog.Get(providerID)
	if !ok {
		return apperrors.New("config.ProviderConfig.TestConnection", apperrors.CodeInvalidConfig, "provider is not registered: "+cfg.Provider)
	}
	if providerID == "mock" {
		return nil
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(definition.DefaultBaseURL, "/")
	}

	apiKey := strings.TrimSpace(cfg.APIKey)
	if cfg.OAuthToken != nil && strings.TrimSpace(cfg.OAuthToken.AccessToken) != "" {
		apiKey = strings.TrimSpace(cfg.OAuthToken.AccessToken)
	}
	apiKey = strings.TrimSpace(oauth.ResolveProviderKey(store, providerID, apiKey))

	if definition.RequiresBaseURL && baseURL == "" {
		return apperrors.New("config.ProviderConfig.TestConnection", apperrors.CodeInvalidConfig, "provider base URL must not be empty: "+providerID)
	}
	if definition.RequiresAPIKey && apiKey == "" {
		return apperrors.New("config.ProviderConfig.TestConnection", apperrors.CodeInvalidConfig, "provider API key must not be empty: "+providerID)
	}

	headers := cloneStringMap(cfg.ExtraHeaders)
	testURL := ""
	switch providerID {
	case "openai":
		testURL = baseURL + "/models"
		headers["Authorization"] = "Bearer " + apiKey
	case "anthropic":
		testURL = baseURL + "/models"
		headers["x-api-key"] = apiKey
		headers["anthropic-version"] = "2023-06-01"
	default:
		return fmt.Errorf("provider validation is not supported for %s yet", providerID)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, testURL, nil)
	if err != nil {
		return fmt.Errorf("create validation request: %w", err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("provider validation failed for %s: %w", providerID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("provider %s returned status %d during validation", providerID, resp.StatusCode)
	}

	return nil
}
