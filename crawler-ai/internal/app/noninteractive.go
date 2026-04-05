package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	stdRuntime "runtime"
	"sort"
	"strings"
	"time"

	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/events"
	"crawler-ai/internal/provider"
	runtimepkg "crawler-ai/internal/runtime"
	"crawler-ai/internal/session"
)

type NonInteractiveRunStatus string

const (
	NonInteractiveRunStatusOK               NonInteractiveRunStatus = "ok"
	NonInteractiveRunStatusApprovalRequired NonInteractiveRunStatus = "approval_required"
	NonInteractiveRunStatusProviderFailed   NonInteractiveRunStatus = "provider_failed"
	NonInteractiveRunStatusError            NonInteractiveRunStatus = "error"
)

const (
	NonInteractiveRunExitOK               = 0
	NonInteractiveRunExitError            = 1
	NonInteractiveRunExitApprovalRequired = 2
	NonInteractiveRunExitProviderFailed   = 3
)

type NonInteractiveProgressKind string

const (
	NonInteractiveProgressStatus            NonInteractiveProgressKind = "status"
	NonInteractiveProgressReasoning         NonInteractiveProgressKind = "reasoning"
	NonInteractiveProgressTask             NonInteractiveProgressKind = "task"
	NonInteractiveProgressToolStarted      NonInteractiveProgressKind = "tool_started"
	NonInteractiveProgressToolCompleted    NonInteractiveProgressKind = "tool_completed"
	NonInteractiveProgressToolFailed       NonInteractiveProgressKind = "tool_failed"
	NonInteractiveProgressApprovalRequired NonInteractiveProgressKind = "approval_required"
	NonInteractiveProgressBusy             NonInteractiveProgressKind = "busy"
	NonInteractiveProgressTokenUsage       NonInteractiveProgressKind = "token_usage"
)

type NonInteractiveProgressEvent struct {
	Kind         NonInteractiveProgressKind `json:"kind"`
	Timestamp    string                     `json:"timestamp"`
	ID           string                     `json:"id,omitempty"`
	Message      string                     `json:"message,omitempty"`
	Detail       string                     `json:"detail,omitempty"`
	Tool         string                     `json:"tool,omitempty"`
	Path         string                     `json:"path,omitempty"`
	Busy         bool                       `json:"busy,omitempty"`
	InputTokens  int64                      `json:"input_tokens,omitempty"`
	OutputTokens int64                      `json:"output_tokens,omitempty"`
	TotalCost    float64                    `json:"total_cost,omitempty"`
}

type NonInteractiveRunOptions struct {
	SessionID    string
	ContinueLast bool
	Progress     func(NonInteractiveProgressEvent)
}

type NonInteractiveRunFailure struct {
	Type           string                            `json:"type"`
	Code           string                            `json:"code,omitempty"`
	Message        string                            `json:"message"`
	Operation      string                            `json:"operation,omitempty"`
	ApprovalAction string                            `json:"approval_action,omitempty"`
	ProviderStatus *NonInteractiveProviderStatusInfo `json:"provider_status,omitempty"`
}

type NonInteractiveProviderStatusInfo struct {
	Operation  string `json:"operation,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Message    string `json:"message,omitempty"`
	Body       string `json:"body,omitempty"`
}

type NonInteractiveRunResult struct {
	Status         NonInteractiveRunStatus   `json:"status"`
	ExitCode       int                       `json:"exit_code"`
	Failure        *NonInteractiveRunFailure `json:"failure,omitempty"`
	SessionID      string                    `json:"session_id"`
	Session        session.Session           `json:"session"`
	Entries        []domain.TranscriptEntry  `json:"entries"`
	RenderedOutput string                    `json:"rendered_output,omitempty"`
}

func (a *App) RunNonInteractive(ctx context.Context, output io.Writer, prompt string, options NonInteractiveRunOptions) (NonInteractiveRunResult, error) {
	if ctx == nil {
		return NonInteractiveRunResult{}, apperrors.New("app.RunNonInteractive", apperrors.CodeInvalidArgument, "context must not be nil")
	}
	if output == nil {
		return NonInteractiveRunResult{}, apperrors.New("app.RunNonInteractive", apperrors.CodeInvalidArgument, "output writer must not be nil")
	}
	if strings.TrimSpace(prompt) == "" {
		return NonInteractiveRunResult{}, apperrors.New("app.RunNonInteractive", apperrors.CodeInvalidArgument, "prompt must not be empty")
	}
	if strings.TrimSpace(options.SessionID) != "" && options.ContinueLast {
		return NonInteractiveRunResult{}, apperrors.New("app.RunNonInteractive", apperrors.CodeInvalidArgument, "session id and continue-last are mutually exclusive")
	}

	target, err := a.resolveNonInteractiveSession(options.SessionID, options.ContinueLast)
	if err != nil {
		return classifyNonInteractiveRunResult(NonInteractiveRunResult{}, err), err
	}
	if err := a.switchSession(target.ID); err != nil {
		return classifyNonInteractiveRunResult(NonInteractiveRunResult{SessionID: target.ID}, err), err
	}

	progressCleanup := a.subscribeNonInteractiveProgress(options.Progress)
	defer progressCleanup()
	if options.Progress != nil {
		options.Progress(NonInteractiveProgressEvent{
			Kind:      NonInteractiveProgressStatus,
			Timestamp: a.now().Format(time.RFC3339Nano),
			Message:   "Starting run",
		})
	}

	before, ok := a.sessions.Get(target.ID)
	if !ok {
		err := apperrors.New("app.RunNonInteractive", apperrors.CodeInvalidArgument, "target session not found")
		return classifyNonInteractiveRunResult(NonInteractiveRunResult{SessionID: target.ID}, err), err
	}
	baseline := len(before.Transcript)

	approvalCh := make(chan domain.ApprovalRequest, 1)
	unsubscribe := a.bus.Subscribe(events.EventApprovalRequested, func(event events.Event) {
		request, ok := event.Payload.(domain.ApprovalRequest)
		if !ok {
			return
		}
		select {
		case approvalCh <- request:
		default:
		}
	})
	defer unsubscribe()

	if err := a.HandlePrompt(ctx, prompt); err != nil {
		result, _ := a.completeNonInteractiveResult(target.ID, baseline, err, "")
		return result, err
	}

	select {
	case approval := <-approvalCh:
		err := apperrors.New("app.RunNonInteractive", apperrors.CodePermissionDenied, fmt.Sprintf("non-interactive run requires approval for %s; rerun with --yolo or use interactive mode", approval.Action))
		result, _ := a.completeNonInteractiveResult(target.ID, baseline, err, approval.Action)
		return result, err
	default:
	}

	result, completeErr := a.completeNonInteractiveResult(target.ID, baseline, nil, "")
	if completeErr != nil {
		return result, completeErr
	}
	rendered := result.RenderedOutput
	if rendered != "" {
		if _, err := io.WriteString(output, rendered); err != nil {
			return classifyNonInteractiveRunResult(result, err), err
		}
		if !strings.HasSuffix(rendered, "\n") {
			if _, err := io.WriteString(output, "\n"); err != nil {
				return classifyNonInteractiveRunResult(result, err), err
			}
		}
	}

	return result, nil
}

func (a *App) subscribeNonInteractiveProgress(progress func(NonInteractiveProgressEvent)) func() {
	if a == nil || a.bus == nil || progress == nil {
		return func() {}
	}

	unsubscribers := []func(){
		a.bus.Subscribe(events.EventStatusUpdated, func(event events.Event) {
			status, ok := event.Payload.(string)
			if !ok || strings.TrimSpace(status) == "" {
				return
			}
			progress(NonInteractiveProgressEvent{
				Kind:      NonInteractiveProgressStatus,
				Timestamp: event.Timestamp.Format(time.RFC3339Nano),
				Message:   strings.TrimSpace(status),
			})
		}),
		a.bus.Subscribe(events.EventTranscriptAdded, func(event events.Event) {
			if entry, ok := nonInteractiveReasoningProgressEvent(event); ok {
				progress(entry)
			}
		}),
		a.bus.Subscribe(events.EventTranscriptUpdated, func(event events.Event) {
			if entry, ok := nonInteractiveReasoningProgressEvent(event); ok {
				progress(entry)
			}
		}),
		a.bus.Subscribe(events.EventTasksUpdated, func(event events.Event) {
			tasks, ok := event.Payload.([]domain.Task)
			if !ok {
				return
			}
			if message := summarizeNonInteractiveTasks(tasks); message != "" {
				progress(NonInteractiveProgressEvent{
					Kind:      NonInteractiveProgressTask,
					Timestamp: event.Timestamp.Format(time.RFC3339Nano),
					Message:   message,
				})
			}
		}),
		a.bus.Subscribe(runtimepkg.EventToolStarted, func(event events.Event) {
			if entry, ok := nonInteractiveToolProgressEvent(event, NonInteractiveProgressToolStarted); ok {
				progress(entry)
			}
		}),
		a.bus.Subscribe(runtimepkg.EventToolCompleted, func(event events.Event) {
			if entry, ok := nonInteractiveToolProgressEvent(event, NonInteractiveProgressToolCompleted); ok {
				progress(entry)
			}
		}),
		a.bus.Subscribe(runtimepkg.EventToolFailed, func(event events.Event) {
			if entry, ok := nonInteractiveToolProgressEvent(event, NonInteractiveProgressToolFailed); ok {
				progress(entry)
			}
		}),
		a.bus.Subscribe(events.EventApprovalRequested, func(event events.Event) {
			request, ok := event.Payload.(domain.ApprovalRequest)
			if !ok {
				return
			}
			progress(NonInteractiveProgressEvent{
				Kind:      NonInteractiveProgressApprovalRequired,
				Timestamp: event.Timestamp.Format(time.RFC3339Nano),
				ID:        request.ID,
				Message:   strings.TrimSpace(request.Action),
			})
		}),
		a.bus.Subscribe(events.EventSessionBusy, func(event events.Event) {
			busy, ok := event.Payload.(bool)
			if !ok {
				return
			}
			progress(NonInteractiveProgressEvent{
				Kind:      NonInteractiveProgressBusy,
				Timestamp: event.Timestamp.Format(time.RFC3339Nano),
				Busy:      busy,
			})
		}),
		a.bus.Subscribe(events.EventTokenUsage, func(event events.Event) {
			payload, ok := event.Payload.(map[string]any)
			if !ok {
				return
			}
			entry, ok := nonInteractiveTokenUsageProgressEvent(event.Timestamp, payload)
			if !ok {
				return
			}
			progress(entry)
		}),
	}

	return func() {
		for _, unsubscribe := range unsubscribers {
			unsubscribe()
		}
	}
}

func nonInteractiveReasoningProgressEvent(event events.Event) (NonInteractiveProgressEvent, bool) {
	entry, ok := event.Payload.(domain.TranscriptEntry)
	if !ok || entry.Kind != domain.TranscriptReasoning {
		return NonInteractiveProgressEvent{}, false
	}
	message := strings.TrimSpace(entry.Message)
	if message == "" {
		return NonInteractiveProgressEvent{}, false
	}
	return NonInteractiveProgressEvent{
		Kind:      NonInteractiveProgressReasoning,
		Timestamp: event.Timestamp.Format(time.RFC3339Nano),
		ID:        entry.ID,
		Message:   message,
	}, true
}

func nonInteractiveToolProgressEvent(event events.Event, kind NonInteractiveProgressKind) (NonInteractiveProgressEvent, bool) {
	payload, ok := event.Payload.(map[string]any)
	if !ok {
		return NonInteractiveProgressEvent{}, false
	}
	return NonInteractiveProgressEvent{
		Kind:      kind,
		Timestamp: event.Timestamp.Format(time.RFC3339Nano),
		ID:        stringValue(payload["call_id"]),
		Tool:      stringValue(payload["tool"]),
		Path:      firstNonEmpty(stringValue(payload["path"]), stringValue(payload["job_id"])),
		Detail:    stringValue(payload["error"]),
	}, true
}

func nonInteractiveTokenUsageProgressEvent(timestamp time.Time, payload map[string]any) (NonInteractiveProgressEvent, bool) {
	entry := NonInteractiveProgressEvent{
		Kind:      NonInteractiveProgressTokenUsage,
		Timestamp: timestamp.Format(time.RFC3339Nano),
	}
	if totalIn, ok := int64Value(payload["total_input_tokens"]); ok {
		entry.InputTokens = totalIn
	}
	if totalOut, ok := int64Value(payload["total_output_tokens"]); ok {
		entry.OutputTokens = totalOut
	}
	if totalCost, ok := float64Value(payload["total_cost"]); ok {
		entry.TotalCost = totalCost
	}
	if entry.InputTokens == 0 && entry.OutputTokens == 0 {
		if in, ok := int64Value(payload["input_tokens"]); ok {
			entry.InputTokens = in
		}
		if out, ok := int64Value(payload["output_tokens"]); ok {
			entry.OutputTokens = out
		}
		if cost, ok := float64Value(payload["estimated_cost"]); ok {
			entry.TotalCost = cost
		}
	}
	if entry.InputTokens == 0 && entry.OutputTokens == 0 && entry.TotalCost == 0 {
		return NonInteractiveProgressEvent{}, false
	}
	return entry, true
}

func summarizeNonInteractiveTasks(tasks []domain.Task) string {
	if len(tasks) == 0 {
		return ""
	}
	running := make([]string, 0, len(tasks))
	completed := 0
	failed := 0
	pending := 0
	blocked := 0
	for _, task := range tasks {
		switch task.Status {
		case domain.TaskRunning:
			running = append(running, strings.TrimSpace(task.Title))
		case domain.TaskCompleted:
			completed++
		case domain.TaskFailed:
			failed++
		case domain.TaskBlocked:
			blocked++
		default:
			pending++
		}
	}
	if len(running) > 0 {
		message := "Running task: " + running[0]
		if len(running) > 1 {
			message += fmt.Sprintf(" (+%d more)", len(running)-1)
		}
		return message
	}
	parts := make([]string, 0, 4)
	if completed > 0 {
		parts = append(parts, fmt.Sprintf("%d completed", completed))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", pending))
	}
	if blocked > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", blocked))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Tasks updated: " + strings.Join(parts, ", ")
}

func int64Value(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case int32:
		return int64(typed), true
	case float64:
		return int64(typed), true
	default:
		return 0, false
	}
}

func float64Value(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func (a *App) completeNonInteractiveResult(sessionID string, baseline int, err error, approvalAction string) (NonInteractiveRunResult, error) {
	result := NonInteractiveRunResult{
		Status:    NonInteractiveRunStatusOK,
		ExitCode:  NonInteractiveRunExitOK,
		SessionID: sessionID,
	}
	if strings.TrimSpace(sessionID) == "" {
		return classifyNonInteractiveRunResult(result, err, approvalAction), err
	}
	after, ok := a.sessions.Get(sessionID)
	if !ok {
		if err != nil {
			return classifyNonInteractiveRunResult(result, err, approvalAction), err
		}
		missingErr := apperrors.New("app.RunNonInteractive", apperrors.CodeInvalidArgument, "target session disappeared after run")
		return classifyNonInteractiveRunResult(result, missingErr, approvalAction), missingErr
	}
	if baseline < 0 {
		baseline = 0
	}
	if baseline > len(after.Transcript) {
		baseline = len(after.Transcript)
	}
	result.Session = after
	result.Entries = visibleNonInteractiveEntries(after.Transcript[baseline:])
	result.RenderedOutput = renderNonInteractiveEntries(result.Entries)
	return classifyNonInteractiveRunResult(result, err, approvalAction), err
}

func classifyNonInteractiveRunResult(result NonInteractiveRunResult, err error, approvalAction ...string) NonInteractiveRunResult {
	if err == nil {
		result.Status = NonInteractiveRunStatusOK
		result.ExitCode = NonInteractiveRunExitOK
		result.Failure = nil
		return result
	}

	failure := &NonInteractiveRunFailure{Message: err.Error(), Type: string(NonInteractiveRunStatusError)}
	result.Status = NonInteractiveRunStatusError
	result.ExitCode = NonInteractiveRunExitError

	var appErr *apperrors.Error
	if errors.As(err, &appErr) {
		failure.Code = string(appErr.Code)
		failure.Operation = appErr.Op
		switch appErr.Code {
		case apperrors.CodePermissionDenied:
			result.Status = NonInteractiveRunStatusApprovalRequired
			result.ExitCode = NonInteractiveRunExitApprovalRequired
			failure.Type = string(NonInteractiveRunStatusApprovalRequired)
		case apperrors.CodeProviderFailed:
			result.Status = NonInteractiveRunStatusProviderFailed
			result.ExitCode = NonInteractiveRunExitProviderFailed
			failure.Type = string(NonInteractiveRunStatusProviderFailed)
		}
	}

	var statusErr *provider.StatusError
	if errors.As(err, &statusErr) {
		result.Status = NonInteractiveRunStatusProviderFailed
		result.ExitCode = NonInteractiveRunExitProviderFailed
		failure.Type = string(NonInteractiveRunStatusProviderFailed)
		if failure.Code == "" {
			failure.Code = string(apperrors.CodeProviderFailed)
		}
		failure.ProviderStatus = &NonInteractiveProviderStatusInfo{
			Operation:  statusErr.Operation,
			StatusCode: statusErr.StatusCode,
			Message:    statusErr.Message,
			Body:       statusErr.Body,
		}
	}

	if len(approvalAction) > 0 && strings.TrimSpace(approvalAction[0]) != "" {
		failure.ApprovalAction = strings.TrimSpace(approvalAction[0])
	}

	result.Failure = failure
	return result
}

func (a *App) resolveNonInteractiveSession(explicitID string, continueLast bool) (session.Session, error) {
	if strings.TrimSpace(explicitID) != "" {
		sess, ok := a.sessions.Get(strings.TrimSpace(explicitID))
		if !ok {
			return session.Session{}, apperrors.New("app.resolveNonInteractiveSession", apperrors.CodeInvalidArgument, "session not found: "+strings.TrimSpace(explicitID))
		}
		if !sameWorkspacePath(sess.WorkspaceRoot, a.config.WorkspaceRoot) {
			return session.Session{}, apperrors.New("app.resolveNonInteractiveSession", apperrors.CodeInvalidArgument, "session belongs to a different workspace: "+sess.WorkspaceRoot)
		}
		return sess, nil
	}

	if continueLast {
		sessions := a.sessions.List()
		sort.Slice(sessions, func(i, j int) bool {
			if !sessions[i].UpdatedAt.Equal(sessions[j].UpdatedAt) {
				return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
			}
			if !sessions[i].CreatedAt.Equal(sessions[j].CreatedAt) {
				return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
			}
			return sessions[i].ID < sessions[j].ID
		})
		for _, sess := range sessions {
			if sameWorkspacePath(sess.WorkspaceRoot, a.config.WorkspaceRoot) {
				return sess, nil
			}
		}
		return session.Session{}, apperrors.New("app.resolveNonInteractiveSession", apperrors.CodeInvalidArgument, "no sessions found for workspace: "+a.config.WorkspaceRoot)
	}

	created, err := a.sessions.Create(a.nextID("run"), a.config.WorkspaceRoot)
	if err != nil {
		return session.Session{}, err
	}
	return created, nil
}

func sameWorkspacePath(left, right string) bool {
	left = normalizeWorkspacePath(left)
	right = normalizeWorkspacePath(right)
	if stdRuntime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func normalizeWorkspacePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func visibleNonInteractiveEntries(entries []domain.TranscriptEntry) []domain.TranscriptEntry {
	visible := make([]domain.TranscriptEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Kind == domain.TranscriptUser {
			continue
		}
		if entry.Metadata != nil && entry.Metadata["status"] == "in_progress" {
			continue
		}
		visible = append(visible, entry)
	}
	return visible
}

func renderNonInteractiveEntries(entries []domain.TranscriptEntry) string {
	if len(entries) == 0 {
		return ""
	}

	sections := make([]string, 0, len(entries))
	assistantOnly := true
	for _, entry := range entries {
		if entry.Kind != domain.TranscriptAssistant {
			assistantOnly = false
			break
		}
	}

	for _, entry := range entries {
		message := strings.TrimSpace(entry.Message)
		if message == "" {
			continue
		}
		switch entry.Kind {
		case domain.TranscriptAssistant:
			sections = append(sections, message)
		case domain.TranscriptReasoning:
			if !assistantOnly {
				sections = append(sections, "Reasoning:\n"+message)
			}
		case domain.TranscriptSystem:
			sections = append(sections, "System:\n"+message)
		case domain.TranscriptTool:
			sections = append(sections, message)
		default:
			sections = append(sections, message)
		}
	}

	return strings.Join(sections, "\n\n")
}
