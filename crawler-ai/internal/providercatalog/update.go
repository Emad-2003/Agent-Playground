package providercatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"crawler-ai/internal/oauth"
)

type UpdateResult struct {
	Applied int
	Ignored []string
	Source  string
}

func OverlayPath() string {
	configDir := oauth.DefaultConfigDir()
	if strings.TrimSpace(configDir) == "" {
		return ""
	}
	return filepath.Join(configDir, "providers.json")
}

func Update(pathOrURL string) (UpdateResult, error) {
	definitions, source, err := loadDefinitionsFromSource(pathOrURL)
	if err != nil {
		return UpdateResult{}, err
	}

	supported := buildDefinitionsByID(Embedded())
	filtered := make([]Definition, 0, len(definitions))
	ignored := make([]string, 0)
	for _, definition := range definitions {
		id := normalize(definition.ID)
		if _, ok := supported[id]; !ok {
			ignored = append(ignored, definition.ID)
			continue
		}
		filtered = append(filtered, cloneDefinition(definition))
	}

	if err := saveOverlay(filtered); err != nil {
		return UpdateResult{}, err
	}

	return UpdateResult{Applied: len(filtered), Ignored: ignored, Source: source}, nil
}

func loadOverlay() ([]Definition, error) {
	path := OverlayPath()
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseDefinitions(data)
}

func saveOverlay(definitions []Definition) error {
	path := OverlayPath()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("provider catalog path is not available")
	}
	data, err := json.MarshalIndent(definitions, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func loadDefinitionsFromSource(pathOrURL string) ([]Definition, string, error) {
	trimmed := strings.TrimSpace(pathOrURL)
	if trimmed == "" || strings.EqualFold(trimmed, "embedded") {
		return Embedded(), "embedded", nil
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "http://") || strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmed, nil)
		if err != nil {
			return nil, "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return nil, "", fmt.Errorf("provider catalog request returned status %d", resp.StatusCode)
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", err
		}
		definitions, err := parseDefinitions(data)
		return definitions, trimmed, err
	}

	data, err := os.ReadFile(trimmed)
	if err != nil {
		return nil, "", err
	}
	definitions, err := parseDefinitions(data)
	return definitions, trimmed, err
}

func parseDefinitions(data []byte) ([]Definition, error) {
	var definitions []Definition
	if err := json.Unmarshal(data, &definitions); err == nil {
		return normalizeDefinitions(definitions), nil
	}

	var payload struct {
		Providers []Definition `json:"providers"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return normalizeDefinitions(payload.Providers), nil
}

func normalizeDefinitions(definitions []Definition) []Definition {
	items := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		definition.ID = normalize(definition.ID)
		definition.DisplayName = strings.TrimSpace(definition.DisplayName)
		if definition.DisplayName == "" {
			definition.DisplayName = definition.ID
		}
		normalizedModels := make([]Model, 0, len(definition.Models)+len(definition.KnownModels))
		seenModels := make(map[string]struct{}, len(definition.Models)+len(definition.KnownModels))
		for _, model := range definition.Models {
			model.ID = strings.TrimSpace(model.ID)
			if model.ID == "" {
				continue
			}
			if model.DisplayName == "" {
				model.DisplayName = model.ID
			}
			trimmedAliases := make([]string, 0, len(model.Aliases))
			for _, alias := range model.Aliases {
				if trimmed := strings.TrimSpace(alias); trimmed != "" {
					trimmedAliases = append(trimmedAliases, trimmed)
				}
			}
			model.Aliases = trimmedAliases
			key := normalize(model.ID)
			if _, ok := seenModels[key]; ok {
				continue
			}
			seenModels[key] = struct{}{}
			normalizedModels = append(normalizedModels, cloneModel(model))
		}
		for _, modelID := range definition.KnownModels {
			trimmed := strings.TrimSpace(modelID)
			if trimmed == "" {
				continue
			}
			key := normalize(trimmed)
			if _, ok := seenModels[key]; ok {
				continue
			}
			seenModels[key] = struct{}{}
			normalizedModels = append(normalizedModels, Model{ID: trimmed, DisplayName: trimmed})
		}
		definition.Models = normalizedModels
		definition.KnownModels = make([]string, 0, len(normalizedModels))
		for _, model := range normalizedModels {
			definition.KnownModels = append(definition.KnownModels, model.ID)
		}
		items = append(items, cloneDefinition(definition))
	}
	return items
}
