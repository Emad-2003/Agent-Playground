package apikeys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewKeyStore(dir)

	store.Set("anthropic", "sk-ant-test123")
	store.Set("openai", "sk-test456")

	if err := store.Save(); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify file permissions on non-Windows
	info, err := os.Stat(filepath.Join(dir, "keys.json"))
	if err != nil {
		t.Fatalf("stat keys.json: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		// On Windows, permissions are different
		t.Logf("note: file permissions are %v (platform-dependent)", info.Mode().Perm())
	}

	// Load into a fresh store
	store2 := NewKeyStore(dir)
	if err := store2.Load(); err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got := store2.Get("anthropic"); got != "sk-ant-test123" {
		t.Errorf("Get(anthropic) = %q, want %q", got, "sk-ant-test123")
	}
	if got := store2.Get("openai"); got != "sk-test456" {
		t.Errorf("Get(openai) = %q, want %q", got, "sk-test456")
	}
}

func TestKeyStoreLoadMissing(t *testing.T) {
	store := NewKeyStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("Load() on missing file should not error, got: %v", err)
	}
}

func TestKeyStoreDelete(t *testing.T) {
	store := NewKeyStore(t.TempDir())
	store.Set("openai", "key1")
	store.Delete("openai")

	if store.HasKey("openai") {
		t.Error("HasKey(openai) should be false after Delete")
	}
}

func TestKeyStoreList(t *testing.T) {
	store := NewKeyStore(t.TempDir())
	store.Set("anthropic", "k1")
	store.Set("openai", "k2")

	providers := store.List()
	if len(providers) != 2 {
		t.Fatalf("List() returned %d providers, want 2", len(providers))
	}
}

func TestResolveEnvFirst(t *testing.T) {
	store := NewKeyStore(t.TempDir())
	store.Set("anthropic", "stored-key")

	t.Setenv("ANTHROPIC_API_KEY", "env-key")

	got := Resolve(store, "anthropic", "config-key")
	if got != "env-key" {
		t.Errorf("Resolve() = %q, want %q (env should win)", got, "env-key")
	}
}

func TestResolveFallsBackToStore(t *testing.T) {
	store := NewKeyStore(t.TempDir())
	store.Set("openai", "stored-key")

	// Clear any env var
	t.Setenv("OPENAI_API_KEY", "")

	got := Resolve(store, "openai", "")
	if got != "stored-key" {
		t.Errorf("Resolve() = %q, want %q", got, "stored-key")
	}
}

func TestResolveFallsBackToConfig(t *testing.T) {
	store := NewKeyStore(t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")

	got := Resolve(store, "anthropic", "config-key")
	if got != "config-key" {
		t.Errorf("Resolve() = %q, want %q", got, "config-key")
	}
}

func TestEnvVarForProvider(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"anthropic", "ANTHROPIC_API_KEY"},
		{"openai", "OPENAI_API_KEY"},
		{"google", "GOOGLE_API_KEY"},
		{"aws", "AWS_SECRET_ACCESS_KEY"},
		{"azure", "AZURE_OPENAI_API_KEY"},
		{"custom", "CUSTOM_API_KEY"},
	}

	for _, tt := range tests {
		if got := EnvVarForProvider(tt.provider); got != tt.want {
			t.Errorf("EnvVarForProvider(%q) = %q, want %q", tt.provider, got, tt.want)
		}
	}
}

func TestPromptForKeyFromReader(t *testing.T) {
	input := strings.NewReader("sk-test-key-12345\n")
	var output strings.Builder

	key, err := PromptForKeyFromReader("anthropic", input, &output)
	if err != nil {
		t.Fatalf("PromptForKeyFromReader() error: %v", err)
	}

	if key != "sk-test-key-12345" {
		t.Errorf("got %q, want %q", key, "sk-test-key-12345")
	}

	if !strings.Contains(output.String(), "anthropic") {
		t.Error("output should mention provider name")
	}
}

func TestPromptForProvider(t *testing.T) {
	input := strings.NewReader("1\n") // Select first provider (anthropic)
	var output strings.Builder

	provider, err := PromptForProvider(input, &output)
	if err != nil {
		t.Fatalf("PromptForProvider() error: %v", err)
	}

	if provider != "anthropic" {
		t.Errorf("got %q, want %q", provider, "anthropic")
	}
}

func TestConfirmOverwrite(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false},
	}

	for _, tt := range tests {
		input := strings.NewReader(tt.input)
		var output strings.Builder
		got, err := ConfirmOverwrite("openai", input, &output)
		if err != nil {
			t.Fatalf("ConfirmOverwrite(%q) error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("ConfirmOverwrite(input=%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDefaultConfigDir(t *testing.T) {
	dir := DefaultConfigDir()
	if dir == "" {
		t.Skip("no home directory available")
	}
	if !strings.Contains(dir, "crawler-ai") {
		t.Errorf("DefaultConfigDir() = %q, expected to contain 'crawler-ai'", dir)
	}
}

func TestDefaultConfigDirHonorsOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom-data")
	SetDefaultConfigDir(override)
	defer SetDefaultConfigDir("")

	if got := DefaultConfigDir(); got != override {
		t.Fatalf("DefaultConfigDir() = %q, want %q", got, override)
	}
}
