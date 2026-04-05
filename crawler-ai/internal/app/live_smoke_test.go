package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crawler-ai/internal/config"
	"crawler-ai/internal/domain"
	"crawler-ai/internal/events"
	"crawler-ai/internal/oauth"
	"crawler-ai/internal/orchestrator"
	"crawler-ai/internal/provider"
	"crawler-ai/internal/session"
)

func TestLiveOpenAISmokeLowToken(t *testing.T) {
	if os.Getenv("CRAWLER_AI_LIVE_SMOKE") != "1" {
		t.Skip("set CRAWLER_AI_LIVE_SMOKE=1 to run live smoke tests")
	}

	store := oauth.DefaultKeyStore()
	_ = store.Load()
	apiKey := strings.TrimSpace(oauth.ResolveProviderKey(store, "openai", os.Getenv("OPENAI_API_KEY")))
	if apiKey == "" {
		t.Skip("OpenAI API key is not configured in env or key store")
	}

	modelID := strings.TrimSpace(os.Getenv("CRAWLER_AI_LIVE_MODEL"))
	if modelID == "" {
		modelID = "gpt-4o-mini"
	}
	baseURL := strings.TrimSpace(os.Getenv("CRAWLER_AI_LIVE_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	workspaceDir := t.TempDir()
	dataDir := t.TempDir()
	bus := events.NewBus()
	sessions := session.NewManager()
	sessions.SetDataDir(dataDir)
	if _, err := sessions.Create("session-1", workspaceDir); err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	stateServices := newSessionStateServices(sessions, bus, nil)

	cfg := config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: workspaceDir,
		Models: config.ModelConfig{
			Orchestrator: config.ProviderConfig{
				Provider: "openai",
				Model:    modelID,
				BaseURL:  baseURL,
				APIKey:   apiKey,
			},
			Worker: config.DefaultModelConfig().Worker,
		},
	}

	router, err := provider.NewRouter(cfg.Models)
	if err != nil {
		t.Fatalf("NewRouter() error: %v", err)
	}

	coordinator := newSessionCoordinator(sessionCoordinatorOptions{
		messages: stateServices.messages,
		tasks:    stateServices.tasks,
		usage:    stateServices.usage,
		lineage:  newSessionLineageService(sessions),
		providers: func() providerResolver {
			return router
		},
		models:  func() config.ModelConfig { return cfg.Models },
		bus:     bus,
		loop:    newSessionLoop(),
		planner: orchestrator.NewPlanner(),
		now: func() time.Time {
			return time.Now().UTC()
		},
		nextID: func(prefix string) string {
			return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
		},
		status: func(string) {},
	})

	selected, err := router.ForRole(provider.RoleOrchestrator)
	if err != nil {
		t.Fatalf("ForRole() error: %v", err)
	}

	var usagePayload map[string]any
	bus.Subscribe(events.EventTokenUsage, func(event events.Event) {
		if payload, ok := event.Payload.(map[string]any); ok {
			usagePayload = payload
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	response, err := coordinator.runProviderRequest(ctx, "session-1", "", selected, provider.Request{
		Model:        modelID,
		Messages:     []provider.Message{{Role: "user", Content: "Reply with exactly OK."}},
		MaxTokens:    16,
		Temperature:  0,
		SystemPrompt: "Respond with the shortest possible plain-text answer.",
	}, map[string]string{
		"provider": selected.Name(),
		"model":    modelID,
		"role":     string(provider.RoleOrchestrator),
	})
	if err != nil {
		t.Fatalf("runProviderRequest() error: %s", formatLiveProviderError(err))
	}

	trimmed := strings.TrimSpace(response.Text)
	if trimmed == "" {
		t.Fatalf("expected non-empty live response, got %#v", response)
	}
	upper := strings.ToUpper(strings.TrimSuffix(trimmed, "."))
	if upper != "OK" {
		t.Fatalf("expected short OK response, got %q", response.Text)
	}
	if response.Usage.InputTokens <= 0 || response.Usage.OutputTokens <= 0 {
		t.Fatalf("expected token usage in live response, got %#v", response.Usage)
	}
	if response.Usage.OutputTokens > 20 {
		t.Fatalf("expected bounded output usage, got %#v", response.Usage)
	}
	if usagePayload == nil {
		t.Fatal("expected token usage event payload")
	}
	if pricingKnown, _ := usagePayload["pricing_known"].(bool); !pricingKnown {
		t.Fatalf("expected pricing-backed estimate for %s, got %#v", modelID, usagePayload)
	}

	reloaded := session.NewManager()
	reloaded.SetDataDir(dataDir)
	if err := reloaded.LoadAll(); err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	loaded, ok := reloaded.Get("session-1")
	if !ok {
		t.Fatal("expected reloaded session after live smoke")
	}
	if loaded.Usage.ResponseCount != 1 || loaded.Usage.PricedResponses != 1 {
		t.Fatalf("expected persisted priced usage totals, got %#v", loaded.Usage)
	}
	if loaded.Usage.TotalCost <= 0 {
		t.Fatalf("expected positive estimated cost, got %#v", loaded.Usage)
	}
	if len(loaded.Transcript) == 0 {
		t.Fatalf("expected transcript persistence, got %#v", loaded)
	}
}

func TestLiveOpenAITicTacToeCommandSessionSmoke(t *testing.T) {
	if os.Getenv("CRAWLER_AI_LIVE_SMOKE") != "1" || os.Getenv("CRAWLER_AI_LIVE_TICTACTOE") != "1" {
		t.Skip("set CRAWLER_AI_LIVE_SMOKE=1 and CRAWLER_AI_LIVE_TICTACTOE=1 to run live tic-tac-toe smoke")
	}

	store := oauth.DefaultKeyStore()
	_ = store.Load()
	apiKey := strings.TrimSpace(oauth.ResolveProviderKey(store, "openai", os.Getenv("OPENAI_API_KEY")))
	if apiKey == "" {
		t.Skip("OpenAI API key is not configured in env or key store")
	}

	modelID := strings.TrimSpace(os.Getenv("CRAWLER_AI_LIVE_MODEL"))
	if modelID == "" {
		modelID = "gpt-4o-mini"
	}
	baseURL := strings.TrimSpace(os.Getenv("CRAWLER_AI_LIVE_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	workspaceDir := t.TempDir()
	dataDir := t.TempDir()
	application, err := New(config.Config{
		Env:           config.EnvTest,
		LogLevel:      "info",
		WorkspaceRoot: workspaceDir,
		Yolo:          true,
		Models: config.ModelConfig{
			Orchestrator: config.ProviderConfig{
				Provider: "openai",
				Model:    modelID,
				BaseURL:  baseURL,
				APIKey:   apiKey,
			},
			Worker: config.ProviderConfig{
				Provider: "openai",
				Model:    modelID,
				BaseURL:  baseURL,
				APIKey:   apiKey,
			},
		},
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer application.Close()
	application.sessions.SetDataDir(dataDir)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	prompt := "/run create a working browser tic-tac-toe app directly in the workspace using tools. Create README.md, index.html, style.css, and app.js. The app must be playable in the browser. Use tools to inspect the workspace and verify the result before finishing. Final summary must list the created files and confirm whether the app should run as a static browser app."
	if err := application.HandlePrompt(ctx, prompt); err != nil {
		t.Fatalf("HandlePrompt(run plan) error: %s", formatLiveProviderError(err))
	}
	rootSession, ok := application.sessions.Get(application.sessionID)
	if !ok {
		t.Fatal("expected live root session to exist")
	}
	if len(rootSession.Tasks) < 4 {
		t.Fatalf("expected planned task set after /run, got %#v", rootSession.Tasks)
	}
	for _, task := range rootSession.Tasks {
		if task.Status != domain.TaskCompleted {
			t.Fatalf("expected completed task after /run, got %#v", rootSession.Tasks)
		}
	}
	childSessions := 0
	for _, stored := range application.sessions.List() {
		if stored.ParentSessionID == application.sessionID {
			childSessions++
		}
	}
	if childSessions < 3 {
		t.Fatalf("expected child sessions from delegated run-plan execution, got %d", childSessions)
	}
	for _, required := range []string{"README.md", "index.html", "style.css", "app.js"} {
		if !taskOrSummaryMentions(rootSession.Tasks, required) {
			t.Fatalf("expected implementation task to mention %s, got %#v", required, rootSession.Tasks)
		}
	}
	for _, name := range []string{"README.md", "index.html", "style.css", "app.js"} {
		data, err := os.ReadFile(filepath.Join(workspaceDir, name))
		if err != nil {
			t.Fatalf("expected written file %s: %v", name, err)
		}
		if strings.TrimSpace(string(data)) == "" {
			t.Fatalf("expected non-empty file content for %s", name)
		}
	}
	initialTranscriptEntries := len(rootSession.Transcript)
	appJSPath := filepath.Join(workspaceDir, "app.js")
	styleCSSPath := filepath.Join(workspaceDir, "style.css")

	followUpCtx, followUpCancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer followUpCancel()
	followUpPrompt := "/run continue improving the existing tic-tac-toe app in the same workspace using tools. Make the layout clearly responsive on narrow screens and improve restart/reset behavior so the board, current player, and status text reset cleanly. Read the current files first, edit only what is needed, and verify the result before finishing. Final summary must mention the responsive changes and the reset behavior."
	if err := application.HandlePrompt(followUpCtx, followUpPrompt); err != nil {
		t.Fatalf("HandlePrompt(follow-up run plan) error: %s", formatLiveProviderError(err))
	}
	rootSession, ok = application.sessions.Get(application.sessionID)
	if !ok {
		t.Fatal("expected live root session to exist after follow-up")
	}
	if len(rootSession.Transcript) <= initialTranscriptEntries {
		t.Fatalf("expected transcript to grow after follow-up, got before=%d after=%d", initialTranscriptEntries, len(rootSession.Transcript))
	}
	appJS, err := os.ReadFile(appJSPath)
	if err != nil {
		t.Fatalf("expected app.js after follow-up run: %v", err)
	}
	if !containsAny(string(appJS), "restartButton.addEventListener", "restartButton.onclick") {
		t.Fatalf("expected restart button handler after follow-up, got %q", string(appJS))
	}
	if !containsAny(string(appJS), "restartGame", "resetGame", "statusDisplay") {
		t.Fatalf("expected explicit reset behavior in app.js after follow-up, got %q", string(appJS))
	}
	styleCSS, err := os.ReadFile(styleCSSPath)
	if err != nil {
		t.Fatalf("expected style.css after follow-up run: %v", err)
	}
	if !containsAny(string(styleCSS), "@media", "max-width", "min(") {
		t.Fatalf("expected responsive styling after follow-up, got %q", string(styleCSS))
	}
	contextService := session.NewCodingContextService(application.sessions, session.NewCommandWorkspaceDiagnosticsProvider())
	snapshot, err := contextService.Snapshot(application.sessionID)
	if err != nil {
		t.Fatalf("CodingContextService.Snapshot() error: %v", err)
	}
	for _, diagnostic := range snapshot.Diagnostics {
		if strings.EqualFold(strings.TrimSpace(diagnostic.Severity), "error") {
			t.Fatalf("expected generated tic-tac-toe repo to pass coding-context diagnostics, got %#v", snapshot.Diagnostics)
		}
	}

	reloaded := session.NewManager()
	reloaded.SetDataDir(dataDir)
	if err := reloaded.LoadAll(); err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	loaded, ok := reloaded.Get(application.sessionID)
	if !ok {
		t.Fatal("expected live session after reload")
	}
	if len(loaded.Transcript) < 6 {
		t.Fatalf("expected persisted transcript activity from live smoke, got %#v", loaded.Transcript)
	}
	trackedFiles := 0
	for _, stored := range reloaded.List() {
		trackedFiles += len(stored.Files)
	}
	if trackedFiles < 4 {
		t.Fatalf("expected tracked files from autonomous live smoke, got %d across %#v", trackedFiles, reloaded.List())
	}
}

func formatLiveProviderError(err error) string {
	if err == nil {
		return ""
	}
	var statusErr *provider.StatusError
	if errors.As(err, &statusErr) {
		return fmt.Sprintf("%s\noperation=%s\nstatus=%d\nbody=%q", statusErr.Error(), statusErr.Operation, statusErr.StatusCode, statusErr.Body)
	}
	return err.Error()
}

func taskOrSummaryMentions(tasks []domain.Task, target string) bool {
	for _, task := range tasks {
		if strings.Contains(task.Result, target) {
			return true
		}
	}
	return false
}

func containsAny(value string, targets ...string) bool {
	for _, target := range targets {
		if strings.Contains(value, target) {
			return true
		}
	}
	return false
}
