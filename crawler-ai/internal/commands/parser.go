package commands

import (
	"strconv"
	"strings"

	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/runtime"
)

type Kind string

const (
	KindNatural Kind = "natural"
	KindTool    Kind = "tool"
	KindPlan    Kind = "plan"
	KindRun     Kind = "run"
)

type Parsed struct {
	Kind Kind
	Text string
	Tool runtime.ToolRequest
}

func ParsePrompt(prompt string) (Parsed, error) {
	trimmed := strings.TrimSpace(prompt)
	switch {
	case strings.HasPrefix(trimmed, "/plan "):
		return Parsed{Kind: KindPlan, Text: strings.TrimSpace(strings.TrimPrefix(trimmed, "/plan "))}, nil
	case strings.HasPrefix(trimmed, "/run "):
		return Parsed{Kind: KindRun, Text: strings.TrimSpace(strings.TrimPrefix(trimmed, "/run "))}, nil
	case strings.HasPrefix(trimmed, "/read "):
		return Parsed{Kind: KindTool, Tool: runtime.ToolRequest{Name: "read_file", Path: strings.TrimSpace(strings.TrimPrefix(trimmed, "/read "))}}, nil
	case strings.HasPrefix(trimmed, "/view "):
		return parseViewCommand(strings.TrimSpace(strings.TrimPrefix(trimmed, "/view ")))
	case strings.HasPrefix(trimmed, "/list"):
		return Parsed{Kind: KindTool, Tool: runtime.ToolRequest{Name: "list_files", Path: strings.TrimSpace(strings.TrimPrefix(trimmed, "/list"))}}, nil
	case strings.HasPrefix(trimmed, "/glob "):
		return Parsed{Kind: KindTool, Tool: runtime.ToolRequest{Name: "glob", Pattern: strings.TrimSpace(strings.TrimPrefix(trimmed, "/glob "))}}, nil
	case strings.HasPrefix(trimmed, "/grep "):
		return Parsed{Kind: KindTool, Tool: runtime.ToolRequest{Name: "grep", Pattern: strings.TrimSpace(strings.TrimPrefix(trimmed, "/grep "))}}, nil
	case strings.HasPrefix(trimmed, "/fetch "):
		return Parsed{Kind: KindTool, Tool: runtime.ToolRequest{Name: "fetch", URL: strings.TrimSpace(strings.TrimPrefix(trimmed, "/fetch "))}}, nil
	case strings.HasPrefix(trimmed, "/shell "):
		return Parsed{Kind: KindTool, Tool: runtime.ToolRequest{Name: "shell", Command: strings.TrimSpace(strings.TrimPrefix(trimmed, "/shell "))}}, nil
	case strings.HasPrefix(trimmed, "/shell-bg "):
		return Parsed{Kind: KindTool, Tool: runtime.ToolRequest{Name: "shell_bg", Command: strings.TrimSpace(strings.TrimPrefix(trimmed, "/shell-bg "))}}, nil
	case strings.HasPrefix(trimmed, "/job-output "):
		return Parsed{Kind: KindTool, Tool: runtime.ToolRequest{Name: "job_output", JobID: strings.TrimSpace(strings.TrimPrefix(trimmed, "/job-output "))}}, nil
	case strings.HasPrefix(trimmed, "/job-kill "):
		return Parsed{Kind: KindTool, Tool: runtime.ToolRequest{Name: "job_kill", JobID: strings.TrimSpace(strings.TrimPrefix(trimmed, "/job-kill "))}}, nil
	case strings.HasPrefix(trimmed, "/write "):
		payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "/write "))
		parts := strings.SplitN(payload, "::", 2)
		if len(parts) != 2 {
			return Parsed{}, apperrors.New("commands.ParsePrompt", apperrors.CodeInvalidArgument, "write command must use /write <path>::<content>")
		}
		return Parsed{Kind: KindTool, Tool: runtime.ToolRequest{Name: "write_file", Path: strings.TrimSpace(parts[0]), Content: parts[1]}}, nil
	case strings.HasPrefix(trimmed, "/edit "):
		payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "/edit "))
		parts := strings.SplitN(payload, "::", 3)
		if len(parts) != 3 {
			return Parsed{}, apperrors.New("commands.ParsePrompt", apperrors.CodeInvalidArgument, "edit command must use /edit <path>::<expected-old-content>::<new-content>")
		}
		return Parsed{Kind: KindTool, Tool: runtime.ToolRequest{Name: "edit_file", Path: strings.TrimSpace(parts[0]), OldText: parts[1], NewText: parts[2]}}, nil
	default:
		return Parsed{Kind: KindNatural, Text: trimmed}, nil
	}
}

func parseViewCommand(payload string) (Parsed, error) {
	parts := strings.SplitN(payload, "::", 3)
	request := runtime.ToolRequest{Name: "view", Path: strings.TrimSpace(parts[0])}
	if request.Path == "" {
		return Parsed{}, apperrors.New("commands.parseViewCommand", apperrors.CodeInvalidArgument, "view command must use /view <path> or /view <path>::<start-line>::<limit>")
	}
	if len(parts) == 1 {
		return Parsed{Kind: KindTool, Tool: request}, nil
	}
	if len(parts) != 3 {
		return Parsed{}, apperrors.New("commands.parseViewCommand", apperrors.CodeInvalidArgument, "view command must use /view <path> or /view <path>::<start-line>::<limit>")
	}
	startLine, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || startLine < 1 {
		return Parsed{}, apperrors.New("commands.parseViewCommand", apperrors.CodeInvalidArgument, "view command start-line must be a positive integer")
	}
	limit, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil || limit < 1 {
		return Parsed{}, apperrors.New("commands.parseViewCommand", apperrors.CodeInvalidArgument, "view command limit must be a positive integer")
	}
	request.StartLine = startLine
	request.Limit = limit
	return Parsed{Kind: KindTool, Tool: request}, nil
}
