package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"crawler-ai/internal/config"
	apperrors "crawler-ai/internal/errors"
)

type OpenAICompatible struct {
	config config.ProviderConfig
	client *http.Client
}

func NewOpenAICompatible(cfg config.ProviderConfig) *OpenAICompatible {
	return &OpenAICompatible{config: cfg, client: http.DefaultClient}
}

func (p *OpenAICompatible) Name() string {
	return "openai"
}

func (p *OpenAICompatible) Complete(ctx context.Context, request Request) (Response, error) {
	body := map[string]any{
		"model":       request.Model,
		"messages":    request.Messages,
		"max_tokens":  request.MaxTokens,
		"temperature": request.Temperature,
	}
	if strings.TrimSpace(request.SystemPrompt) != "" {
		body["messages"] = prependSystemMessage(request.Messages, request.SystemPrompt)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, apperrors.Wrap("provider.OpenAICompatible.Complete", apperrors.CodeProviderFailed, err, "marshal request body")
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.config.BaseURL, "/")+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Response{}, apperrors.Wrap("provider.OpenAICompatible.Complete", apperrors.CodeProviderFailed, err, "create HTTP request")
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	httpResponse, err := p.client.Do(httpRequest)
	if err != nil {
		return Response{}, apperrors.Wrap("provider.OpenAICompatible.Complete", apperrors.CodeProviderFailed, err, "send HTTP request")
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode >= 300 {
		return Response{}, apperrors.New("provider.OpenAICompatible.Complete", apperrors.CodeProviderFailed, "provider returned non-success status")
	}

	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(httpResponse.Body).Decode(&decoded); err != nil {
		return Response{}, apperrors.Wrap("provider.OpenAICompatible.Complete", apperrors.CodeProviderFailed, err, "decode response body")
	}
	if len(decoded.Choices) == 0 {
		return Response{}, apperrors.New("provider.OpenAICompatible.Complete", apperrors.CodeProviderFailed, "provider returned no choices")
	}

	return Response{
		Text: decoded.Choices[0].Message.Content,
		Usage: Usage{
			InputTokens:  decoded.Usage.PromptTokens,
			OutputTokens: decoded.Usage.CompletionTokens,
		},
	}, nil
}

func prependSystemMessage(messages []Message, systemPrompt string) []Message {
	combined := make([]Message, 0, len(messages)+1)
	combined = append(combined, Message{Role: "system", Content: systemPrompt})
	combined = append(combined, messages...)
	return combined
}
