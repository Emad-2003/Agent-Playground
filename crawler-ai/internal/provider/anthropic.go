package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	body := mergeRequestBody(p.config, request, map[string]any{
		"model":       request.Model,
		"messages":    serializeAnthropicMessages(request.Messages),
		"system":      request.SystemPrompt,
		"max_tokens":  request.MaxTokens,
		"temperature": request.Temperature,
	}, false)
	if len(request.Tools) > 0 {
		body["tools"] = serializeAnthropicTools(request.Tools)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, apperrors.Wrap("provider.Anthropic.Complete", apperrors.CodeProviderFailed, err, "marshal request body")
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.config.BaseURL, "/")+"/messages", bytes.NewReader(payload))
	if err != nil {
		return Response{}, apperrors.Wrap("provider.Anthropic.Complete", apperrors.CodeProviderFailed, err, "create HTTP request")
	}
	for key, value := range mergeRequestHeaders(p.config, request, map[string]string{
		"x-api-key":         p.config.APIKey,
		"anthropic-version": "2023-06-01",
		"Content-Type":      "application/json",
	}) {
		httpRequest.Header.Set(key, value)
	}

	httpResponse, err := p.client.Do(httpRequest)
	if err != nil {
		return Response{}, apperrors.Wrap("provider.Anthropic.Complete", apperrors.CodeProviderFailed, err, "send HTTP request")
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode >= 300 {
		return Response{}, newStatusErrorFromResponse("provider.Anthropic.Complete", httpResponse, "provider returned non-success status")
	}

	response, err := decodeAnthropicResponse(httpResponse.Body)
	if err != nil {
		return Response{}, apperrors.Wrap("provider.Anthropic.Complete", apperrors.CodeProviderFailed, err, "decode response body")
	}
	return response, nil
}

type anthropicMessageResponse struct {
	StopReason string                  `json:"stop_reason"`
	Content    []anthropicContentBlock `json:"content"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

func decodeAnthropicResponse(r io.Reader) (Response, error) {
	var decoded anthropicMessageResponse
	if err := json.NewDecoder(r).Decode(&decoded); err != nil {
		return Response{}, err
	}
	if len(decoded.Content) == 0 {
		return Response{}, apperrors.New("provider.decodeAnthropicResponse", apperrors.CodeProviderFailed, "provider returned no content")
	}

	textParts := make([]string, 0, len(decoded.Content))
	reasoningParts := make([]string, 0, len(decoded.Content))
	content := make([]ContentBlock, 0, len(decoded.Content))
	for _, block := range decoded.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
			content = append(content, TextBlock(block.Text))
		case "thinking":
			reasoningParts = append(reasoningParts, block.Thinking)
			content = append(content, ReasoningBlock(block.Thinking))
		case "tool_use":
			callID := strings.TrimSpace(block.ID)
			if callID == "" {
				continue
			}
			content = append(content, ToolCallBlock(ToolCall{
				ID:       callID,
				Name:     strings.TrimSpace(block.Name),
				Input:    normalizeStructuredPayload(block.Input),
				Finished: true,
			}))
		default:
			if result, ok := anthropicToolResultBlock(block); ok {
				content = append(content, result)
			}
		}
	}

	return Response{
		Text:         strings.Join(textParts, "\n"),
		Reasoning:    strings.Join(reasoningParts, "\n"),
		Content:      content,
		FinishReason: decoded.StopReason,
		Usage: Usage{
			InputTokens:  decoded.Usage.InputTokens,
			OutputTokens: decoded.Usage.OutputTokens,
		},
	}, nil
}

func normalizeStructuredPayload(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return trimmed
	}
	if text, ok := decoded.(string); ok {
		return text
	}
	compacted, err := json.Marshal(decoded)
	if err != nil {
		return trimmed
	}
	return string(compacted)
}

func anthropicToolResultBlock(block anthropicContentBlock) (ContentBlock, bool) {
	if !isAnthropicToolResultType(block.Type) {
		return ContentBlock{}, false
	}
	toolCallID := strings.TrimSpace(block.ToolUseID)
	if toolCallID == "" {
		toolCallID = strings.TrimSpace(block.ID)
	}
	if toolCallID == "" {
		return ContentBlock{}, false
	}
	return ToolResultBlock(ToolResult{
		ToolCallID: toolCallID,
		Name:       anthropicToolResultName(block.Type, block.Name),
		Content:    normalizeStructuredPayload(block.Content),
		IsError:    block.IsError,
	}), true
}

func isAnthropicToolResultType(kind string) bool {
	trimmed := strings.TrimSpace(kind)
	return trimmed == "tool_result" || strings.HasSuffix(trimmed, "_tool_result")
}

func anthropicToolResultName(kind string, explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	trimmed := strings.TrimSpace(kind)
	trimmed = strings.TrimSuffix(trimmed, "_tool_result")
	trimmed = strings.TrimSuffix(trimmed, "_result")
	trimmed = strings.ReplaceAll(trimmed, "_", " ")
	return strings.TrimSpace(trimmed)
}

func appendJSONStringFragment(current string, fragment string) string {
	trimmed := strings.TrimSpace(fragment)
	if trimmed == "" {
		return current
	}
	if strings.TrimSpace(current) == "{}" {
		return trimmed
	}
	if current == "" {
		return trimmed
	}
	return current + trimmed
}

func anthropicDeltaToolCall(index int, call *ToolCall) (ContentBlock, error) {
	if call == nil || strings.TrimSpace(call.ID) == "" {
		return ContentBlock{}, fmt.Errorf("missing tool call id for index %d", index)
	}
	return ToolCallBlock(ToolCall{
		ID:       call.ID,
		Name:     call.Name,
		Input:    call.Input,
		Finished: call.Finished,
	}), nil
}
