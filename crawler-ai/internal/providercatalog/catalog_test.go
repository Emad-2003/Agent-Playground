package providercatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetReturnsKnownProvider(t *testing.T) {
	t.Parallel()

	definition, ok := Get("OpenAI")
	if !ok {
		t.Fatal("expected provider definition")
	}
	if definition.ID != "openai" {
		t.Fatalf("expected openai id, got %s", definition.ID)
	}
	if !definition.Capabilities.Streaming {
		t.Fatal("expected openai provider to support streaming")
	}
}

func TestGetRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	if _, ok := Get("gemini"); ok {
		t.Fatal("expected unknown provider lookup to fail")
	}
}

func TestListReturnsClonedDefinitions(t *testing.T) {
	t.Parallel()

	definitions := List()
	if len(definitions) < 3 {
		t.Fatalf("expected at least 3 provider definitions, got %d", len(definitions))
	}

	definitions[0].KnownModels[0] = "changed"
	reloaded, _ := Get(definitions[0].ID)
	if reloaded.KnownModels[0] == "changed" {
		t.Fatal("expected known models to be cloned")
	}
	if len(definitions[0].Models) > 0 {
		definitions[0].Models[0].ID = "changed-model"
		reloaded, _ = Get(definitions[0].ID)
		if reloaded.Models[0].ID == "changed-model" {
			t.Fatal("expected models to be cloned")
		}
	}
}

func TestLookupModelAndEstimateCost(t *testing.T) {
	t.Parallel()

	model, ok := LookupModel("openai", "gpt-4o")
	if !ok {
		t.Fatal("expected openai model metadata")
	}
	if model.Pricing.InputPerMillionTokensUSD <= 0 || model.Pricing.OutputPerMillionTokensUSD <= 0 {
		t.Fatalf("expected built-in pricing metadata, got %#v", model)
	}
	cost, ok := EstimateCostUSD("openai", "gpt-4o", 1_000_000, 500_000)
	if !ok {
		t.Fatal("expected cost estimate to be available")
	}
	if cost <= 0 {
		t.Fatalf("expected positive cost estimate, got %f", cost)
	}
}

func TestUpdateAcceptsStructuredModelsOverlay(t *testing.T) {
	userDir := t.TempDir()
	t.Setenv("APPDATA", userDir)

	source := filepath.Join(t.TempDir(), "providers-models.json")
	data := `[
	  {
	    "id": "openai",
	    "display_name": "OpenAI Updated",
	    "auth_mode": "api_key",
	    "requires_base_url": true,
	    "requires_api_key": true,
	    "default_base_url": "https://example.com/v1",
	    "capabilities": {"streaming": true, "system_prompt": true},
	    "models": [
	      {
	        "id": "gpt-priced",
	        "pricing": {
	          "input_per_million_tokens_usd": 1.25,
	          "output_per_million_tokens_usd": 5.00
	        }
	      }
	    ]
	  }
	]`
	if err := os.WriteFile(source, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	if _, err := Update(source); err != nil {
		t.Fatalf("Update() error: %v", err)
	}

	definition, ok := Get("openai")
	if !ok {
		t.Fatal("expected updated provider definition")
	}
	if len(definition.Models) != 1 || definition.Models[0].ID != "gpt-priced" {
		t.Fatalf("expected structured models overlay, got %#v", definition.Models)
	}
	if definition.KnownModels[0] != "gpt-priced" {
		t.Fatalf("expected known models to derive from structured models, got %#v", definition.KnownModels)
	}
}

func TestUpdateWritesOverlayAndListUsesIt(t *testing.T) {
	userDir := t.TempDir()
	t.Setenv("APPDATA", userDir)

	source := filepath.Join(t.TempDir(), "providers.json")
	data := `[
	  {
	    "id": "openai",
	    "display_name": "OpenAI Updated",
	    "auth_mode": "api_key",
	    "requires_base_url": true,
	    "requires_api_key": true,
	    "default_base_url": "https://example.com/v1",
	    "capabilities": {"streaming": true, "system_prompt": true},
	    "known_models": ["gpt-test"]
	  },
	  {
	    "id": "unknown-provider",
	    "display_name": "Unknown",
	    "auth_mode": "none"
	  }
	]`
	if err := os.WriteFile(source, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	result, err := Update(source)
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if result.Applied != 1 {
		t.Fatalf("expected 1 applied provider, got %d", result.Applied)
	}
	if len(result.Ignored) != 1 || result.Ignored[0] != "unknown-provider" {
		t.Fatalf("expected unknown provider to be ignored, got %#v", result.Ignored)
	}
	if _, err := os.Stat(OverlayPath()); err != nil {
		t.Fatalf("expected overlay file to exist: %v", err)
	}

	definition, ok := Get("openai")
	if !ok {
		t.Fatal("expected updated provider definition")
	}
	if definition.DisplayName != "OpenAI Updated" {
		t.Fatalf("expected overlay display name, got %s", definition.DisplayName)
	}
	if definition.DefaultBaseURL != "https://example.com/v1" {
		t.Fatalf("expected overlay base url, got %s", definition.DefaultBaseURL)
	}
	if len(definition.KnownModels) != 1 || definition.KnownModels[0] != "gpt-test" {
		t.Fatalf("expected overlay known models, got %#v", definition.KnownModels)
	}

	embedded, ok := buildDefinitionsByID(Embedded())["anthropic"]
	if !ok {
		t.Fatal("expected anthropic embedded provider")
	}
	merged, ok := Get("anthropic")
	if !ok {
		t.Fatal("expected anthropic provider")
	}
	if merged.DisplayName != embedded.DisplayName {
		t.Fatalf("expected untouched provider to preserve embedded definition, got %s", merged.DisplayName)
	}
}
