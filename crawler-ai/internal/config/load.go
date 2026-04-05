package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/oauth"
)

// configFile represents the JSON config on disk.
type configFile struct {
	Environment  string            `json:"environment,omitempty"`
	LogLevel     string            `json:"log_level,omitempty"`
	Yolo         bool              `json:"yolo,omitempty"`
	Models       *modelsFile       `json:"models,omitempty"`
	RecentModels *recentModelsFile `json:"recent_models,omitempty"`
	Permissions  *permissionsFile  `json:"permissions,omitempty"`
}

type modelsFile struct {
	Orchestrator *providerFile `json:"orchestrator,omitempty"`
	Worker       *providerFile `json:"worker,omitempty"`
}

type recentModelsFile struct {
	Orchestrator []recentModelFile `json:"orchestrator,omitempty"`
	Worker       []recentModelFile `json:"worker,omitempty"`
}

type recentModelFile struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
}

type providerFile struct {
	Provider        string            `json:"provider,omitempty"`
	Model           string            `json:"model,omitempty"`
	BaseURL         string            `json:"base_url,omitempty"`
	APIKey          string            `json:"api_key,omitempty"`
	OAuth           *oauth.Token      `json:"oauth,omitempty"`
	ExtraHeaders    map[string]string `json:"extra_headers,omitempty"`
	ExtraBody       map[string]any    `json:"extra_body,omitempty"`
	ProviderOptions map[string]any    `json:"provider_options,omitempty"`
}

type permissionsFile struct {
	AllowedTools  []string `json:"allowed_tools,omitempty"`
	DisabledTools []string `json:"disabled_tools,omitempty"`
}

// Load resolves configuration from files + environment in priority order:
//  1. Environment variables (highest priority)
//  2. Project config: .crawler-ai.json or crawler-ai.json in cwd
//  3. User config: <crawler-ai data dir>/config.json
func Load() (Config, error) {
	cwd, err := lookupWorkingDirectory()
	if err != nil {
		return Config{}, apperrors.Wrap("config.Load", apperrors.CodeInvalidConfig, err, "resolve working directory")
	}

	return LoadForWorkingDir(cwd)
}

// LoadForWorkingDir resolves configuration using the provided workspace root
// for project-scoped config files.
func LoadForWorkingDir(workingDir string) (Config, error) {
	cfg := Config{
		Env:      EnvDevelopment,
		LogLevel: "info",
		Models:   DefaultModelConfig(),
	}

	if strings.TrimSpace(workingDir) == "" {
		cwd, err := lookupWorkingDirectory()
		if err != nil {
			return Config{}, apperrors.Wrap("config.LoadForWorkingDir", apperrors.CodeInvalidConfig, err, "resolve working directory")
		}
		workingDir = cwd
	}
	workingDir = filepath.Clean(workingDir)

	// Load user config
	userDir := oauth.DefaultConfigDir()
	if userDir != "" {
		userPath := filepath.Join(userDir, "config.json")
		if err := mergeFromFile(&cfg, userPath); err != nil {
			return Config{}, err
		}
	}

	// Load project config
	projectPaths := projectConfigPaths(workingDir)
	for _, p := range projectPaths {
		if err := mergeFromFile(&cfg, p); err != nil {
			return Config{}, err
		}
	}

	// Environment overrides (highest priority)
	applyEnvOverrides(&cfg)

	// Set workspace root
	if cfg.WorkspaceRoot == "" {
		cfg.WorkspaceRoot = workingDir
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func projectConfigPaths(workingDir string) []string {
	return []string{
		filepath.Join(workingDir, ".crawler-ai.json"),
		filepath.Join(workingDir, "crawler-ai.json"),
	}
}

func mergeFromFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return apperrors.Wrap("config.mergeFromFile", apperrors.CodeInvalidConfig, err, "read config file: "+path)
	}

	var file configFile
	if err := json.Unmarshal(data, &file); err != nil {
		return apperrors.Wrap("config.mergeFromFile", apperrors.CodeInvalidConfig, err, "parse config file: "+path)
	}

	if file.Environment != "" {
		cfg.Env = parseEnvironment(file.Environment)
	}
	if file.LogLevel != "" {
		cfg.LogLevel = file.LogLevel
	}
	if file.Yolo {
		cfg.Yolo = true
	}

	if file.Models != nil {
		if file.Models.Orchestrator != nil {
			mergeProvider(&cfg.Models.Orchestrator, file.Models.Orchestrator)
		}
		if file.Models.Worker != nil {
			mergeProvider(&cfg.Models.Worker, file.Models.Worker)
		}
	}

	if file.RecentModels != nil {
		if file.RecentModels.Orchestrator != nil {
			cfg.RecentModels.Orchestrator = cloneRecentModels(file.RecentModels.Orchestrator)
		}
		if file.RecentModels.Worker != nil {
			cfg.RecentModels.Worker = cloneRecentModels(file.RecentModels.Worker)
		}
	}

	if file.Permissions != nil {
		cfg.Permissions.AllowedTools = file.Permissions.AllowedTools
		cfg.Permissions.DisabledTools = file.Permissions.DisabledTools
	}

	return nil
}

func mergeProvider(target *ProviderConfig, source *providerFile) {
	if source.Provider != "" {
		target.Provider = source.Provider
	}
	if source.Model != "" {
		target.Model = source.Model
	}
	if source.BaseURL != "" {
		target.BaseURL = source.BaseURL
	}
	if source.APIKey != "" {
		target.APIKey = source.APIKey
	}
	if source.OAuth != nil {
		target.OAuthToken = source.OAuth.Clone()
	}
	if source.ExtraHeaders != nil {
		target.ExtraHeaders = cloneStringMap(source.ExtraHeaders)
	}
	if source.ExtraBody != nil {
		target.ExtraBody = cloneAnyMap(source.ExtraBody)
	}
	if source.ProviderOptions != nil {
		target.ProviderOptions = cloneAnyMap(source.ProviderOptions)
	}
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneAnyMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneAnyValue(value)
	}
	return cloned
}

func cloneAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneAnyValue(item)
		}
		return cloned
	default:
		return typed
	}
}

func cloneRecentModels(source []recentModelFile) []RecentModel {
	if source == nil {
		return nil
	}
	cloned := make([]RecentModel, len(source))
	for index, item := range source {
		cloned[index] = RecentModel{
			Provider: item.Provider,
			Model:    item.Model,
			BaseURL:  item.BaseURL,
		}
	}
	return cloned
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("CRAWLER_AI_ENV"); v != "" {
		cfg.Env = parseEnvironment(v)
	}
	if v := os.Getenv("CRAWLER_AI_LOG_LEVEL"); v != "" {
		cfg.LogLevel = parseLogLevel(v)
	}
	if v := os.Getenv("CRAWLER_AI_WORKSPACE_ROOT"); strings.TrimSpace(v) != "" {
		cfg.WorkspaceRoot = filepath.Clean(v)
	}

	if v := os.Getenv("CRAWLER_AI_ORCHESTRATOR_PROVIDER"); v != "" {
		cfg.Models.Orchestrator.Provider = parseProviderName(v, cfg.Models.Orchestrator.Provider)
	}
	if v := os.Getenv("CRAWLER_AI_ORCHESTRATOR_MODEL"); v != "" {
		cfg.Models.Orchestrator.Model = defaultString(v, cfg.Models.Orchestrator.Model)
	}
	if v := os.Getenv("CRAWLER_AI_ORCHESTRATOR_BASE_URL"); v != "" {
		cfg.Models.Orchestrator.BaseURL = strings.TrimSpace(v)
	}
	if v := os.Getenv("CRAWLER_AI_ORCHESTRATOR_API_KEY"); v != "" {
		cfg.Models.Orchestrator.APIKey = strings.TrimSpace(v)
	}

	if v := os.Getenv("CRAWLER_AI_WORKER_PROVIDER"); v != "" {
		cfg.Models.Worker.Provider = parseProviderName(v, cfg.Models.Worker.Provider)
	}
	if v := os.Getenv("CRAWLER_AI_WORKER_MODEL"); v != "" {
		cfg.Models.Worker.Model = defaultString(v, cfg.Models.Worker.Model)
	}
	if v := os.Getenv("CRAWLER_AI_WORKER_BASE_URL"); v != "" {
		cfg.Models.Worker.BaseURL = strings.TrimSpace(v)
	}
	if v := os.Getenv("CRAWLER_AI_WORKER_API_KEY"); v != "" {
		cfg.Models.Worker.APIKey = strings.TrimSpace(v)
	}
}
