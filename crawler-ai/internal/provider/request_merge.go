package provider

import "crawler-ai/internal/config"

func mergeRequestHeaders(cfg config.ProviderConfig, request Request, base map[string]string) map[string]string {
	merged := cloneStringMap(base)
	for key, value := range cfg.ExtraHeaders {
		merged[key] = value
	}
	for key, value := range request.Headers {
		merged[key] = value
	}
	return merged
}

func mergeRequestBody(cfg config.ProviderConfig, request Request, base map[string]any, includeExtraBody bool) map[string]any {
	merged := cloneAnyMap(base)
	mergeAnyMaps(merged, cfg.ProviderOptions)
	mergeAnyMaps(merged, request.ProviderOptions)
	if includeExtraBody {
		mergeAnyMaps(merged, cfg.ExtraBody)
	}
	mergeAnyMaps(merged, request.Body)
	return merged
}

func mergeAnyMaps(target map[string]any, overlay map[string]any) {
	if target == nil || overlay == nil {
		return
	}
	for key, value := range overlay {
		if existing, ok := target[key].(map[string]any); ok {
			if next, ok := value.(map[string]any); ok {
				mergedChild := cloneAnyMap(existing)
				mergeAnyMaps(mergedChild, next)
				target[key] = mergedChild
				continue
			}
		}
		target[key] = cloneAnyValue(value)
	}
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneAnyMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
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
