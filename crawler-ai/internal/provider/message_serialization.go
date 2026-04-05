package provider

import (
	"encoding/json"
	"strings"
)

func serializeOpenAIMessages(messages []Message) []map[string]any {
	serialized := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		if role == "" {
			role = "user"
		}
		if role == "tool" {
			serialized = append(serialized, map[string]any{
				"role":         "tool",
				"tool_call_id": strings.TrimSpace(message.ToolCallID),
				"content":      openAIMessageText(message),
			})
			continue
		}
		item := map[string]any{"role": role}
		text := openAIMessageText(message)
		if role == "assistant" {
			toolCalls := openAIMessageToolCalls(message)
			if len(toolCalls) > 0 {
				if text == "" {
					item["content"] = ""
				} else {
					item["content"] = text
				}
				item["tool_calls"] = toolCalls
				serialized = append(serialized, item)
				continue
			}
		}
		if text == "" {
			item["content"] = nil
		} else {
			item["content"] = text
		}
		serialized = append(serialized, item)
	}
	return serialized
}

func serializeAnthropicMessages(messages []Message) []map[string]any {
	serialized := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		role := strings.TrimSpace(message.Role)
		if role == "" {
			role = "user"
		}
		if role == "tool" {
			content := anthropicToolResultContent(message)
			if len(content) == 0 {
				continue
			}
			serialized = append(serialized, map[string]any{
				"role":    "user",
				"content": content,
			})
			continue
		}
		if len(message.ContentBlocks) == 0 {
			text := strings.TrimSpace(message.Content)
			if text == "" {
				continue
			}
			serialized = append(serialized, map[string]any{"role": role, "content": text})
			continue
		}
		content := anthropicMessageContent(message)
		if len(content) == 0 {
			text := strings.TrimSpace(message.Content)
			if text == "" {
				continue
			}
			serialized = append(serialized, map[string]any{"role": role, "content": text})
			continue
		}
		serialized = append(serialized, map[string]any{"role": role, "content": content})
	}
	return serialized
}

func serializeOpenAITools(tools []ToolDefinition) []map[string]any {
	serialized := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" {
			continue
		}
		serialized = append(serialized, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  cloneAnyMap(tool.Parameters),
			},
		})
	}
	return serialized
}

func serializeAnthropicTools(tools []ToolDefinition) []map[string]any {
	serialized := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" {
			continue
		}
		serialized = append(serialized, map[string]any{
			"name":         tool.Name,
			"description":  tool.Description,
			"input_schema": cloneAnyMap(tool.Parameters),
		})
	}
	return serialized
}

func openAIMessageText(message Message) string {
	if strings.TrimSpace(message.Content) != "" {
		return message.Content
	}
	parts := make([]string, 0, len(message.ContentBlocks))
	for _, block := range message.ContentBlocks {
		switch block.Kind {
		case ContentBlockText, ContentBlockReasoning:
			if strings.TrimSpace(block.Text) != "" {
				parts = append(parts, block.Text)
			}
		case ContentBlockToolResult:
			if block.ToolResult != nil && strings.TrimSpace(block.ToolResult.Content) != "" {
				parts = append(parts, block.ToolResult.Content)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func openAIMessageToolCalls(message Message) []map[string]any {
	toolCalls := make([]map[string]any, 0)
	for _, block := range message.ContentBlocks {
		if block.Kind != ContentBlockToolCall || block.ToolCall == nil || strings.TrimSpace(block.ToolCall.ID) == "" {
			continue
		}
		toolCalls = append(toolCalls, map[string]any{
			"id":   block.ToolCall.ID,
			"type": "function",
			"function": map[string]any{
				"name":      block.ToolCall.Name,
				"arguments": block.ToolCall.Input,
			},
		})
	}
	return toolCalls
}

func anthropicMessageContent(message Message) []map[string]any {
	content := make([]map[string]any, 0, len(message.ContentBlocks)+1)
	if strings.TrimSpace(message.Content) != "" {
		content = append(content, map[string]any{"type": "text", "text": message.Content})
	}
	for _, block := range message.ContentBlocks {
		switch block.Kind {
		case ContentBlockText:
			if strings.TrimSpace(block.Text) != "" {
				content = append(content, map[string]any{"type": "text", "text": block.Text})
			}
		case ContentBlockToolCall:
			if block.ToolCall != nil && strings.TrimSpace(block.ToolCall.ID) != "" {
				content = append(content, map[string]any{"type": "tool_use", "id": block.ToolCall.ID, "name": block.ToolCall.Name, "input": cloneAnyValue(parseStructuredInput(block.ToolCall.Input))})
			}
		}
	}
	return content
}

func anthropicToolResultContent(message Message) []map[string]any {
	for _, block := range message.ContentBlocks {
		if block.Kind != ContentBlockToolResult || block.ToolResult == nil || strings.TrimSpace(block.ToolResult.ToolCallID) == "" {
			continue
		}
		return []map[string]any{{
			"type":        "tool_result",
			"tool_use_id": block.ToolResult.ToolCallID,
			"content":     block.ToolResult.Content,
			"is_error":    block.ToolResult.IsError,
		}}
	}
	if strings.TrimSpace(message.ToolCallID) == "" {
		return nil
	}
	return []map[string]any{{
		"type":        "tool_result",
		"tool_use_id": strings.TrimSpace(message.ToolCallID),
		"content":     message.Content,
	}}
}

func parseStructuredInput(raw string) any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		return decoded
	}
	return trimmed
}
