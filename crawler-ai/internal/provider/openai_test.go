package provider

import (
	"strings"
	"testing"
)

func TestDecodeOpenAIResponseToolCalls(t *testing.T) {
	payload := `{
		"choices": [
			{
				"finish_reason": "tool_calls",
				"message": {
					"content": "",
					"tool_calls": [
						{
							"id": "call_1",
							"type": "function",
							"function": {
								"name": "grep",
								"arguments": "{\"pattern\":\"TODO\"}"
							}
						}
					]
				}
			}
		],
		"usage": {
			"prompt_tokens": 3,
			"completion_tokens": 5
		}
	}`

	resp, err := decodeOpenAIResponse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q, want tool_calls", resp.FinishReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Kind != ContentBlockToolCall {
		t.Fatalf("unexpected content blocks: %#v", resp.Content)
	}
	call := resp.Content[0].ToolCall
	if call == nil {
		t.Fatal("expected tool call block")
	}
	if call.ID != "call_1" || call.Name != "grep" || call.Input != `{"pattern":"TODO"}` || !call.Finished {
		t.Fatalf("unexpected tool call payload: %#v", call)
	}
	if resp.Usage.InputTokens != 3 || resp.Usage.OutputTokens != 5 {
		t.Fatalf("unexpected usage: %#v", resp.Usage)
	}
}

func TestDecodeOpenAIResponseToolResultContent(t *testing.T) {
	payload := `{
		"choices": [
			{
				"finish_reason": "stop",
				"message": {
					"content": [
						{"type": "text", "text": "Done."},
						{"type": "function_call_output", "call_id": "call_1", "name": "grep", "output": "README.md:1: TODO"}
					]
				}
			}
		],
		"usage": {
			"prompt_tokens": 6,
			"completion_tokens": 4
		}
	}`

	resp, err := decodeOpenAIResponse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "Done." {
		t.Fatalf("unexpected response text: %q", resp.Text)
	}
	if len(resp.Content) != 2 || resp.Content[0].Kind != ContentBlockText || resp.Content[1].Kind != ContentBlockToolResult {
		t.Fatalf("unexpected content blocks: %#v", resp.Content)
	}
	result := resp.Content[1].ToolResult
	if result == nil || result.ToolCallID != "call_1" || result.Name != "grep" || result.Content != "README.md:1: TODO" || result.IsError {
		t.Fatalf("unexpected tool result payload: %#v", result)
	}
}

func TestDecodeOpenAIResponseTextAnnotations(t *testing.T) {
	payload := `{
		"choices": [
			{
				"finish_reason": "stop",
				"message": {
					"content": [
						{
							"type": "output_text",
							"text": "Answer body.",
							"annotations": [
								{"type": "url_citation", "title": "Docs", "url": "https://example.com/docs"}
							]
						}
					]
				}
			}
		],
		"usage": {"prompt_tokens": 2, "completion_tokens": 3}
	}`

	resp, err := decodeOpenAIResponse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Kind != ContentBlockText {
		t.Fatalf("unexpected content blocks: %#v", resp.Content)
	}
	if !strings.Contains(resp.Content[0].Text, "Answer body.") || !strings.Contains(resp.Content[0].Text, "[1] Docs - https://example.com/docs") {
		t.Fatalf("expected annotation text to be preserved, got %q", resp.Content[0].Text)
	}
}

func TestDecodeOpenAIResponseRefusalPart(t *testing.T) {
	payload := `{
		"choices": [
			{
				"finish_reason": "stop",
				"message": {
					"content": [
						{"type": "refusal", "refusal": "I can not comply."}
					]
				}
			}
		],
		"usage": {"prompt_tokens": 1, "completion_tokens": 1}
	}`

	resp, err := decodeOpenAIResponse(strings.NewReader(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Kind != ContentBlockText {
		t.Fatalf("unexpected content blocks: %#v", resp.Content)
	}
	if resp.Content[0].Text != "Refusal:\nI can not comply." {
		t.Fatalf("unexpected refusal text: %q", resp.Content[0].Text)
	}
}
