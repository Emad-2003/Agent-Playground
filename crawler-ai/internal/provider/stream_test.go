package provider

import (
	"strings"
	"testing"
)

func TestParseOpenAISSE(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hello"}}]}`,
		``,
		`data: {"choices":[{"delta":{"content":" world"}}]}`,
		``,
		`data: {"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var chunks []StreamChunk
	resp, err := parseOpenAISSE(strings.NewReader(sse), func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "Hello world" {
		t.Errorf("response text = %q, want %q", resp.Text, "Hello world")
	}
	if resp.Usage.InputTokens != 5 {
		t.Errorf("input tokens = %d, want 5", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 2 {
		t.Errorf("output tokens = %d, want 2", resp.Usage.OutputTokens)
	}
	// Expect 3 chunks: "Hello", " world", done
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if chunks[0].Text != "Hello" {
		t.Errorf("chunk 0 text = %q, want %q", chunks[0].Text, "Hello")
	}
	if chunks[1].Text != " world" {
		t.Errorf("chunk 1 text = %q, want %q", chunks[1].Text, " world")
	}
	if !chunks[2].Done {
		t.Error("expected last chunk to be done")
	}
	if len(resp.Content) != 1 || resp.Content[0].Kind != ContentBlockText {
		t.Fatalf("expected text content block, got %#v", resp.Content)
	}
}

func TestParseOpenAISSEToolCalls(t *testing.T) {
	sse := strings.Join([]string{
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"grep\",\"arguments\":\"{\\\"pattern\\\":\"}}]}}]}",
		``,
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"TODO\\\"}\"}}]}}]}",
		``,
		`data: {"choices":[{"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":12,"completion_tokens":4}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var chunks []StreamChunk
	resp, err := parseOpenAISSE(strings.NewReader(sse), func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q, want tool_calls", resp.FinishReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Kind != ContentBlockToolCall {
		t.Fatalf("expected tool call content block, got %#v", resp.Content)
	}
	call := resp.Content[0].ToolCall
	if call == nil {
		t.Fatal("expected tool call payload")
	}
	if call.ID != "call_1" || call.Name != "grep" || call.Input != `{"pattern":"TODO"}` || !call.Finished {
		t.Fatalf("unexpected tool call: %#v", call)
	}
	if len(chunks) != 4 {
		t.Fatalf("got %d chunks, want 4", len(chunks))
	}
	if len(chunks[0].Content) != 1 || chunks[0].Content[0].Kind != ContentBlockToolCall {
		t.Fatalf("expected first chunk to carry tool call delta, got %#v", chunks[0].Content)
	}
	if len(chunks[2].Content) != 1 || !chunks[2].Content[0].ToolCall.Finished {
		t.Fatalf("expected finish chunk to carry completed tool call, got %#v", chunks[2].Content)
	}
	if !chunks[3].Done {
		t.Fatal("expected final done chunk")
	}
	if resp.Usage.InputTokens != 12 || resp.Usage.OutputTokens != 4 {
		t.Fatalf("unexpected usage: %#v", resp.Usage)
	}
}

func TestParseOpenAISSEAnnotations(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Answer body."}}]}`,
		``,
		`data: {"choices":[{"delta":{"annotations":[{"type":"url_citation","title":"Docs","url":"https://example.com/docs"}]}}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var chunks []StreamChunk
	resp, err := parseOpenAISSE(strings.NewReader(sse), func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Text, "Answer body.") || !strings.Contains(resp.Text, "[1] Docs - https://example.com/docs") {
		t.Fatalf("expected streamed annotations in text, got %q", resp.Text)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if !strings.Contains(chunks[1].Text, "[1] Docs - https://example.com/docs") {
		t.Fatalf("expected annotation chunk, got %#v", chunks[1])
	}
	if !chunks[2].Done {
		t.Fatal("expected done chunk")
	}
}

func TestParseOpenAISSERefusal(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"refusal":"I can not comply."}}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	var chunks []StreamChunk
	resp, err := parseOpenAISSE(strings.NewReader(sse), func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "Refusal:\nI can not comply." {
		t.Fatalf("unexpected refusal text: %q", resp.Text)
	}
	if len(chunks) != 2 || chunks[0].Text != "Refusal:\nI can not comply." || !chunks[1].Done {
		t.Fatalf("unexpected refusal chunks: %#v", chunks)
	}
}

func TestParseAnthropicSSE(t *testing.T) {
	sse := strings.Join([]string{
		`event: message_start`,
		`data: {"message":{"usage":{"input_tokens":10}}}`,
		``,
		`event: content_block_delta`,
		`data: {"index":0,"delta":{"type":"text_delta","text":"Hi"}}`,
		``,
		`event: content_block_delta`,
		`data: {"index":0,"delta":{"type":"text_delta","text":" there"}}`,
		``,
		`event: message_delta`,
		`data: {"usage":{"output_tokens":3}}`,
		``,
	}, "\n")

	var chunks []StreamChunk
	resp, err := parseAnthropicSSE(strings.NewReader(sse), func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "Hi there" {
		t.Errorf("response text = %q, want %q", resp.Text, "Hi there")
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("input tokens = %d, want 10", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 3 {
		t.Errorf("output tokens = %d, want 3", resp.Usage.OutputTokens)
	}
	// 2 text chunks + 1 done
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if chunks[0].Text != "Hi" {
		t.Errorf("chunk 0 text = %q, want %q", chunks[0].Text, "Hi")
	}
	if !chunks[2].Done {
		t.Error("expected last chunk to be done")
	}
	if len(resp.Content) != 1 || resp.Content[0].Kind != ContentBlockText {
		t.Fatalf("expected text content block, got %#v", resp.Content)
	}
}

func TestParseAnthropicSSEThinking(t *testing.T) {
	sse := strings.Join([]string{
		`event: content_block_delta`,
		`data: {"index":0,"delta":{"type":"thinking_delta","thinking":"Need more context."}}`,
		``,
		`event: content_block_delta`,
		`data: {"index":1,"delta":{"type":"text_delta","text":"Done."}}`,
		``,
		`event: message_delta`,
		`data: {"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
		``,
	}, "\n")

	var chunks []StreamChunk
	resp, err := parseAnthropicSSE(strings.NewReader(sse), func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Reasoning != "Need more context." {
		t.Fatalf("response reasoning = %q, want %q", resp.Reasoning, "Need more context.")
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if chunks[0].Reasoning != "Need more context." {
		t.Fatalf("reasoning chunk = %q, want %q", chunks[0].Reasoning, "Need more context.")
	}
	if len(resp.Content) != 2 || resp.Content[0].Kind != ContentBlockReasoning || resp.Content[1].Kind != ContentBlockText {
		t.Fatalf("unexpected response content: %#v", resp.Content)
	}
}

func TestParseAnthropicSSEToolUse(t *testing.T) {
	sse := strings.Join([]string{
		`event: message_start`,
		`data: {"message":{"usage":{"input_tokens":7}}}`,
		``,
		`event: content_block_start`,
		`data: {"index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"grep","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"{\"pattern\":\"TODO\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"index":0}`,
		``,
		`event: message_delta`,
		`data: {"delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`,
		``,
	}, "\n")

	var chunks []StreamChunk
	resp, err := parseAnthropicSSE(strings.NewReader(sse), func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FinishReason != "tool_use" {
		t.Fatalf("finish reason = %q, want tool_use", resp.FinishReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Kind != ContentBlockToolCall {
		t.Fatalf("unexpected response content: %#v", resp.Content)
	}
	call := resp.Content[0].ToolCall
	if call == nil || call.ID != "toolu_1" || call.Name != "grep" || call.Input != `{"pattern":"TODO"}` || !call.Finished {
		t.Fatalf("unexpected tool call payload: %#v", call)
	}
	if len(chunks) != 4 {
		t.Fatalf("got %d chunks, want 4", len(chunks))
	}
	if len(chunks[0].Content) != 1 || chunks[0].Content[0].Kind != ContentBlockToolCall {
		t.Fatalf("expected tool-use start chunk, got %#v", chunks[0].Content)
	}
	if len(chunks[2].Content) != 1 || !chunks[2].Content[0].ToolCall.Finished {
		t.Fatalf("expected completed tool-use chunk, got %#v", chunks[2].Content)
	}
	if !chunks[3].Done {
		t.Fatal("expected final done chunk")
	}
}

func TestParseAnthropicSSEToolResult(t *testing.T) {
	sse := strings.Join([]string{
		`event: message_start`,
		`data: {"message":{"usage":{"input_tokens":5}}}`,
		``,
		`event: content_block_start`,
		`data: {"index":0,"content_block":{"type":"tool_result","tool_use_id":"toolu_1","name":"grep","content":"README.md:1: TO"}}`,
		``,
		`event: content_block_delta`,
		`data: {"index":0,"delta":{"type":"text_delta","text":"DO"}}`,
		``,
		`event: message_delta`,
		`data: {"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`,
		``,
	}, "\n")

	var chunks []StreamChunk
	resp, err := parseAnthropicSSE(strings.NewReader(sse), func(chunk StreamChunk) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Kind != ContentBlockToolResult {
		t.Fatalf("unexpected response content: %#v", resp.Content)
	}
	result := resp.Content[0].ToolResult
	if result == nil || result.ToolCallID != "toolu_1" || result.Name != "grep" || result.Content != "README.md:1: TODO" || result.IsError {
		t.Fatalf("unexpected tool result payload: %#v", result)
	}
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	if len(chunks[0].Content) != 1 || chunks[0].Content[0].Kind != ContentBlockToolResult {
		t.Fatalf("expected tool result start chunk, got %#v", chunks[0].Content)
	}
	if !chunks[2].Done {
		t.Fatal("expected final done chunk")
	}
}
