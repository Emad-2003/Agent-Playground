package app

import (
	"fmt"
	"strings"
	"time"

	"crawler-ai/internal/domain"
	"crawler-ai/internal/provider"
	"crawler-ai/internal/runtime"
)

const (
	toolStatusPending   = "pending"
	toolStatusRunning   = "running"
	toolStatusCompleted = "completed"
	toolStatusFailed    = "failed"
)

func ensureToolCallID(callID string, nextID func(string) string) string {
	if strings.TrimSpace(callID) != "" {
		return callID
	}
	return nextID("toolcall")
}

func toolTranscriptEntryID(callID string) string {
	return "tool-" + strings.TrimSpace(callID)
}

func toolInputSummary(request runtime.ToolRequest) string {
	switch request.Name {
	case "read_file", "write_file", "edit_file", "list_files":
		return request.Path
	case "view":
		if request.StartLine > 0 && request.Limit > 0 {
			return fmt.Sprintf("%s::%d::%d", request.Path, request.StartLine, request.Limit)
		}
		return request.Path
	case "fetch":
		return request.URL
	case "glob", "grep":
		return request.Pattern
	case "shell", "shell_bg":
		return request.Command
	case "job_output", "job_kill":
		return request.JobID
	default:
		return ""
	}
}

func formatToolTranscriptMessage(name, input, output string) string {
	parts := []string{fmt.Sprintf("Tool: %s", name)}
	if trimmed := strings.TrimSpace(input); trimmed != "" {
		parts = append(parts, "Input:\n"+trimmed)
	}
	if trimmed := strings.TrimSpace(output); trimmed != "" {
		parts = append(parts, "Result:\n"+trimmed)
	}
	return strings.Join(parts, "\n\n")
}

func newRuntimeToolTranscriptEntry(request runtime.ToolRequest, now time.Time) domain.TranscriptEntry {
	callID := ensureToolCallID(request.CallID, func(string) string { return request.CallID })
	return domain.TranscriptEntry{
		ID:        toolTranscriptEntryID(callID),
		Kind:      domain.TranscriptTool,
		Message:   formatToolTranscriptMessage(request.Name, toolInputSummary(request), ""),
		CreatedAt: now,
		UpdatedAt: now,
		Metadata: map[string]string{
			"tool":         request.Name,
			"tool_call_id": callID,
			"status":       toolStatusRunning,
		},
	}
}

func newProviderToolTranscriptEntry(call provider.ToolCall, now time.Time, metadata map[string]string) domain.TranscriptEntry {
	entry := domain.TranscriptEntry{
		ID:        toolTranscriptEntryID(call.ID),
		Kind:      domain.TranscriptTool,
		Message:   formatToolTranscriptMessage(call.Name, call.Input, ""),
		CreatedAt: now,
		UpdatedAt: now,
		Metadata:  cloneMetadata(metadata),
	}
	if entry.Metadata == nil {
		entry.Metadata = make(map[string]string)
	}
	entry.Metadata["tool"] = call.Name
	entry.Metadata["tool_call_id"] = call.ID
	if call.Finished {
		entry.Metadata["status"] = toolStatusCompleted
	} else {
		entry.Metadata["status"] = toolStatusPending
	}
	return entry
}

func applyToolTranscriptResult(entry domain.TranscriptEntry, output string, status string, now time.Time) domain.TranscriptEntry {
	toolName := entry.Metadata["tool"]
	input := ""
	parts := strings.Split(entry.Message, "\n\n")
	for _, part := range parts {
		if strings.HasPrefix(part, "Input:\n") {
			input = strings.TrimPrefix(part, "Input:\n")
			break
		}
	}
	entry.Message = formatToolTranscriptMessage(toolName, input, output)
	entry.UpdatedAt = now
	if entry.Metadata == nil {
		entry.Metadata = make(map[string]string)
	}
	entry.Metadata["status"] = status
	return entry
}
