package apikeys

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	apperrors "crawler-ai/internal/errors"
)

const DataDirEnvVar = "CRAWLER_AI_DATA_DIR"

var (
	defaultConfigDirMu       sync.RWMutex
	defaultConfigDirOverride string
)

// KeyStore manages API key persistence with secure file permissions.
type KeyStore struct {
	mu   sync.RWMutex
	path string
	keys map[string]string
}

// NewKeyStore creates a store rooted at the given directory.
// The keys are stored in <dir>/keys.json with restricted permissions.
func NewKeyStore(dir string) *KeyStore {
	return &KeyStore{
		path: filepath.Join(dir, "keys.json"),
		keys: make(map[string]string),
	}
}

// DefaultKeyStore returns a store at ~/.config/crawler-ai/keys.json.
func DefaultKeyStore() *KeyStore {
	return NewKeyStore(DefaultConfigDir())
}

func SetDefaultConfigDir(dir string) {
	defaultConfigDirMu.Lock()
	defer defaultConfigDirMu.Unlock()
	defaultConfigDirOverride = strings.TrimSpace(dir)
	if defaultConfigDirOverride != "" {
		defaultConfigDirOverride = filepath.Clean(defaultConfigDirOverride)
	}
}

func ConfigDirOverride() string {
	defaultConfigDirMu.RLock()
	override := defaultConfigDirOverride
	defaultConfigDirMu.RUnlock()
	if override != "" {
		return override
	}

	if envOverride := strings.TrimSpace(os.Getenv(DataDirEnvVar)); envOverride != "" {
		return filepath.Clean(envOverride)
	}

	return ""
}

// DefaultConfigDir returns ~/.config/crawler-ai on Unix, %APPDATA%/crawler-ai on Windows.
func DefaultConfigDir() string {
	if override := ConfigDirOverride(); override != "" {
		return override
	}

	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "crawler-ai")
		}
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	return filepath.Join(xdg, "crawler-ai")
}

// Load reads keys from disk. Returns nil if the file doesn't exist.
func (s *KeyStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return apperrors.Wrap("apikeys.Load", apperrors.CodeToolFailed, err, "read keys file")
	}

	var keys map[string]string
	if err := json.Unmarshal(data, &keys); err != nil {
		return apperrors.Wrap("apikeys.Load", apperrors.CodeInvalidConfig, err, "parse keys file")
	}

	s.keys = keys
	return nil
}

// Save writes keys to disk with restricted permissions (0600).
func (s *KeyStore) Save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.keys, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return apperrors.Wrap("apikeys.Save", apperrors.CodeToolFailed, err, "marshal keys")
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return apperrors.Wrap("apikeys.Save", apperrors.CodeToolFailed, err, "create config directory")
	}

	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return apperrors.Wrap("apikeys.Save", apperrors.CodeToolFailed, err, "write keys file")
	}

	return nil
}

// Set stores a key for the given provider.
func (s *KeyStore) Set(provider, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[provider] = key
}

// Get returns the key for the given provider, or empty string if not found.
func (s *KeyStore) Get(provider string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.keys[provider]
}

// Delete removes the key for the given provider.
func (s *KeyStore) Delete(provider string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, provider)
}

// List returns all stored provider names.
func (s *KeyStore) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	providers := make([]string, 0, len(s.keys))
	for p := range s.keys {
		providers = append(providers, p)
	}
	return providers
}

// HasKey returns whether a key is stored for the given provider.
func (s *KeyStore) HasKey(provider string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.keys[provider]
	return ok
}

// Path returns the file path of the key store.
func (s *KeyStore) Path() string {
	return s.path
}

// Resolve resolves an API key for a provider using the priority:
// 1. Environment variable (e.g. ANTHROPIC_API_KEY)
// 2. Stored key from keys.json
// 3. Config-provided key (passthrough)
func Resolve(store *KeyStore, provider, configKey string) string {
	// 1. Check environment variable
	envKey := envVarForProvider(provider)
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		return v
	}

	// 2. Check stored key
	if store != nil {
		if v := store.Get(provider); v != "" {
			return v
		}
	}

	// 3. Fall back to config value
	return configKey
}

// envVarForProvider maps provider names to their conventional env var names.
func envVarForProvider(provider string) string {
	switch strings.ToLower(provider) {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "google", "vertex", "gemini":
		return "GOOGLE_API_KEY"
	case "aws", "bedrock":
		return "AWS_SECRET_ACCESS_KEY"
	case "azure":
		return "AZURE_OPENAI_API_KEY"
	case "groq":
		return "GROQ_API_KEY"
	case "mistral":
		return "MISTRAL_API_KEY"
	case "deepseek":
		return "DEEPSEEK_API_KEY"
	case "xai":
		return "XAI_API_KEY"
	default:
		return strings.ToUpper(provider) + "_API_KEY"
	}
}

// EnvVarForProvider is the exported version for display purposes.
func EnvVarForProvider(provider string) string {
	return envVarForProvider(provider)
}

// SupportedProviders returns the list of supported provider names.
func SupportedProviders() []string {
	return []string{
		"anthropic",
		"openai",
		"google",
		"aws",
		"azure",
		"groq",
		"mistral",
		"deepseek",
		"xai",
	}
}
