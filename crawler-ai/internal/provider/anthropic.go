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

type Anthropic struct {
	config config.ProviderConfig
	client *http.Client
}

func NewAnthropic(cfg config.ProviderConfig) *Anthropic {
	return &Anthropic{config: cfg, client: http.DefaultClient}
}

func (p *Anthropic) Name() string {
	return "anthropic"
}

func (p *Anthropic) Complete(ctx context.Context, request Request) (Response, error) {
	body := map[string]any{
		"model":       request.Model,
		"messages":    request.Messages,
		"system":      request.SystemPrompt,
		"max_tokens":  request.MaxTokens,
		"temperature": request.Temperature,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, apperrors.Wrap("provider.Anthropic.Complete", apperrors.CodeProviderFailed, err, "marshal request body")
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.config.BaseURL, "/")+"/messages", bytes.NewReader(payload))
	if err != nil {
		return Response{}, apperrors.Wrap("provider.Anthropic.Complete", apperrors.CodeProviderFailed, err, "create HTTP request")
	}
	httpRequest.Header.Set("x-api-key", p.config.APIKey)
	httpRequest.Header.Set("anthropic-version", "2023-06-01")
	httpRequest.Header.Set("Content-Type", "application/json")

	httpResponse, err := p.client.Do(httpRequest)
	if err != nil {
		return Response{}, apperrors.Wrap("provider.Anthropic.Complete", apperrors.CodeProviderFailed, err, "send HTTP request")
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode >= 300 {
		return Response{}, apperrors.New("provider.Anthropic.Complete", apperrors.CodeProviderFailed, "provider returned non-success status")
	}

	var decoded struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(httpResponse.Body).Decode(&decoded); err != nil {
		return Response{}, apperrors.Wrap("provider.Anthropic.Complete", apperrors.CodeProviderFailed, err, "decode response body")
	}
	if len(decoded.Content) == 0 {
		return Response{}, apperrors.New("provider.Anthropic.Complete", apperrors.CodeProviderFailed, "provider returned no content")
	}

	textParts := make([]string, 0, len(decoded.Content))
	for _, block := range decoded.Content {
		if block.Type == "text" {
			textParts = append(textParts, block.Text)
		}
	}

	return Response{
		Text: strings.Join(textParts, "\n"),
		Usage: Usage{
			InputTokens:  decoded.Usage.InputTokens,
			OutputTokens: decoded.Usage.OutputTokens,
		},
	}, nil
}
