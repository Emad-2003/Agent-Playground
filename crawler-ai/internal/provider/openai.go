package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
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
	body := mergeRequestBody(p.config, request, map[string]any{
		"model":       request.Model,
		"messages":    serializeOpenAIMessages(request.Messages),
		"max_tokens":  request.MaxTokens,
		"temperature": request.Temperature,
	}, true)
	if strings.TrimSpace(request.SystemPrompt) != "" {
		body["messages"] = serializeOpenAIMessages(prependSystemMessage(request.Messages, request.SystemPrompt))
	}
	if len(request.Tools) > 0 {
		body["tools"] = serializeOpenAITools(request.Tools)
		if _, ok := body["tool_choice"]; !ok {
			body["tool_choice"] = "auto"
		}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, apperrors.Wrap("provider.OpenAICompatible.Complete", apperrors.CodeProviderFailed, err, "marshal request body")
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.config.BaseURL, "/")+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Response{}, apperrors.Wrap("provider.OpenAICompatible.Complete", apperrors.CodeProviderFailed, err, "create HTTP request")
	}
	for key, value := range mergeRequestHeaders(p.config, request, map[string]string{
		"Authorization": "Bearer " + p.config.APIKey,
		"Content-Type":  "application/json",
	}) {
		httpRequest.Header.Set(key, value)
	}

	httpResponse, err := p.client.Do(httpRequest)
	if err != nil {
		return Response{}, apperrors.Wrap("provider.OpenAICompatible.Complete", apperrors.CodeProviderFailed, err, "send HTTP request")
	}
	defer httpResponse.Body.Close()

	if httpResponse.StatusCode >= 300 {
		return Response{}, newStatusErrorFromResponse("provider.OpenAICompatible.Complete", httpResponse, "provider returned non-success status")
	}

	response, err := decodeOpenAIResponse(httpResponse.Body)
	if err != nil {
		return Response{}, apperrors.Wrap("provider.OpenAICompatible.Complete", apperrors.CodeProviderFailed, err, "decode response body")
	}
	return response, nil
}

type openAIChatCompletionResponse struct {
	Choices []struct {
		FinishReason string            `json:"finish_reason"`
		Message      openAIChatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type openAIChatMessage struct {
	Content   json.RawMessage         `json:"content"`
	ToolCalls []openAIToolCallPayload `json:"tool_calls"`
}

type openAIContentPart struct {
	Type        string             `json:"type"`
	Text        string             `json:"text"`
	Refusal     string             `json:"refusal"`
	Output      json.RawMessage    `json:"output"`
	Content     json.RawMessage    `json:"content"`
	CallID      string             `json:"call_id"`
	ToolCallID  string             `json:"tool_call_id"`
	Name        string             `json:"name"`
	IsError     bool               `json:"is_error"`
	Annotations []openAIAnnotation `json:"annotations"`
}

type openAIAnnotation struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	FileID   string `json:"file_id"`
	Filename string `json:"filename"`
	Text     string `json:"text"`
}

type openAIToolCallPayload struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func decodeOpenAIResponse(r io.Reader) (Response, error) {
	var decoded openAIChatCompletionResponse
	if err := json.NewDecoder(r).Decode(&decoded); err != nil {
		return Response{}, err
	}
	if len(decoded.Choices) == 0 {
		return Response{}, apperrors.New("provider.decodeOpenAIResponse", apperrors.CodeProviderFailed, "provider returned no choices")
	}
	choice := decoded.Choices[0]
	text, content := buildOpenAIContent(choice.Message.Content, choice.Message.ToolCalls, true)
	return Response{
		Text:         text,
		Content:      content,
		FinishReason: choice.FinishReason,
		Usage: Usage{
			InputTokens:  decoded.Usage.PromptTokens,
			OutputTokens: decoded.Usage.CompletionTokens,
		},
	}, nil
}

func buildOpenAIContent(rawContent json.RawMessage, toolCalls []openAIToolCallPayload, finished bool) (string, []ContentBlock) {
	text, parts := openAIContentParts(rawContent)
	blocks := make([]ContentBlock, 0, len(toolCalls)+1)
	blocks = append(blocks, parts...)
	if len(parts) == 0 && strings.TrimSpace(text) != "" {
		blocks = append(blocks, TextBlock(text))
	}
	for _, toolCall := range toolCalls {
		id := strings.TrimSpace(toolCall.ID)
		if id == "" {
			continue
		}
		blocks = append(blocks, ToolCallBlock(ToolCall{
			ID:       id,
			Name:     strings.TrimSpace(toolCall.Function.Name),
			Input:    toolCall.Function.Arguments,
			Finished: finished,
		}))
	}
	if len(blocks) == 0 {
		return text, normalizeContentBlocks(nil, text, "")
	}
	return text, blocks
}

func openAIContentParts(raw json.RawMessage) (string, []ContentBlock) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if strings.TrimSpace(text) == "" {
			return text, nil
		}
		return text, []ContentBlock{TextBlock(text)}
	}
	var parts []openAIContentPart
	if err := json.Unmarshal(raw, &parts); err == nil {
		var builder strings.Builder
		blocks := make([]ContentBlock, 0, len(parts))
		for _, part := range parts {
			switch strings.TrimSpace(part.Type) {
			case "text", "output_text", "input_text":
				formatted := normalizeOpenAITextPart(part)
				builder.WriteString(formatted)
				if strings.TrimSpace(formatted) != "" {
					blocks = append(blocks, TextBlock(formatted))
				}
			case "refusal":
				formatted := normalizeOpenAIRefusalPart(part)
				builder.WriteString(formatted)
				if strings.TrimSpace(formatted) != "" {
					blocks = append(blocks, TextBlock(formatted))
				}
			default:
				if result, ok := openAIResultContentBlock(part); ok {
					blocks = append(blocks, result)
				}
			}
		}
		return builder.String(), blocks
	}
	return "", nil
}

func openAIResultContentBlock(part openAIContentPart) (ContentBlock, bool) {
	typeName := strings.TrimSpace(part.Type)
	if typeName != "tool_result" && typeName != "function_call_output" {
		return ContentBlock{}, false
	}
	toolCallID := strings.TrimSpace(part.ToolCallID)
	if toolCallID == "" {
		toolCallID = strings.TrimSpace(part.CallID)
	}
	if toolCallID == "" {
		return ContentBlock{}, false
	}
	content := normalizeOpenAIResultPayload(part)
	return ToolResultBlock(ToolResult{
		ToolCallID: toolCallID,
		Name:       strings.TrimSpace(part.Name),
		Content:    content,
		IsError:    part.IsError,
	}), true
}

func normalizeOpenAIResultPayload(part openAIContentPart) string {
	if strings.TrimSpace(part.Text) != "" {
		return part.Text
	}
	if strings.TrimSpace(string(part.Output)) != "" && string(part.Output) != "null" {
		return normalizeStructuredPayload(part.Output)
	}
	if strings.TrimSpace(string(part.Content)) != "" && string(part.Content) != "null" {
		return normalizeStructuredPayload(part.Content)
	}
	return ""
}

func normalizeOpenAITextPart(part openAIContentPart) string {
	text := part.Text
	annotations := formatOpenAIAnnotations(part.Annotations)
	if annotations == "" {
		return text
	}
	if strings.TrimSpace(text) == "" {
		return annotations
	}
	return text + "\n\n" + annotations
}

func normalizeOpenAIRefusalPart(part openAIContentPart) string {
	trimmed := strings.TrimSpace(part.Refusal)
	if trimmed == "" {
		return ""
	}
	return "Refusal:\n" + trimmed
}

func openAIStreamTextChunk(rawContent json.RawMessage, refusal string, annotations []openAIAnnotation, hasPriorText bool) string {
	parts := make([]string, 0, 3)
	if len(rawContent) > 0 && string(rawContent) != "null" {
		if text, blocks := openAIContentParts(rawContent); strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		} else {
			for _, block := range blocks {
				if block.Kind == ContentBlockText && strings.TrimSpace(block.Text) != "" {
					parts = append(parts, block.Text)
				}
			}
		}
	}
	if len(annotations) > 0 {
		annotationText := formatOpenAIAnnotations(annotations)
		if strings.TrimSpace(annotationText) != "" {
			if len(parts) > 0 || hasPriorText {
				annotationText = "\n\n" + annotationText
			}
			parts = append(parts, annotationText)
		}
	}
	if strings.TrimSpace(refusal) != "" {
		refusalText := normalizeOpenAIRefusalPart(openAIContentPart{Refusal: refusal})
		if len(parts) > 0 || hasPriorText {
			refusalText = "\n\n" + refusalText
		}
		parts = append(parts, refusalText)
	}
	return strings.Join(parts, "")
}

func formatOpenAIAnnotations(annotations []openAIAnnotation) string {
	formatted := make([]string, 0, len(annotations))
	for index, annotation := range annotations {
		line := formatOpenAIAnnotation(annotation)
		if line == "" {
			continue
		}
		formatted = append(formatted, "["+strconv.Itoa(index+1)+"] "+line)
	}
	return strings.Join(formatted, "\n")
}

func formatOpenAIAnnotation(annotation openAIAnnotation) string {
	parts := make([]string, 0, 3)
	if strings.TrimSpace(annotation.Title) != "" {
		parts = append(parts, strings.TrimSpace(annotation.Title))
	}
	if strings.TrimSpace(annotation.URL) != "" {
		parts = append(parts, strings.TrimSpace(annotation.URL))
	}
	if strings.TrimSpace(annotation.Filename) != "" {
		parts = append(parts, strings.TrimSpace(annotation.Filename))
	}
	if strings.TrimSpace(annotation.FileID) != "" {
		parts = append(parts, "file:"+strings.TrimSpace(annotation.FileID))
	}
	if strings.TrimSpace(annotation.Text) != "" && len(parts) == 0 {
		parts = append(parts, strings.TrimSpace(annotation.Text))
	}
	return strings.Join(parts, " - ")
}

func prependSystemMessage(messages []Message, systemPrompt string) []Message {
	combined := make([]Message, 0, len(messages)+1)
	combined = append(combined, Message{Role: "system", Content: systemPrompt})
	combined = append(combined, messages...)
	return combined
}
