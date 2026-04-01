package runtime

import (
	"context"

	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/events"
	"crawler-ai/internal/tools"
)

const (
	EventToolStarted   = "tool.started"
	EventToolCompleted = "tool.completed"
	EventToolFailed    = "tool.failed"
)

type ToolRequest struct {
	Name    string
	Path    string
	Content string
	Pattern string
	Command string
}

type ToolResult struct {
	Tool   string
	Output string
	Path   string
}

type Engine struct {
	bus   *events.Bus
	tools *tools.Executor
}

func NewEngine(workspaceRoot string, bus *events.Bus) (*Engine, error) {
	executor, err := tools.NewExecutor(workspaceRoot)
	if err != nil {
		return nil, err
	}

	if bus == nil {
		bus = events.NewBus()
	}

	return &Engine{bus: bus, tools: executor}, nil
}

func (e *Engine) Execute(ctx context.Context, request ToolRequest) (ToolResult, error) {
	e.bus.Publish(EventToolStarted, map[string]any{"tool": request.Name})

	result, err := e.execute(ctx, request)
	if err != nil {
		e.bus.Publish(EventToolFailed, map[string]any{"tool": request.Name, "error": err.Error()})
		return ToolResult{}, err
	}

	e.bus.Publish(EventToolCompleted, map[string]any{"tool": request.Name, "path": result.Path})
	return result, nil
}

func (e *Engine) execute(ctx context.Context, request ToolRequest) (ToolResult, error) {
	var (
		result tools.Result
		err    error
	)

	switch request.Name {
	case "read_file":
		result, err = e.tools.ReadFile(request.Path)
	case "write_file":
		result, err = e.tools.WriteFile(request.Path, request.Content)
	case "list_files":
		result, err = e.tools.ListFiles(request.Path)
	case "grep":
		result, err = e.tools.Grep(request.Pattern)
	case "shell":
		result, err = e.tools.RunShell(ctx, request.Command)
	default:
		return ToolResult{}, apperrors.New("runtime.execute", apperrors.CodeInvalidArgument, "unknown tool requested")
	}
	if err != nil {
		return ToolResult{}, err
	}

	return ToolResult{Tool: request.Name, Output: result.Output, Path: result.Path}, nil
}
