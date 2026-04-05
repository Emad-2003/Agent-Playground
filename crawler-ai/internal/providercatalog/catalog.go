package providercatalog

import "strings"

type AuthMode string

const (
	AuthModeNone   AuthMode = "none"
	AuthModeAPIKey AuthMode = "api_key"
	AuthModeOAuth  AuthMode = "oauth"
)

type Capabilities struct {
	Streaming    bool `json:"streaming"`
	SystemPrompt bool `json:"system_prompt"`
}

type Pricing struct {
	InputPerMillionTokensUSD  float64 `json:"input_per_million_tokens_usd,omitempty"`
	OutputPerMillionTokensUSD float64 `json:"output_per_million_tokens_usd,omitempty"`
}

type Model struct {
	ID            string   `json:"id"`
	DisplayName   string   `json:"display_name,omitempty"`
	ContextWindow int64    `json:"context_window,omitempty"`
	Aliases       []string `json:"aliases,omitempty"`
	Pricing       Pricing  `json:"pricing,omitempty"`
}

type Definition struct {
	ID              string       `json:"id"`
	DisplayName     string       `json:"display_name"`
	AuthMode        AuthMode     `json:"auth_mode"`
	RequiresBaseURL bool         `json:"requires_base_url"`
	RequiresAPIKey  bool         `json:"requires_api_key"`
	DefaultBaseURL  string       `json:"default_base_url"`
	Capabilities    Capabilities `json:"capabilities"`
	Models          []Model      `json:"models,omitempty"`
	KnownModels     []string     `json:"known_models"`
}

var embeddedDefinitions = []Definition{
	{
		ID:          "mock",
		DisplayName: "Mock",
		AuthMode:    AuthModeNone,
		Capabilities: Capabilities{
			Streaming:    false,
			SystemPrompt: true,
		},
		Models: []Model{
			{ID: "mock-orchestrator-v1", DisplayName: "Mock Orchestrator v1"},
			{ID: "mock-worker-v1", DisplayName: "Mock Worker v1"},
		},
	},
	{
		ID:              "anthropic",
		DisplayName:     "Anthropic",
		AuthMode:        AuthModeAPIKey,
		RequiresBaseURL: true,
		RequiresAPIKey:  true,
		DefaultBaseURL:  "https://api.anthropic.com/v1",
		Capabilities: Capabilities{
			Streaming:    true,
			SystemPrompt: true,
		},
		Models: []Model{
			{ID: "claude-sonnet-4-20250514", DisplayName: "Claude Sonnet 4", Pricing: Pricing{InputPerMillionTokensUSD: 3.00, OutputPerMillionTokensUSD: 15.00}},
			{ID: "claude-opus-4-20250514", DisplayName: "Claude Opus 4", Pricing: Pricing{InputPerMillionTokensUSD: 15.00, OutputPerMillionTokensUSD: 75.00}},
			{ID: "claude-3-5-haiku-20241022", DisplayName: "Claude 3.5 Haiku", Pricing: Pricing{InputPerMillionTokensUSD: 0.80, OutputPerMillionTokensUSD: 4.00}},
		},
	},
	{
		ID:              "openai",
		DisplayName:     "OpenAI-Compatible",
		AuthMode:        AuthModeAPIKey,
		RequiresBaseURL: true,
		RequiresAPIKey:  true,
		DefaultBaseURL:  "https://api.openai.com/v1",
		Capabilities: Capabilities{
			Streaming:    true,
			SystemPrompt: true,
		},
		Models: []Model{
			{ID: "gpt-4o", DisplayName: "GPT-4o", Pricing: Pricing{InputPerMillionTokensUSD: 5.00, OutputPerMillionTokensUSD: 15.00}},
			{ID: "gpt-4o-mini", DisplayName: "GPT-4o mini", Pricing: Pricing{InputPerMillionTokensUSD: 0.15, OutputPerMillionTokensUSD: 0.60}},
			{ID: "o3-mini", DisplayName: "o3-mini", Pricing: Pricing{InputPerMillionTokensUSD: 1.10, OutputPerMillionTokensUSD: 4.40}},
			{ID: "o1", DisplayName: "o1", Pricing: Pricing{InputPerMillionTokensUSD: 15.00, OutputPerMillionTokensUSD: 60.00}},
		},
	},
}

func LookupModel(providerID, modelID string) (Model, bool) {
	definition, ok := Get(providerID)
	if !ok {
		return Model{}, false
	}
	needle := normalize(modelID)
	for _, model := range definition.Models {
		if normalize(model.ID) == needle {
			return cloneModel(model), true
		}
		for _, alias := range model.Aliases {
			if normalize(alias) == needle {
				return cloneModel(model), true
			}
		}
	}
	return Model{}, false
}

func EstimateCostUSD(providerID, modelID string, inputTokens, outputTokens int) (float64, bool) {
	model, ok := LookupModel(providerID, modelID)
	if !ok {
		return 0, false
	}
	if model.Pricing.InputPerMillionTokensUSD <= 0 && model.Pricing.OutputPerMillionTokensUSD <= 0 {
		return 0, false
	}
	inputCost := (float64(inputTokens) / 1_000_000) * model.Pricing.InputPerMillionTokensUSD
	outputCost := (float64(outputTokens) / 1_000_000) * model.Pricing.OutputPerMillionTokensUSD
	return inputCost + outputCost, true
}

func List() []Definition {
	definitions := mergedDefinitions()
	items := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		items = append(items, cloneDefinition(definition))
	}
	return items
}

func Get(id string) (Definition, bool) {
	definition, ok := buildDefinitionsByID(mergedDefinitions())[normalize(id)]
	if !ok {
		return Definition{}, false
	}
	return cloneDefinition(definition), true
}

func Embedded() []Definition {
	normalized := normalizeDefinitions(embeddedDefinitions)
	items := make([]Definition, 0, len(normalized))
	for _, definition := range normalized {
		items = append(items, cloneDefinition(definition))
	}
	return items
}

func mergedDefinitions() []Definition {
	definitions := Embedded()
	overlay, err := loadOverlay()
	if err != nil || len(overlay) == 0 {
		return definitions
	}

	byID := buildDefinitionsByID(definitions)
	for _, definition := range overlay {
		if _, ok := byID[normalize(definition.ID)]; !ok {
			continue
		}
		byID[normalize(definition.ID)] = cloneDefinition(definition)
	}

	merged := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		merged = append(merged, cloneDefinition(byID[normalize(definition.ID)]))
	}
	return merged
}

func buildDefinitionsByID(definitions []Definition) map[string]Definition {
	items := make(map[string]Definition, len(definitions))
	for _, definition := range definitions {
		items[normalize(definition.ID)] = cloneDefinition(definition)
	}
	return items
}

func cloneDefinition(definition Definition) Definition {
	definition.Models = cloneModels(definition.Models)
	definition.KnownModels = append([]string(nil), definition.KnownModels...)
	return definition
}

func cloneModels(models []Model) []Model {
	cloned := make([]Model, 0, len(models))
	for _, model := range models {
		cloned = append(cloned, cloneModel(model))
	}
	return cloned
}

func cloneModel(model Model) Model {
	model.Aliases = append([]string(nil), model.Aliases...)
	return model
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
