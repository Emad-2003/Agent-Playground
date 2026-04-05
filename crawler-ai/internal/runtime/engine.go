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
	CallID    string
	Name      string
	Path      string
	URL       string
	JobID     string
	StartLine int
	Limit     int
	Content   string
	OldText   string
	NewText   string
	Pattern   string
	Command   string
}

type ToolResult struct {
	CallID string
	Tool   string
	Output string
	Path   string
	Extra  map[string]string
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
	e.bus.Publish(EventToolStarted, map[string]any{"tool": request.Name, "call_id": request.CallID})

	result, err := e.execute(ctx, request)
	if err != nil {
		e.bus.Publish(EventToolFailed, map[string]any{"tool": request.Name, "call_id": request.CallID, "error": err.Error()})
		return ToolResult{}, err
	}

	payload := map[string]any{"tool": request.Name, "call_id": request.CallID, "path": result.Path}
	for key, value := range result.Extra {
		payload[key] = value
	}
	e.bus.Publish(EventToolCompleted, payload)
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
	case "view":
		result, err = e.tools.View(request.Path, request.StartLine, request.Limit)
	case "fetch":
		result, err = e.tools.Fetch(ctx, request.URL)
	case "write_file":
		result, err = e.tools.WriteFile(request.Path, request.Content)
	case "edit_file":
		result, err = e.tools.EditFile(request.Path, request.OldText, request.NewText)
	case "list_files":
		result, err = e.tools.ListFiles(request.Path)
	case "glob":
		result, err = e.tools.Glob(ctx, request.Pattern)
	case "grep":
		result, err = e.tools.Grep(ctx, request.Pattern)
	case "shell":
		result, err = e.tools.RunShell(ctx, request.Command)
	case "shell_bg":
		result, err = e.tools.RunBackgroundShell(ctx, request.Command)
	case "job_output":
		result, err = e.tools.GetBackgroundShellOutput(request.JobID)
	case "job_kill":
		result, err = e.tools.KillBackgroundShell(request.JobID)
	default:
		return ToolResult{}, apperrors.New("runtime.execute", apperrors.CodeInvalidArgument, "unknown tool requested")
	}
	if err != nil {
		return ToolResult{}, err
	}

	return ToolResult{CallID: request.CallID, Tool: request.Name, Output: result.Output, Path: result.Path, Extra: result.Extra}, nil
}
