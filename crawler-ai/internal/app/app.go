package app

import (
	"context"
	"crawler-ai/internal/commands"
	"crawler-ai/internal/config"
	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/events"
	"crawler-ai/internal/logging"
	"crawler-ai/internal/orchestrator"
	"crawler-ai/internal/permission"
	"crawler-ai/internal/provider"
	"crawler-ai/internal/runtime"
	"crawler-ai/internal/session"
	"crawler-ai/internal/shell"
	"crawler-ai/internal/tui"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const interactiveReadyMessage = "Interactive mode ready. Use /read, /view, /list, /glob, /grep, /fetch, /write, /edit, /shell, /shell-bg, /job-output, /job-kill, /plan, /run, or natural language prompts. Submit with Ctrl+S."

type App struct {
	config       config.Config
	bus          *events.Bus
	runtime      *runtime.Engine
	providers    *provider.Router
	loop         *sessionLoop
	coordinator  *sessionCoordinator
	permissions  *permission.Service
	sessions     *session.Manager
	messages     messageStateService
	taskState    taskStateService
	usageState   usageStateService
	fileTracker  fileTrackingStateService
	toolExecutor *sessionToolExecutor
	lineage      sessionLineageService
	context      codingContextPromptService
	sessionID    string
	planner      *orchestrator.Planner
	pending      map[string]runtime.ToolRequest
	pendingMu    sync.Mutex
	now          func() time.Time
	approvalSeed atomic.Uint64
	logCleanup   func()
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

	// Set up persistent file logging alongside the console logger.
	logPath, logCleanup, logErr := logging.SetupFileLogging(cfg.LogLevel)
	if logErr != nil {
		logging.Warn("file logging unavailable", "error", logErr)
		logCleanup = func() {} // no-op
	} else {
		logging.Debug("file logging enabled", "path", logPath)
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
	if cfg.Env != config.EnvTest {
		sessions.SetDataDir(session.DefaultDataDir())
		if err := sessions.LoadAll(); err != nil {
			logging.Warn("could not load saved sessions", "error", err)
		}
	}

	defaultSession, err := sessions.Create("default", cfg.WorkspaceRoot)
	if err != nil {
		// If "default" already exists from a previous run, just use it.
		if s, ok := sessions.Get("default"); ok {
			defaultSession = s
			err = nil
		}
	}
	if err != nil {
		return nil, apperrors.Wrap("app.New", apperrors.CodeStartupFailed, err, "create default session")
	}

	application := &App{
		config:      cfg,
		bus:         bus,
		runtime:     runtimeEngine,
		providers:   router,
		loop:        newSessionLoop(),
		permissions: permission.NewService(cfg.Permissions, cfg.Yolo),
		sessions:    sessions,
		sessionID:   defaultSession.ID,
		planner:     orchestrator.NewPlanner(),
		pending:     make(map[string]runtime.ToolRequest),
		logCleanup:  logCleanup,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
	stateServices := newSessionStateServices(sessions, bus, func() string { return application.sessionID })
	application.messages = stateServices.messages
	application.taskState = stateServices.tasks
	application.usageState = stateServices.usage
	application.fileTracker = stateServices.fileTracker
	application.toolExecutor = newSessionToolExecutor(application.runtime, application.permissions, application.messages, application.fileTracker, application.sessions, application.nextID, application.now, func(message string) {
		application.bus.Publish(events.EventStatusUpdated, message)
	})
	application.lineage = newSessionLineageService(sessions)
	application.context = session.NewCodingContextService(sessions, session.NewCommandWorkspaceDiagnosticsProvider())
	application.coordinator = newSessionCoordinator(sessionCoordinatorOptions{
		messages: application.messages,
		tasks:    application.taskState,
		usage:    application.usageState,
		tools:    application.toolExecutor,
		lineage:  application.lineage,
		context:  application.context,
		providers: func() providerResolver {
			return application.providers
		},
		models:  func() config.ModelConfig { return application.config.Models },
		bus:     bus,
		loop:    application.loop,
		planner: application.planner,
		now:     application.now,
		nextID:  application.nextID,
		status: func(message string) {
			application.bus.Publish(events.EventStatusUpdated, message)
		},
	})
	return application, nil
}

func (a *App) Run(ctx context.Context) error {
	if ctx == nil {
		return apperrors.New("app.Run", apperrors.CodeInvalidArgument, "context must not be nil")
	}

	defer a.Close()

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
	parsed, err := commands.ParsePrompt(trimmed)
	if err != nil {
		return err
	}
	toolReq := parsed.Tool
	if parsed.Kind == commands.KindTool {
		toolReq.CallID = ensureToolCallID(toolReq.CallID, a.nextID)
	}

	if err := a.messages.Append(a.sessionID, domain.TranscriptEntry{
		ID:        a.nextID("user"),
		Kind:      domain.TranscriptUser,
		Message:   trimmed,
		CreatedAt: a.now(),
		UpdatedAt: a.now(),
	}); err != nil {
		return err
	}
	switch parsed.Kind {
	case commands.KindPlan:
		return a.coordinator.PlanPrompt(a.sessionID, parsed.Text)
	case commands.KindRun:
		return a.coordinator.RunPlan(ctx, a.sessionID, parsed.Text)
	case commands.KindTool:
		if err := a.permissions.CheckTool(toolReq.Name); err != nil {
			a.bus.Publish(events.EventStatusUpdated, "Tool denied")
			return err
		}

		requiresApproval := a.permissions.RequiresApproval(toolReq.Name)
		if requiresApproval && (toolReq.Name == "shell" || toolReq.Name == "shell_bg") && shell.IsSafeReadOnly(toolReq.Command) {
			requiresApproval = false
		}

		if requiresApproval {
			approval := domain.ApprovalRequest{
				ID:          a.nextID("approval"),
				ToolCallID:  toolReq.CallID,
				Action:      toolReq.Name,
				Description: prompt,
				CreatedAt:   a.now(),
			}
			a.pendingMu.Lock()
			a.pending[approval.ID] = toolReq
			a.pendingMu.Unlock()
			a.bus.Publish(events.EventApprovalRequested, approval)
			a.bus.Publish(events.EventStatusUpdated, "Approval required")
			return nil
		}

		return a.executeTool(ctx, toolReq)
	}
	return a.coordinator.RunNaturalPrompt(ctx, a.sessionID, trimmed)
}

func (a *App) IsSessionBusy(sessionID string) bool {
	return a.coordinator.IsSessionBusy(sessionID)
}

func (a *App) QueuedPrompts(sessionID string) int {
	return a.coordinator.QueuedPrompts(sessionID)
}

func (a *App) CancelSession(sessionID string) bool {
	return a.coordinator.CancelSession(sessionID)
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
	_, err := a.toolExecutor.ExecuteRequest(ctx, a.sessionID, request)
	return err
}

func (a *App) publishTranscript(entry domain.TranscriptEntry) {
	a.appendTranscript(a.sessionID, entry)
}

func (a *App) appendTranscript(sessionID string, entry domain.TranscriptEntry) {
	logServiceFailure("append transcript", a.messages.Append(sessionID, entry))
}

func (a *App) updateTranscript(sessionID string, entry domain.TranscriptEntry) {
	logServiceFailure("update transcript", a.messages.Update(sessionID, entry))
}

func (a *App) replaceTranscript(sessionID string, entries []domain.TranscriptEntry) {
	logServiceFailure("replace transcript", a.messages.Replace(sessionID, entries))
}

func (a *App) setTasks(sessionID string, tasks []domain.Task) {
	logServiceFailure("set tasks", a.taskState.Set(sessionID, tasks))
}

func (a *App) nextID(prefix string) string {
	sequence := a.approvalSeed.Add(1)
	return fmt.Sprintf("%s-%d", prefix, sequence)
}

func (a *App) runInteractive(ctx context.Context) error {
	model := tui.NewApp(
		tui.WithSubmitHandler(func(prompt string) {
			go func() {
				if err := a.HandlePrompt(ctx, prompt); err != nil {
					a.publishSystemMessage("Request failed: " + err.Error())
					a.bus.Publish(events.EventStatusUpdated, "Request failed")
				}
			}()
		}),
		tui.WithApprovalHandler(func(request domain.ApprovalRequest, approved bool) {
			go func() {
				if err := a.ResolveApproval(ctx, request.ID, approved); err != nil {
					a.publishSystemMessage("Approval resolution failed: " + err.Error())
					a.bus.Publish(events.EventStatusUpdated, "Approval resolution failed")
				}
			}()
		}),
		tui.WithSessionSwitchHandler(func(id string) {
			if err := a.switchSession(id); err != nil {
				a.publishSystemMessage("Session switch failed: " + err.Error())
				a.bus.Publish(events.EventStatusUpdated, "Session switch failed")
			}
		}),
		tui.WithModelSwitchHandler(func(prov, mdl string) {
			if err := a.switchModel(provider.RoleOrchestrator, prov, mdl); err != nil {
				a.publishSystemMessage("Model switch failed: " + err.Error())
				return
			}
			a.bus.Publish(events.EventStatusUpdated, "Switched model: "+prov+"/"+mdl)
		}),
	)

	var err error
	model, err = a.bootstrapInteractiveModel(model)
	if err != nil {
		return err
	}

	program := tea.NewProgram(model, tea.WithAltScreen())
	unsubscribers := []func(){
		a.bus.Subscribe(events.EventTranscriptAdded, func(event events.Event) {
			entry, ok := event.Payload.(domain.TranscriptEntry)
			if ok {
				program.Send(tui.AddTranscriptMsg{Entry: entry})
			}
		}),
		a.bus.Subscribe(events.EventTranscriptUpdated, func(event events.Event) {
			entry, ok := event.Payload.(domain.TranscriptEntry)
			if ok {
				program.Send(tui.UpdateTranscriptMsg{Entry: entry})
			}
		}),
		a.bus.Subscribe(events.EventTranscriptReset, func(event events.Event) {
			entries, ok := event.Payload.([]domain.TranscriptEntry)
			if ok {
				program.Send(tui.SetTranscriptMsg{Entries: entries})
			}
		}),
		a.bus.Subscribe(events.EventStatusUpdated, func(event events.Event) {
			status, ok := event.Payload.(string)
			if ok {
				program.Send(tui.SetStatusMsg{Status: status})
			}
		}),
		a.bus.Subscribe(events.EventApprovalRequested, func(event events.Event) {
			request, ok := event.Payload.(domain.ApprovalRequest)
			if ok {
				program.Send(tui.ShowApprovalMsg{Request: request})
			}
		}),
		a.bus.Subscribe(events.EventTasksUpdated, func(event events.Event) {
			tasks, ok := event.Payload.([]domain.Task)
			if ok {
				program.Send(tui.SetTasksMsg{Tasks: tasks})
			}
		}),
		a.bus.Subscribe(events.EventApprovalCleared, func(event events.Event) {
			program.Send(tui.ClearApprovalMsg{})
		}),
		a.bus.Subscribe(runtime.EventToolStarted, func(event events.Event) {
			if entry, ok := activityEntryFromEvent(event); ok {
				program.Send(tui.AddActivityMsg{Entry: entry})
			}
		}),
		a.bus.Subscribe(runtime.EventToolCompleted, func(event events.Event) {
			if entry, ok := activityEntryFromEvent(event); ok {
				program.Send(tui.AddActivityMsg{Entry: entry})
			}
		}),
		a.bus.Subscribe(runtime.EventToolFailed, func(event events.Event) {
			if entry, ok := activityEntryFromEvent(event); ok {
				program.Send(tui.AddActivityMsg{Entry: entry})
			}
		}),
		a.bus.Subscribe(events.EventApprovalRequested, func(event events.Event) {
			if entry, ok := activityEntryFromEvent(event); ok {
				program.Send(tui.AddActivityMsg{Entry: entry})
			}
		}),
		a.bus.Subscribe(events.EventApprovalCleared, func(event events.Event) {
			if entry, ok := activityEntryFromEvent(event); ok {
				program.Send(tui.AddActivityMsg{Entry: entry})
			}
		}),
		a.bus.Subscribe(events.EventSessionChanged, func(event events.Event) {
			if id, ok := event.Payload.(string); ok {
				program.Send(tui.SetSessionTitleMsg{Title: id})
			}
		}),
		a.bus.Subscribe(events.EventTokenUsage, func(event events.Event) {
			if payload, ok := event.Payload.(map[string]any); ok {
				if totalIn, ok := payload["total_input_tokens"].(int64); ok {
					totalOut, _ := payload["total_output_tokens"].(int64)
					totalCost, _ := payload["total_cost"].(float64)
					priced, _ := payload["priced_responses"].(int64)
					unpriced, _ := payload["unpriced_responses"].(int64)
					program.Send(tui.SetTokenUsageMsg{InputTokens: totalIn, OutputTokens: totalOut, TotalCost: totalCost, PricedResponses: priced, UnpricedResponses: unpriced, Replace: true})
					return
				}
				if totalIn, ok := payload["total_input_tokens"].(int); ok {
					totalOut, _ := payload["total_output_tokens"].(int)
					totalCost, _ := payload["total_cost"].(float64)
					priced, _ := payload["priced_responses"].(int)
					unpriced, _ := payload["unpriced_responses"].(int)
					program.Send(tui.SetTokenUsageMsg{InputTokens: int64(totalIn), OutputTokens: int64(totalOut), TotalCost: totalCost, PricedResponses: int64(priced), UnpricedResponses: int64(unpriced), Replace: true})
					return
				}
				in, _ := payload["input_tokens"].(int)
				out, _ := payload["output_tokens"].(int)
				estimatedCost, _ := payload["estimated_cost"].(float64)
				pricingKnown, _ := payload["pricing_known"].(bool)
				message := tui.SetTokenUsageMsg{InputTokens: int64(in), OutputTokens: int64(out), TotalCost: estimatedCost}
				if pricingKnown {
					message.PricedResponses = 1
				} else {
					message.UnpricedResponses = 1
				}
				program.Send(message)
			}
		}),
		a.bus.Subscribe(events.EventStreamDelta, func(event events.Event) {
			if text, ok := event.Payload.(string); ok {
				program.Send(tui.StreamDeltaMsg{Text: text})
			}
		}),
		a.bus.Subscribe(events.EventSessionBusy, func(event events.Event) {
			busy, ok := event.Payload.(bool)
			if ok {
				program.Send(tui.SetBusyMsg(busy))
			}
		}),
	}
	defer func() {
		for _, unsubscribe := range unsubscribers {
			unsubscribe()
		}
	}()
	_, err = program.Run()
	if err != nil {
		return apperrors.Wrap("app.runInteractive", apperrors.CodeStartupFailed, err, "run Bubble Tea program")
	}

	return nil
}

func (a *App) publishSystemMessage(message string) {
	logServiceFailure("publish system message", a.messages.Append(a.sessionID, domain.TranscriptEntry{
		ID:        a.nextID("system"),
		Kind:      domain.TranscriptSystem,
		Message:   message,
		CreatedAt: a.now(),
	}))
}

func (a *App) switchModel(role provider.Role, providerID, modelID string) error {
	next := a.config
	var selected config.ProviderConfig

	switch role {
	case provider.RoleOrchestrator:
		next.Models.Orchestrator.Provider = providerID
		next.Models.Orchestrator.Model = modelID
		selected = next.Models.Orchestrator
	case provider.RoleWorker:
		next.Models.Worker.Provider = providerID
		next.Models.Worker.Model = modelID
		selected = next.Models.Worker
	default:
		return apperrors.New("app.switchModel", apperrors.CodeInvalidArgument, "unknown provider role")
	}

	newRouter, err := provider.NewRouter(next.Models)
	if err != nil {
		return err
	}

	store, err := config.OpenStore(a.config.WorkspaceRoot)
	if err != nil {
		return err
	}
	scope, err := store.ScopeForRole(string(role))
	if err != nil {
		return err
	}
	if err := store.UpdatePreferredModel(scope, string(role), selected); err != nil {
		return err
	}

	a.config = next
	a.providers = newRouter
	return nil
}

func (a *App) String() string {
	return fmt.Sprintf("App(env=%s, workspace=%s)", a.config.Env, a.config.WorkspaceRoot)
}

// Close releases resources held by the application (e.g. log files, sessions).
func (a *App) Close() {
	if a.sessions != nil {
		_ = a.sessions.SaveAll()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	shell.GetBackgroundJobManager().KillAll(ctx)
	if a.logCleanup != nil {
		a.logCleanup()
	}
}

func (a *App) bootstrapInteractiveModel(model tui.App) (tui.App, error) {
	sess, ok := a.messages.Session(a.sessionID)
	if !ok {
		return tui.App{}, apperrors.New("app.bootstrapInteractiveModel", apperrors.CodeStartupFailed, "active session not found")
	}

	if len(sess.Transcript) == 0 {
		entry := domain.TranscriptEntry{
			ID:        a.nextID("system"),
			Kind:      domain.TranscriptSystem,
			Message:   interactiveReadyMessage,
			CreatedAt: a.now(),
		}
		if err := a.messages.Append(a.sessionID, entry); err != nil {
			return tui.App{}, apperrors.Wrap("app.bootstrapInteractiveModel", apperrors.CodeStartupFailed, err, "seed interactive transcript")
		}
		sess, _ = a.messages.Session(a.sessionID)
	}

	updatedModel, _ := model.Update(tui.SetTranscriptMsg{Entries: sess.Transcript})
	hydratedModel, ok := updatedModel.(tui.App)
	if !ok {
		return tui.App{}, apperrors.New("app.bootstrapInteractiveModel", apperrors.CodeStartupFailed, "unexpected Bubble Tea model type after transcript update")
	}

	updatedModel, _ = hydratedModel.Update(tui.SetTasksMsg{Tasks: sess.Tasks})
	hydratedModel, ok = updatedModel.(tui.App)
	if !ok {
		return tui.App{}, apperrors.New("app.bootstrapInteractiveModel", apperrors.CodeStartupFailed, "unexpected Bubble Tea model type after task update")
	}

	updatedModel, _ = hydratedModel.Update(tui.SetSessionTitleMsg{Title: sess.ID})
	hydratedModel, ok = updatedModel.(tui.App)
	if !ok {
		return tui.App{}, apperrors.New("app.bootstrapInteractiveModel", apperrors.CodeStartupFailed, "unexpected Bubble Tea model type after session title update")
	}

	updatedModel, _ = hydratedModel.Update(tui.SetTokenUsageMsg{InputTokens: sess.Usage.InputTokens, OutputTokens: sess.Usage.OutputTokens, TotalCost: sess.Usage.TotalCost, PricedResponses: sess.Usage.PricedResponses, UnpricedResponses: sess.Usage.UnpricedResponses, Replace: true})
	hydratedModel, ok = updatedModel.(tui.App)
	if !ok {
		return tui.App{}, apperrors.New("app.bootstrapInteractiveModel", apperrors.CodeStartupFailed, "unexpected Bubble Tea model type after usage update")
	}

	updatedModel, _ = hydratedModel.Update(tui.SetStatusMsg{Status: "Interactive mode ready"})
	hydratedModel, ok = updatedModel.(tui.App)
	if !ok {
		return tui.App{}, apperrors.New("app.bootstrapInteractiveModel", apperrors.CodeStartupFailed, "unexpected Bubble Tea model type after status update")
	}

	return hydratedModel, nil
}

func (a *App) switchSession(id string) error {
	sess, ok := a.messages.Session(id)
	if !ok {
		return apperrors.New("app.switchSession", apperrors.CodeInvalidArgument, "session not found")
	}

	a.sessionID = id

	a.bus.Publish(events.EventSessionChanged, sess.ID)
	a.bus.Publish(events.EventTranscriptReset, sess.Transcript)
	a.bus.Publish(events.EventTasksUpdated, append([]domain.Task(nil), sess.Tasks...))
	a.bus.Publish(events.EventTokenUsage, map[string]any{
		"total_input_tokens":  sess.Usage.InputTokens,
		"total_output_tokens": sess.Usage.OutputTokens,
		"response_count":      sess.Usage.ResponseCount,
		"priced_responses":    sess.Usage.PricedResponses,
		"unpriced_responses":  sess.Usage.UnpricedResponses,
		"total_cost":          sess.Usage.TotalCost,
	})
	a.bus.Publish(events.EventStatusUpdated, "Switched to session: "+id)
	return nil
}

func activityEntryFromEvent(event events.Event) (tui.ActivityEntry, bool) {
	entry := tui.ActivityEntry{CreatedAt: event.Timestamp, Level: tui.ActivityInfo}

	switch event.Name {
	case runtime.EventToolStarted:
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			return tui.ActivityEntry{}, false
		}
		entry.Level = tui.ActivityPending
		entry.Label = "Tool running"
		entry.Detail = stringValue(payload["tool"])
		return entry, true
	case runtime.EventToolCompleted:
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			return tui.ActivityEntry{}, false
		}
		entry.Level = tui.ActivitySuccess
		entry.Label = "Tool completed"
		entry.Detail = strings.TrimSpace(strings.Join([]string{stringValue(payload["tool"]), firstNonEmpty(stringValue(payload["path"]), stringValue(payload["job_id"]))}, " "))
		return entry, true
	case runtime.EventToolFailed:
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			return tui.ActivityEntry{}, false
		}
		entry.Level = tui.ActivityError
		entry.Label = "Tool failed"
		entry.Detail = strings.TrimSpace(strings.Join([]string{stringValue(payload["tool"]), stringValue(payload["error"])}, " "))
		return entry, true
	case events.EventApprovalRequested:
		payload, ok := event.Payload.(domain.ApprovalRequest)
		if !ok {
			return tui.ActivityEntry{}, false
		}
		entry.Level = tui.ActivityPending
		entry.Label = "Approval requested"
		entry.Detail = payload.Action
		return entry, true
	case events.EventApprovalCleared:
		entry.Level = tui.ActivityInfo
		entry.Label = "Approval resolved"
		entry.Detail = stringValue(event.Payload)
		return entry, true
	default:
		return tui.ActivityEntry{}, false
	}
}

func stringValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
