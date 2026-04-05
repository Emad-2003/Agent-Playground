package provider

import (
	"strings"
	"testing"
)

func TestDecodeAnthropicResponseToolUse(t *testing.T) {
	payload := `{
		"stop_reason": "tool_use",
		"content": [
			{"type": "thinking", "thinking": "Need to inspect files."},
			{"type": "tool_use", "id": "toolu_1", "name": "grep", "input": {"pattern": "TODO"}}
		],
		"usage": {"input_tokens": 9, "output_tokens": 4}
	}`

	resp, err := decodeAnthropicResponse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FinishReason != "tool_use" {
		t.Fatalf("finish reason = %q, want tool_use", resp.FinishReason)
	}
	if resp.Reasoning != "Need to inspect files." {
		t.Fatalf("reasoning = %q, want %q", resp.Reasoning, "Need to inspect files.")
	}
	if len(resp.Content) != 2 || resp.Content[0].Kind != ContentBlockReasoning || resp.Content[1].Kind != ContentBlockToolCall {
		t.Fatalf("unexpected content blocks: %#v", resp.Content)
	}
	call := resp.Content[1].ToolCall
	if call == nil || call.ID != "toolu_1" || call.Name != "grep" || call.Input != `{"pattern":"TODO"}` || !call.Finished {
		t.Fatalf("unexpected tool call payload: %#v", call)
	}
	if resp.Usage.InputTokens != 9 || resp.Usage.OutputTokens != 4 {
		t.Fatalf("unexpected usage: %#v", resp.Usage)
	}
}

func TestDecodeAnthropicResponseToolResult(t *testing.T) {
	payload := `{
		"stop_reason": "end_turn",
		"content": [
			{"type": "tool_result", "tool_use_id": "toolu_1", "name": "grep", "content": "README.md:1: TODO", "is_error": false}
		],
		"usage": {"input_tokens": 4, "output_tokens": 6}
	}`

	resp, err := decodeAnthropicResponse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Kind != ContentBlockToolResult {
		t.Fatalf("unexpected content blocks: %#v", resp.Content)
	}
	result := resp.Content[0].ToolResult
	if result == nil || result.ToolCallID != "toolu_1" || result.Name != "grep" || result.Content != "README.md:1: TODO" || result.IsError {
		t.Fatalf("unexpected tool result payload: %#v", result)
	}
}
