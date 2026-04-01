package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"crawler-ai/internal/config"
	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/events"
	"crawler-ai/internal/logging"
	"crawler-ai/internal/orchestrator"
	"crawler-ai/internal/provider"
	"crawler-ai/internal/runtime"
	"crawler-ai/internal/session"
	"crawler-ai/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

type App struct {
	config       config.Config
	bus          *events.Bus
	runtime      *runtime.Engine
	providers    *provider.Router
	sessions     *session.Manager
	sessionID    string
	planner      *orchestrator.Planner
	tasks        []domain.Task
	tasksMu      sync.Mutex
	pending      map[string]runtime.ToolRequest
	pendingMu    sync.Mutex
	now          func() time.Time
	approvalSeed atomic.Uint64
}

func New(cfg config.Config) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, apperrors.Wrap("app.New", apperrors.CodeInvalidConfig, err, "validate application configuration")
	}

	if _, err := logging.Configure(logging.Options{
		Environment: string(cfg.Env),
		Level:       cfg.LogLevel,
	}); err != nil {
		return nil, apperrors.Wrap("app.New", apperrors.CodeStartupFailed, err, "configure global logger")
	}

	bus := events.NewBus()
	runtimeEngine, err := runtime.NewEngine(cfg.WorkspaceRoot, bus)
	if err != nil {
		return nil, apperrors.Wrap("app.New", apperrors.CodeStartupFailed, err, "create runtime engine")
	}

	router, err := provider.NewRouter(cfg.Models)
	if err != nil {
		return nil, apperrors.Wrap("app.New", apperrors.CodeStartupFailed, err, "create provider router")
	}

	sessions := session.NewManager()
	defaultSession, err := sessions.Create("default", cfg.WorkspaceRoot)
	if err != nil {
		return nil, apperrors.Wrap("app.New", apperrors.CodeStartupFailed, err, "create default session")
	}

	return &App{
		config:    cfg,
		bus:       bus,
		runtime:   runtimeEngine,
		providers: router,
		sessions:  sessions,
		sessionID: defaultSession.ID,
		planner:   orchestrator.NewPlanner(),
		tasks:     make([]domain.Task, 0),
		pending:   make(map[string]runtime.ToolRequest),
		now: func() time.Time {
			return time.Now().UTC()
		},
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	if ctx == nil {
		return apperrors.New("app.Run", apperrors.CodeInvalidArgument, "context must not be nil")
	}

	logging.Info("application starting",
		"environment", a.config.Env,
		"workspace_root", a.config.WorkspaceRoot,
		"log_level", a.config.LogLevel,
	)

	a.bus.Publish(events.EventAppStarted, map[string]any{
		"workspace_root": a.config.WorkspaceRoot,
		"environment":    a.config.Env,
	})
	a.bus.Publish(events.EventStatusUpdated, "Application started")

	logging.Debug("foundation services initialized")
	if a.config.Env == config.EnvTest {
		return nil
	}

	return a.runInteractive(ctx)
}

func (a *App) HandlePrompt(ctx context.Context, prompt string) error {
	if ctx == nil {
		return apperrors.New("app.HandlePrompt", apperrors.CodeInvalidArgument, "context must not be nil")
	}

	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return apperrors.New("app.HandlePrompt", apperrors.CodeInvalidArgument, "prompt must not be empty")
	}

	a.publishTranscript(domain.TranscriptEntry{
		ID:        a.nextID("user"),
		Kind:      domain.TranscriptUser,
		Message:   trimmed,
		CreatedAt: a.now(),
	})

	request, dangerous, handled, err := a.parseToolRequest(trimmed)
	if err != nil {
		return err
	}
	if strings.HasPrefix(trimmed, "/plan ") {
		return a.planPrompt(strings.TrimSpace(strings.TrimPrefix(trimmed, "/plan ")))
	}
	if strings.HasPrefix(trimmed, "/run ") {
		return a.runPlan(ctx, strings.TrimSpace(strings.TrimPrefix(trimmed, "/run ")))
	}
	if handled {
		if dangerous {
			approval := domain.ApprovalRequest{
				ID:          a.nextID("approval"),
				Action:      request.Name,
				Description: trimmed,
				CreatedAt:   a.now(),
			}
			a.pendingMu.Lock()
			a.pending[approval.ID] = request
			a.pendingMu.Unlock()
			a.bus.Publish(events.EventApprovalRequested, approval)
			a.bus.Publish(events.EventStatusUpdated, "Approval required")
			return nil
		}

		return a.executeTool(ctx, request)
	}

	selected, err := a.providers.ForRole(provider.RoleOrchestrator)
	if err != nil {
		return err
	}

	response, err := selected.Complete(ctx, provider.Request{
		Model: a.config.Models.Orchestrator.Model,
		Messages: []provider.Message{{
			Role:    "user",
			Content: trimmed,
		}},
		MaxTokens:   1024,
		Temperature: 0.2,
	})
	if err != nil {
		return err
	}

	a.publishTranscript(domain.TranscriptEntry{
		ID:        a.nextID("assistant"),
		Kind:      domain.TranscriptAssistant,
		Message:   response.Text,
		CreatedAt: a.now(),
		Metadata: map[string]string{
			"provider": selected.Name(),
			"model":    a.config.Models.Orchestrator.Model,
		},
	})
	a.bus.Publish(events.EventStatusUpdated, "Prompt completed")
	return nil
}

func (a *App) ResolveApproval(ctx context.Context, approvalID string, approved bool) error {
	if ctx == nil {
		return apperrors.New("app.ResolveApproval", apperrors.CodeInvalidArgument, "context must not be nil")
	}

	a.pendingMu.Lock()
	request, ok := a.pending[approvalID]
	if ok {
		delete(a.pending, approvalID)
	}
	a.pendingMu.Unlock()
	if !ok {
		return apperrors.New("app.ResolveApproval", apperrors.CodeInvalidArgument, "approval request not found")
	}

	a.bus.Publish(events.EventApprovalCleared, approvalID)
	if !approved {
		a.publishTranscript(domain.TranscriptEntry{
			ID:        a.nextID("system"),
			Kind:      domain.TranscriptSystem,
			Message:   "Approval rejected: " + request.Name,
			CreatedAt: a.now(),
		})
		a.bus.Publish(events.EventStatusUpdated, "Approval rejected")
		return nil
	}

	a.bus.Publish(events.EventStatusUpdated, "Approval granted")
	return a.executeTool(ctx, request)
}

func (a *App) executeTool(ctx context.Context, request runtime.ToolRequest) error {
	result, err := a.runtime.Execute(ctx, request)
	if err != nil {
		return err
	}

	message := result.Output
	if strings.TrimSpace(result.Path) != "" {
		message = result.Path + "\n" + result.Output
	}

	a.publishTranscript(domain.TranscriptEntry{
		ID:        a.nextID("tool"),
		Kind:      domain.TranscriptTool,
		Message:   message,
		CreatedAt: a.now(),
		Metadata: map[string]string{
			"tool": result.Tool,
		},
	})
	a.bus.Publish(events.EventStatusUpdated, "Tool completed: "+result.Tool)
	return nil
}

func (a *App) publishTranscript(entry domain.TranscriptEntry) {
	_ = a.sessions.AppendTranscript(a.sessionID, entry)
	a.bus.Publish(events.EventTranscriptAdded, entry)
}

func (a *App) parseToolRequest(prompt string) (runtime.ToolRequest, bool, bool, error) {
	switch {
	case strings.HasPrefix(prompt, "/read "):
		return runtime.ToolRequest{Name: "read_file", Path: strings.TrimSpace(strings.TrimPrefix(prompt, "/read "))}, false, true, nil
	case strings.HasPrefix(prompt, "/list"):
		return runtime.ToolRequest{Name: "list_files", Path: strings.TrimSpace(strings.TrimPrefix(prompt, "/list"))}, false, true, nil
	case strings.HasPrefix(prompt, "/grep "):
		return runtime.ToolRequest{Name: "grep", Pattern: strings.TrimSpace(strings.TrimPrefix(prompt, "/grep "))}, false, true, nil
	case strings.HasPrefix(prompt, "/shell "):
		return runtime.ToolRequest{Name: "shell", Command: strings.TrimSpace(strings.TrimPrefix(prompt, "/shell "))}, true, true, nil
	case strings.HasPrefix(prompt, "/write "):
		payload := strings.TrimSpace(strings.TrimPrefix(prompt, "/write "))
		parts := strings.SplitN(payload, "::", 2)
		if len(parts) != 2 {
			return runtime.ToolRequest{}, false, false, apperrors.New("app.parseToolRequest", apperrors.CodeInvalidArgument, "write command must use /write <path>::<content>")
		}
		return runtime.ToolRequest{Name: "write_file", Path: strings.TrimSpace(parts[0]), Content: parts[1]}, true, true, nil
	default:
		return runtime.ToolRequest{}, false, false, nil
	}
}

func (a *App) planPrompt(prompt string) error {
	tasks := a.planner.Build(prompt)
	a.setTasks(tasks)
	a.publishSystemMessage(fmt.Sprintf("Plan created with %d tasks.", len(tasks)))
	a.bus.Publish(events.EventStatusUpdated, "Plan created")
	return nil
}

func (a *App) runPlan(ctx context.Context, prompt string) error {
	if ctx == nil {
		return apperrors.New("app.runPlan", apperrors.CodeInvalidArgument, "context must not be nil")
	}

	queue := orchestrator.NewQueue(a.planner.Build(prompt))
	a.setTasks(queue.List())

	for {
		task, ok := queue.NextReady()
		if !ok {
			break
		}

		if err := queue.Start(task.ID); err != nil {
			return err
		}
		a.setTasks(queue.List())
		a.bus.Publish(events.EventStatusUpdated, "Running task: "+task.Title)

		result, err := a.executePlannedTask(ctx, task)
		if err != nil {
			_ = queue.Fail(task.ID, err.Error())
			a.setTasks(queue.List())
			return err
		}

		if err := queue.Complete(task.ID, result); err != nil {
			return err
		}
		a.setTasks(queue.List())
	}

	a.publishSystemMessage("Planned run completed.")
	a.bus.Publish(events.EventStatusUpdated, "Planned run completed")
	return nil
}

func (a *App) executePlannedTask(ctx context.Context, task domain.Task) (string, error) {
	role := provider.RoleWorker
	model := a.config.Models.Worker.Model
	if task.Assignee == domain.RoleOrchestrator {
		role = provider.RoleOrchestrator
		model = a.config.Models.Orchestrator.Model
	}

	selected, err := a.providers.ForRole(role)
	if err != nil {
		return "", err
	}

	response, err := selected.Complete(ctx, provider.Request{
		Model: model,
		Messages: []provider.Message{{
			Role:    "user",
			Content: task.Description,
		}},
		SystemPrompt: fmt.Sprintf("You are acting as the %s role for task %s.", task.Assignee, task.Title),
		MaxTokens:    1024,
		Temperature:  0.2,
	})
	if err != nil {
		return "", err
	}

	a.publishTranscript(domain.TranscriptEntry{
		ID:        a.nextID("assistant"),
		Kind:      domain.TranscriptAssistant,
		Message:   fmt.Sprintf("Task %s: %s", task.Title, response.Text),
		CreatedAt: a.now(),
		Metadata: map[string]string{
			"task_id": task.ID,
			"role":    string(task.Assignee),
			"model":   model,
		},
	})
	return response.Text, nil
}

func (a *App) setTasks(tasks []domain.Task) {
	a.tasksMu.Lock()
	defer a.tasksMu.Unlock()

	a.tasks = append([]domain.Task(nil), tasks...)
	taskIDs := make([]string, 0, len(tasks))
	for _, task := range tasks {
		taskIDs = append(taskIDs, task.ID)
	}
	_ = a.sessions.SetActiveTasks(a.sessionID, taskIDs)
	a.bus.Publish(events.EventTasksUpdated, append([]domain.Task(nil), tasks...))
}

func (a *App) nextID(prefix string) string {
	sequence := a.approvalSeed.Add(1)
	return fmt.Sprintf("%s-%d", prefix, sequence)
}

func (a *App) runInteractive(ctx context.Context) error {
	model := ui.NewModel(
		ui.WithSubmitHandler(func(prompt string) {
			go func() {
				if err := a.HandlePrompt(ctx, prompt); err != nil {
					a.publishSystemMessage("Request failed: " + err.Error())
					a.bus.Publish(events.EventStatusUpdated, "Request failed")
				}
			}()
		}),
		ui.WithApprovalHandler(func(request domain.ApprovalRequest, approved bool) {
			go func() {
				if err := a.ResolveApproval(ctx, request.ID, approved); err != nil {
					a.publishSystemMessage("Approval resolution failed: " + err.Error())
					a.bus.Publish(events.EventStatusUpdated, "Approval resolution failed")
				}
			}()
		}),
	)

	program := tea.NewProgram(model, tea.WithAltScreen())
	unsubscribers := []func(){
		a.bus.Subscribe(events.EventTranscriptAdded, func(event events.Event) {
			entry, ok := event.Payload.(domain.TranscriptEntry)
			if ok {
				program.Send(ui.AddTranscriptMsg{Entry: entry})
			}
		}),
		a.bus.Subscribe(events.EventStatusUpdated, func(event events.Event) {
			status, ok := event.Payload.(string)
			if ok {
				program.Send(ui.SetStatusMsg{Status: status})
			}
		}),
		a.bus.Subscribe(events.EventApprovalRequested, func(event events.Event) {
			request, ok := event.Payload.(domain.ApprovalRequest)
			if ok {
				program.Send(ui.ShowApprovalMsg{Request: request})
			}
		}),
		a.bus.Subscribe(events.EventTasksUpdated, func(event events.Event) {
			tasks, ok := event.Payload.([]domain.Task)
			if ok {
				program.Send(ui.SetTasksMsg{Tasks: tasks})
			}
		}),
		a.bus.Subscribe(events.EventApprovalCleared, func(event events.Event) {
			program.Send(ui.ClearApprovalMsg{})
		}),
	}
	defer func() {
		for _, unsubscribe := range unsubscribers {
			unsubscribe()
		}
	}()

	a.publishSystemMessage("Interactive mode ready. Use /read, /list, /grep, /write, /shell, /plan, /run, or natural language prompts. Submit with Ctrl+S.")
	_, err := program.Run()
	if err != nil {
		return apperrors.Wrap("app.runInteractive", apperrors.CodeStartupFailed, err, "run Bubble Tea program")
	}

	return nil
}

func (a *App) publishSystemMessage(message string) {
	a.publishTranscript(domain.TranscriptEntry{
		ID:        a.nextID("system"),
		Kind:      domain.TranscriptSystem,
		Message:   message,
		CreatedAt: a.now(),
	})
}

func (a *App) String() string {
	return fmt.Sprintf("App(env=%s, workspace=%s)", a.config.Env, a.config.WorkspaceRoot)
}
