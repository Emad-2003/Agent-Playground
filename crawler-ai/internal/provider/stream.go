package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	apperrors "crawler-ai/internal/errors"
)

// StreamChunk represents a partial response from a streaming completion.
type StreamChunk struct {
	Text         string // incremental text delta
	Reasoning    string // incremental reasoning delta when available
	Content      []ContentBlock
	Done         bool   // true when stream is finished
	Usage        Usage  // populated only on the final chunk
	FinishReason string // populated when the provider exposes a finish reason
}

func (c StreamChunk) ContentBlocks() []ContentBlock {
	return normalizeContentBlocks(c.Content, c.Text, c.Reasoning)
}

// StreamingProvider extends Provider with streaming support.
type StreamingProvider interface {
	Provider
	CompleteStream(ctx context.Context, request Request, onChunk func(StreamChunk)) (Response, error)
}

// SupportsStreaming returns true if the provider implements StreamingProvider.
func SupportsStreaming(p Provider) (StreamingProvider, bool) {
	sp, ok := p.(StreamingProvider)
	return sp, ok
}

// --- OpenAI streaming ---

func (p *OpenAICompatible) CompleteStream(ctx context.Context, request Request, onChunk func(StreamChunk)) (Response, error) {
	body := mergeRequestBody(p.config, request, map[string]any{
		"model":       request.Model,
		"messages":    serializeOpenAIMessages(request.Messages),
		"max_tokens":  request.MaxTokens,
		"temperature": request.Temperature,
		"stream":      true,
		"stream_options": map[string]any{
			"include_usage": true,
		},
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
		return Response{}, apperrors.Wrap("provider.OpenAI.CompleteStream", apperrors.CodeProviderFailed, err, "marshal request")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.config.BaseURL, "/")+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Response{}, apperrors.Wrap("provider.OpenAI.CompleteStream", apperrors.CodeProviderFailed, err, "create request")
	}
	for key, value := range mergeRequestHeaders(p.config, request, map[string]string{
		"Authorization": "Bearer " + p.config.APIKey,
		"Content-Type":  "application/json",
		"Accept":        "text/event-stream",
	}) {
		httpReq.Header.Set(key, value)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, apperrors.Wrap("provider.OpenAI.CompleteStream", apperrors.CodeProviderFailed, err, "send request")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return Response{}, newStatusErrorFromResponse("provider.OpenAI.CompleteStream", resp, "provider returned non-success status")
	}

	return parseOpenAISSE(resp.Body, onChunk)
}

func parseOpenAISSE(r io.Reader, onChunk func(StreamChunk)) (Response, error) {
	var fullText strings.Builder
	var usage Usage
	finishReason := "stop"
	toolCalls := make(map[int]*ToolCall)
	toolCallOrder := make([]int, 0)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var event struct {
			Choices []struct {
				FinishReason string `json:"finish_reason"`
				Delta        struct {
					Content     json.RawMessage    `json:"content"`
					Refusal     string             `json:"refusal"`
					Annotations []openAIAnnotation `json:"annotations"`
					ToolCalls   []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage,omitempty"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue // skip malformed events
		}

		if len(event.Choices) > 0 {
			choice := event.Choices[0]
			textChunk := openAIStreamTextChunk(choice.Delta.Content, choice.Delta.Refusal, choice.Delta.Annotations, fullText.Len() > 0)
			if textChunk != "" {
				fullText.WriteString(textChunk)
				onChunk(StreamChunk{Text: textChunk, Content: []ContentBlock{TextBlock(textChunk)}})
			}
			for _, deltaToolCall := range choice.Delta.ToolCalls {
				call, ok := toolCalls[deltaToolCall.Index]
				if !ok {
					call = &ToolCall{}
					toolCalls[deltaToolCall.Index] = call
					toolCallOrder = append(toolCallOrder, deltaToolCall.Index)
				}
				if strings.TrimSpace(deltaToolCall.ID) != "" {
					call.ID = strings.TrimSpace(deltaToolCall.ID)
				}
				if deltaToolCall.Function.Name != "" {
					call.Name += deltaToolCall.Function.Name
				}
				if deltaToolCall.Function.Arguments != "" {
					call.Input += deltaToolCall.Function.Arguments
				}
				if strings.TrimSpace(call.ID) != "" {
					onChunk(StreamChunk{Content: []ContentBlock{ToolCallBlock(ToolCall{ID: call.ID, Name: call.Name, Input: call.Input, Finished: false})}})
				}
			}
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
				if finishReason == "tool_calls" {
					for _, idx := range toolCallOrder {
						call := toolCalls[idx]
						if call == nil || strings.TrimSpace(call.ID) == "" {
							continue
						}
						onChunk(StreamChunk{Content: []ContentBlock{ToolCallBlock(ToolCall{ID: call.ID, Name: call.Name, Input: call.Input, Finished: true})}})
					}
				}
			}
		}

		if event.Usage != nil {
			usage = Usage{
				InputTokens:  event.Usage.PromptTokens,
				OutputTokens: event.Usage.CompletionTokens,
			}
		}
	}

	finalContent := make([]ContentBlock, 0, len(toolCallOrder)+1)
	if strings.TrimSpace(fullText.String()) != "" {
		finalContent = append(finalContent, TextBlock(fullText.String()))
	}
	for _, idx := range toolCallOrder {
		call := toolCalls[idx]
		if call == nil || strings.TrimSpace(call.ID) == "" {
			continue
		}
		finalContent = append(finalContent, ToolCallBlock(ToolCall{ID: call.ID, Name: call.Name, Input: call.Input, Finished: true}))
	}
	final := Response{Text: fullText.String(), Content: normalizeContentBlocks(finalContent, fullText.String(), ""), Usage: usage, FinishReason: finishReason}
	onChunk(StreamChunk{Done: true, Usage: usage, FinishReason: finishReason})
	return final, scanner.Err()
}

// --- Anthropic streaming ---

func (p *Anthropic) CompleteStream(ctx context.Context, request Request, onChunk func(StreamChunk)) (Response, error) {
	body := mergeRequestBody(p.config, request, map[string]any{
		"model":       request.Model,
		"messages":    serializeAnthropicMessages(request.Messages),
		"system":      request.SystemPrompt,
		"max_tokens":  request.MaxTokens,
		"temperature": request.Temperature,
		"stream":      true,
	}, false)
	if len(request.Tools) > 0 {
		body["tools"] = serializeAnthropicTools(request.Tools)
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, apperrors.Wrap("provider.Anthropic.CompleteStream", apperrors.CodeProviderFailed, err, "marshal request")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.config.BaseURL, "/")+"/messages", bytes.NewReader(payload))
	if err != nil {
		return Response{}, apperrors.Wrap("provider.Anthropic.CompleteStream", apperrors.CodeProviderFailed, err, "create request")
	}
	for key, value := range mergeRequestHeaders(p.config, request, map[string]string{
		"x-api-key":         p.config.APIKey,
		"anthropic-version": "2023-06-01",
		"Content-Type":      "application/json",
		"Accept":            "text/event-stream",
	}) {
		httpReq.Header.Set(key, value)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return Response{}, apperrors.Wrap("provider.Anthropic.CompleteStream", apperrors.CodeProviderFailed, err, "send request")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return Response{}, newStatusErrorFromResponse("provider.Anthropic.CompleteStream", resp, "provider returned non-success status")
	}

	return parseAnthropicSSE(resp.Body, onChunk)
}

func parseAnthropicSSE(r io.Reader, onChunk func(StreamChunk)) (Response, error) {
	var usage Usage
	finishReason := ""
	streamBlocks := make(map[int]*ContentBlock)
	blockOrder := make([]int, 0)
	toolCalls := make(map[int]*ToolCall)

	scanner := bufio.NewScanner(r)
	var eventType string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		switch eventType {
		case "content_block_start":
			var start struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type      string          `json:"type"`
					Text      string          `json:"text"`
					Thinking  string          `json:"thinking"`
					ID        string          `json:"id"`
					Name      string          `json:"name"`
					Input     json.RawMessage `json:"input"`
					ToolUseID string          `json:"tool_use_id"`
					Content   json.RawMessage `json:"content"`
					IsError   bool            `json:"is_error"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(data), &start); err == nil {
				if _, ok := streamBlocks[start.Index]; !ok {
					blockOrder = append(blockOrder, start.Index)
				}
				switch start.ContentBlock.Type {
				case "text":
					block := TextBlock(start.ContentBlock.Text)
					streamBlocks[start.Index] = &block
				case "thinking":
					block := ReasoningBlock(start.ContentBlock.Thinking)
					streamBlocks[start.Index] = &block
				case "tool_use":
					call := &ToolCall{ID: strings.TrimSpace(start.ContentBlock.ID), Name: strings.TrimSpace(start.ContentBlock.Name), Input: normalizeStructuredPayload(start.ContentBlock.Input), Finished: false}
					toolCalls[start.Index] = call
					if block, blockErr := anthropicDeltaToolCall(start.Index, call); blockErr == nil {
						streamBlocks[start.Index] = &block
						onChunk(StreamChunk{Content: []ContentBlock{block}})
					}
				default:
					if result, ok := anthropicToolResultBlock(anthropicContentBlock{
						Type:      start.ContentBlock.Type,
						ID:        start.ContentBlock.ID,
						Name:      start.ContentBlock.Name,
						ToolUseID: start.ContentBlock.ToolUseID,
						Content:   start.ContentBlock.Content,
						IsError:   start.ContentBlock.IsError,
					}); ok {
						streamBlocks[start.Index] = &result
						onChunk(StreamChunk{Content: []ContentBlock{result}})
					}
				}
			}
		case "content_block_delta":
			var delta struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					Thinking    string `json:"thinking"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &delta); err == nil {
				switch delta.Delta.Type {
				case "text_delta":
					if delta.Delta.Text != "" {
						block, ok := streamBlocks[delta.Index]
						if !ok || block == nil {
							newBlock := TextBlock("")
							streamBlocks[delta.Index] = &newBlock
							block = &newBlock
							blockOrder = append(blockOrder, delta.Index)
						}
						if block.Kind == ContentBlockToolResult && block.ToolResult != nil {
							block.ToolResult.Content += delta.Delta.Text
							onChunk(StreamChunk{Content: []ContentBlock{ToolResultBlock(*block.ToolResult)}})
						} else {
							block.Text += delta.Delta.Text
							onChunk(StreamChunk{Text: delta.Delta.Text, Content: []ContentBlock{TextBlock(delta.Delta.Text)}})
						}
					}
				case "thinking_delta":
					if delta.Delta.Thinking != "" {
						block, ok := streamBlocks[delta.Index]
						if !ok || block == nil {
							newBlock := ReasoningBlock("")
							streamBlocks[delta.Index] = &newBlock
							block = &newBlock
							blockOrder = append(blockOrder, delta.Index)
						}
						block.Text += delta.Delta.Thinking
						onChunk(StreamChunk{Reasoning: delta.Delta.Thinking, Content: []ContentBlock{ReasoningBlock(delta.Delta.Thinking)}})
					}
				case "input_json_delta":
					call, ok := toolCalls[delta.Index]
					if !ok || call == nil {
						call = &ToolCall{}
						toolCalls[delta.Index] = call
						if _, exists := streamBlocks[delta.Index]; !exists {
							blockOrder = append(blockOrder, delta.Index)
						}
					}
					call.Input = appendJSONStringFragment(call.Input, delta.Delta.PartialJSON)
					if block, blockErr := anthropicDeltaToolCall(delta.Index, call); blockErr == nil {
						streamBlocks[delta.Index] = &block
						onChunk(StreamChunk{Content: []ContentBlock{block}})
					}
				}
			}
		case "content_block_stop":
			var stop struct {
				Index int `json:"index"`
			}
			if err := json.Unmarshal([]byte(data), &stop); err == nil {
				if call, ok := toolCalls[stop.Index]; ok && call != nil {
					call.Finished = true
					if block, blockErr := anthropicDeltaToolCall(stop.Index, call); blockErr == nil {
						streamBlocks[stop.Index] = &block
						onChunk(StreamChunk{Content: []ContentBlock{block}})
					}
				}
			}
		case "message_delta":
			var md struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &md); err == nil {
				usage.OutputTokens = md.Usage.OutputTokens
				if md.Delta.StopReason != "" {
					finishReason = md.Delta.StopReason
				}
			}
		case "message_start":
			var ms struct {
				Message struct {
					Usage struct {
						InputTokens int `json:"input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(data), &ms); err == nil {
				usage.InputTokens = ms.Message.Usage.InputTokens
			}
		}
		eventType = ""
	}

	if finishReason == "" {
		finishReason = "end_turn"
	}
	content := make([]ContentBlock, 0, len(blockOrder))
	textParts := make([]string, 0, len(blockOrder))
	reasoningParts := make([]string, 0, len(blockOrder))
	for _, index := range blockOrder {
		block := streamBlocks[index]
		if block == nil {
			continue
		}
		content = append(content, *block)
		switch block.Kind {
		case ContentBlockText:
			textParts = append(textParts, block.Text)
		case ContentBlockReasoning:
			reasoningParts = append(reasoningParts, block.Text)
		}
	}
	final := Response{
		Text:         strings.Join(textParts, ""),
		Reasoning:    strings.Join(reasoningParts, ""),
		Content:      normalizeContentBlocks(content, strings.Join(textParts, ""), strings.Join(reasoningParts, "")),
		Usage:        usage,
		FinishReason: finishReason,
	}
	onChunk(StreamChunk{Done: true, Usage: usage, FinishReason: finishReason})
	return final, scanner.Err()
}
