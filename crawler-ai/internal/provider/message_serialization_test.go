package provider

import "testing"

func TestSerializeOpenAIMessagesIncludesToolCallsAndToolResults(t *testing.T) {
	messages := []Message{
		{Role: "assistant", ContentBlocks: []ContentBlock{ToolCallBlock(ToolCall{ID: "call-1", Name: "write_file", Input: `{"path":"index.html","content":"<html></html>"}`, Finished: true})}},
		{Role: "tool", ToolCallID: "call-1", ContentBlocks: []ContentBlock{ToolResultBlock(ToolResult{ToolCallID: "call-1", Name: "write_file", Content: "index.html\nwrote 13 bytes"})}},
	}
	serialized := serializeOpenAIMessages(messages)
	if len(serialized) != 2 {
		t.Fatalf("expected 2 serialized messages, got %#v", serialized)
	}
	if serialized[0]["tool_calls"] == nil {
		t.Fatalf("expected assistant tool_calls payload, got %#v", serialized[0])
	}
	if serialized[0]["content"] != "" {
		t.Fatalf("expected empty string content for assistant tool calls, got %#v", serialized[0])
	}
	if serialized[1]["role"] != "tool" || serialized[1]["tool_call_id"] != "call-1" {
		t.Fatalf("unexpected tool result serialization: %#v", serialized[1])
	}
	if serialized[1]["content"] != "index.html\nwrote 13 bytes" {
		t.Fatalf("unexpected tool content: %#v", serialized[1])
	}
}

func TestSerializeAnthropicMessagesMapsToolResultsToUserContent(t *testing.T) {
	messages := []Message{
		{Role: "assistant", ContentBlocks: []ContentBlock{ToolCallBlock(ToolCall{ID: "toolu_1", Name: "grep", Input: `{"pattern":"tic"}`, Finished: true})}},
		{Role: "tool", ToolCallID: "toolu_1", ContentBlocks: []ContentBlock{ToolResultBlock(ToolResult{ToolCallID: "toolu_1", Name: "grep", Content: "README.md:1: tic", IsError: false})}},
	}
	serialized := serializeAnthropicMessages(messages)
	if len(serialized) != 2 {
		t.Fatalf("expected 2 serialized messages, got %#v", serialized)
	}
	if serialized[1]["role"] != "user" {
		t.Fatalf("expected tool result to serialize as user content, got %#v", serialized[1])
	}
	content, ok := serialized[1]["content"].([]map[string]any)
	if !ok || len(content) != 1 || content[0]["tool_use_id"] != "toolu_1" {
		t.Fatalf("unexpected anthropic tool-result content: %#v", serialized[1])
	}
}
