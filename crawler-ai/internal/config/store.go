package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/oauth"
)

type Scope string

const (
	ScopeUser              Scope = "user"
	ScopeWorkspace         Scope = "workspace"
	maxRecentModelsPerRole       = 5
)

type Store struct {
	workingDir    string
	userPath      string
	workspacePath string
	config        Config
}

func OpenStore(workingDir string) (*Store, error) {
	if strings.TrimSpace(workingDir) == "" {
		cwd, err := lookupWorkingDirectory()
		if err != nil {
			return nil, apperrors.Wrap("config.OpenStore", apperrors.CodeInvalidConfig, err, "resolve working directory")
		}
		workingDir = cwd
	}

	workingDir = filepath.Clean(workingDir)
	cfg, err := LoadForWorkingDir(workingDir)
	if err != nil {
		return nil, err
	}

	return &Store{
		workingDir:    workingDir,
		userPath:      userConfigPath(),
		workspacePath: workspaceConfigPath(workingDir),
		config:        cfg,
	}, nil
}

func (s *Store) Config() Config {
	return s.config
}

func (s *Store) WorkingDir() string {
	return s.workingDir
}

func (s *Store) ProviderConfigForRole(role string) (ProviderConfig, error) {
	return s.providerConfigForRole(role)
}

func (s *Store) RecentModelsForRole(role string) ([]RecentModel, error) {
	normalizedRole, err := normalizeRole(role)
	if err != nil {
		return nil, err
	}
	if normalizedRole == "orchestrator" {
		return append([]RecentModel(nil), s.config.RecentModels.Orchestrator...), nil
	}
	return append([]RecentModel(nil), s.config.RecentModels.Worker...), nil
}

func (s *Store) ScopeForRole(role string) (Scope, error) {
	normalizedRole, err := normalizeRole(role)
	if err != nil {
		return "", err
	}

	workspaceFile, err := readScopeConfigFile(s.workspacePath)
	if err != nil {
		return "", err
	}
	if scopeHasRole(workspaceFile, normalizedRole) {
		return ScopeWorkspace, nil
	}

	userFile, err := readScopeConfigFile(s.userPath)
	if err != nil {
		return "", err
	}
	if scopeHasRole(userFile, normalizedRole) {
		return ScopeUser, nil
	}

	return ScopeUser, nil
}

func (s *Store) Path(scope Scope) string {
	switch scope {
	case ScopeWorkspace:
		return s.workspacePath
	default:
		return s.userPath
	}
}

func ParseScope(value string) (Scope, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(ScopeUser), "global":
		return ScopeUser, nil
	case string(ScopeWorkspace), "project":
		return ScopeWorkspace, nil
	default:
		return "", apperrors.New("config.ParseScope", apperrors.CodeInvalidArgument, "scope must be user or workspace")
	}
}

func (s *Store) UpdateProviderConfig(scope Scope, role string, providerCfg ProviderConfig) error {
	return s.updateRoleConfig(scope, role, providerCfg, false)
}

func (s *Store) UpdatePreferredModel(scope Scope, role string, providerCfg ProviderConfig) error {
	return s.updateRoleConfig(scope, role, providerCfg, true)
}

func (s *Store) SetProviderHeader(scope Scope, role, key, value string) error {
	return s.mutateProviderConfig(scope, role, func(providerCfg *ProviderConfig) error {
		if providerCfg.ExtraHeaders == nil {
			providerCfg.ExtraHeaders = make(map[string]string)
		}
		providerCfg.ExtraHeaders[key] = value
		return nil
	})
}

func (s *Store) RemoveProviderHeader(scope Scope, role, key string) error {
	return s.mutateProviderConfig(scope, role, func(providerCfg *ProviderConfig) error {
		if providerCfg.ExtraHeaders == nil {
			return nil
		}
		delete(providerCfg.ExtraHeaders, key)
		if len(providerCfg.ExtraHeaders) == 0 {
			providerCfg.ExtraHeaders = nil
		}
		return nil
	})
}

func (s *Store) SetProviderBodyValue(scope Scope, role, keyPath string, value any) error {
	return s.mutateProviderConfig(scope, role, func(providerCfg *ProviderConfig) error {
		providerCfg.ExtraBody = setNestedMapValue(providerCfg.ExtraBody, keyPath, value)
		return nil
	})
}

func (s *Store) RemoveProviderBodyValue(scope Scope, role, keyPath string) error {
	return s.mutateProviderConfig(scope, role, func(providerCfg *ProviderConfig) error {
		providerCfg.ExtraBody = deleteNestedMapValue(providerCfg.ExtraBody, keyPath)
		return nil
	})
}

func (s *Store) SetProviderOptionValue(scope Scope, role, keyPath string, value any) error {
	return s.mutateProviderConfig(scope, role, func(providerCfg *ProviderConfig) error {
		providerCfg.ProviderOptions = setNestedMapValue(providerCfg.ProviderOptions, keyPath, value)
		return nil
	})
}

func (s *Store) RemoveProviderOptionValue(scope Scope, role, keyPath string) error {
	return s.mutateProviderConfig(scope, role, func(providerCfg *ProviderConfig) error {
		providerCfg.ProviderOptions = deleteNestedMapValue(providerCfg.ProviderOptions, keyPath)
		return nil
	})
}

func (s *Store) mutateProviderConfig(scope Scope, role string, mutate func(providerCfg *ProviderConfig) error) error {
	providerCfg, err := s.providerConfigForRole(role)
	if err != nil {
		return err
	}
	if err := mutate(&providerCfg); err != nil {
		return err
	}
	return s.UpdateProviderConfig(scope, role, providerCfg)
}

func (s *Store) updateRoleConfig(scope Scope, role string, providerCfg ProviderConfig, recordRecent bool) error {
	normalizedRole, err := normalizeRole(role)
	if err != nil {
		return err
	}

	merged := s.config
	switch normalizedRole {
	case "orchestrator":
		merged.Models.Orchestrator = providerCfg
	case "worker":
		merged.Models.Worker = providerCfg
	}

	if err := merged.Validate(); err != nil {
		return err
	}

	file, err := readScopeConfigFile(s.Path(scope))
	if err != nil {
		return err
	}
	if file.Models == nil {
		file.Models = &modelsFile{}
	}

	updated := &providerFile{
		Provider:        providerCfg.Provider,
		Model:           providerCfg.Model,
		BaseURL:         providerCfg.BaseURL,
		APIKey:          providerCfg.APIKey,
		OAuth:           providerCfg.OAuthToken.Clone(),
		ExtraHeaders:    cloneStringMap(providerCfg.ExtraHeaders),
		ExtraBody:       cloneAnyMap(providerCfg.ExtraBody),
		ProviderOptions: cloneAnyMap(providerCfg.ProviderOptions),
	}
	if normalizedRole == "orchestrator" {
		file.Models.Orchestrator = updated
	} else {
		file.Models.Worker = updated
	}

	if recordRecent {
		merged.RecentModels = recordRecentModel(merged.RecentModels, normalizedRole, providerCfg)
		if file.RecentModels == nil {
			file.RecentModels = &recentModelsFile{}
		}
		setRecentModelsForRole(file.RecentModels, normalizedRole, recentModelFilesForRole(merged.RecentModels, normalizedRole))
	}

	if err := writeScopeConfigFile(s.Path(scope), file); err != nil {
		return err
	}

	s.config = merged
	return nil
}

func (s *Store) SetProviderOAuthToken(scope Scope, role string, token *oauth.Token) error {
	providerCfg, err := s.providerConfigForRole(role)
	if err != nil {
		return err
	}
	providerCfg.OAuthToken = token.Clone()
	if token != nil {
		providerCfg.APIKey = token.AccessToken
	}
	return s.UpdateProviderConfig(scope, role, providerCfg)
}

func (s *Store) RefreshProviderOAuthToken(ctx context.Context, scope Scope, role string) error {
	providerCfg, err := s.providerConfigForRole(role)
	if err != nil {
		return err
	}
	if providerCfg.OAuthToken == nil {
		return apperrors.New("config.Store.RefreshProviderOAuthToken", apperrors.CodeInvalidArgument, "provider does not have an oauth token")
	}

	refreshed, err := oauth.RefreshToken(ctx, providerCfg.Provider, providerCfg.OAuthToken)
	if err != nil {
		return err
	}
	return s.SetProviderOAuthToken(scope, role, refreshed)
}

func (s *Store) providerConfigForRole(role string) (ProviderConfig, error) {
	normalizedRole, err := normalizeRole(role)
	if err != nil {
		return ProviderConfig{}, err
	}
	if normalizedRole == "orchestrator" {
		return s.config.Models.Orchestrator, nil
	}
	return s.config.Models.Worker, nil
}

func userConfigPath() string {
	userDir := oauth.DefaultConfigDir()
	if userDir == "" {
		return ""
	}
	return filepath.Join(userDir, "config.json")
}

func workspaceConfigPath(workingDir string) string {
	for _, path := range projectConfigPaths(workingDir) {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return filepath.Join(workingDir, ".crawler-ai.json")
}

func normalizeRole(role string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(role))
	switch value {
	case "orchestrator", "worker":
		return value, nil
	default:
		return "", apperrors.New("config.normalizeRole", apperrors.CodeInvalidArgument, "role must be orchestrator or worker")
	}
}

func readScopeConfigFile(path string) (configFile, error) {
	if strings.TrimSpace(path) == "" {
		return configFile{}, apperrors.New("config.readScopeConfigFile", apperrors.CodeInvalidArgument, "config path must not be empty")
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return configFile{}, nil
	}
	if err != nil {
		return configFile{}, apperrors.Wrap("config.readScopeConfigFile", apperrors.CodeToolFailed, err, "read scoped config file")
	}

	var file configFile
	if err := json.Unmarshal(data, &file); err != nil {
		return configFile{}, apperrors.Wrap("config.readScopeConfigFile", apperrors.CodeInvalidConfig, err, "parse scoped config file")
	}
	return file, nil
}

func writeScopeConfigFile(path string, file configFile) error {
	if strings.TrimSpace(path) == "" {
		return apperrors.New("config.writeScopeConfigFile", apperrors.CodeInvalidArgument, "config path must not be empty")
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return apperrors.Wrap("config.writeScopeConfigFile", apperrors.CodeToolFailed, err, "marshal scoped config file")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return apperrors.Wrap("config.writeScopeConfigFile", apperrors.CodeToolFailed, err, "create config directory")
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return apperrors.Wrap("config.writeScopeConfigFile", apperrors.CodeToolFailed, err, "write scoped config file")
	}
	return nil
}

func scopeHasRole(file configFile, role string) bool {
	if file.Models == nil {
		return false
	}
	if role == "orchestrator" {
		return file.Models.Orchestrator != nil
	}
	return file.Models.Worker != nil
}

func recordRecentModel(current RecentModelsConfig, role string, providerCfg ProviderConfig) RecentModelsConfig {
	if strings.TrimSpace(providerCfg.Provider) == "" || strings.TrimSpace(providerCfg.Model) == "" {
		return current
	}

	entry := RecentModel{
		Provider: providerCfg.Provider,
		Model:    providerCfg.Model,
		BaseURL:  providerCfg.BaseURL,
	}

	var source []RecentModel
	if role == "orchestrator" {
		source = current.Orchestrator
	} else {
		source = current.Worker
	}

	updated := make([]RecentModel, 0, len(source)+1)
	updated = append(updated, entry)
	for _, existing := range source {
		if existing.Provider == entry.Provider && existing.Model == entry.Model && existing.BaseURL == entry.BaseURL {
			continue
		}
		updated = append(updated, existing)
		if len(updated) == maxRecentModelsPerRole {
			break
		}
	}

	if role == "orchestrator" {
		current.Orchestrator = updated
	} else {
		current.Worker = updated
	}
	return current
}

func setRecentModelsForRole(file *recentModelsFile, role string, models []recentModelFile) {
	if role == "orchestrator" {
		file.Orchestrator = models
		return
	}
	file.Worker = models
}

func recentModelFilesForRole(current RecentModelsConfig, role string) []recentModelFile {
	var source []RecentModel
	if role == "orchestrator" {
		source = current.Orchestrator
	} else {
		source = current.Worker
	}
	if source == nil {
		return nil
	}
	result := make([]recentModelFile, len(source))
	for index, item := range source {
		result[index] = recentModelFile{
			Provider: item.Provider,
			Model:    item.Model,
			BaseURL:  item.BaseURL,
		}
	}
	return result
}

func setNestedMapValue(target map[string]any, keyPath string, value any) map[string]any {
	parts := splitConfigPath(keyPath)
	if len(parts) == 0 {
		return target
	}
	if target == nil {
		target = make(map[string]any)
	}

	current := target
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok || next == nil {
			next = make(map[string]any)
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = cloneAnyValue(value)
	return target
}

func deleteNestedMapValue(target map[string]any, keyPath string) map[string]any {
	parts := splitConfigPath(keyPath)
	if len(parts) == 0 || target == nil {
		return target
	}
	deleteNestedMapValueRecursive(target, parts)
	if len(target) == 0 {
		return nil
	}
	return target
}

func deleteNestedMapValueRecursive(target map[string]any, parts []string) bool {
	if len(parts) == 1 {
		delete(target, parts[0])
		return len(target) == 0
	}

	next, ok := target[parts[0]].(map[string]any)
	if !ok || next == nil {
		return len(target) == 0
	}
	if deleteNestedMapValueRecursive(next, parts[1:]) {
		delete(target, parts[0])
	}
	return len(target) == 0
}

func splitConfigPath(keyPath string) []string {
	parts := strings.Split(strings.TrimSpace(keyPath), ".")
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return filtered
}
