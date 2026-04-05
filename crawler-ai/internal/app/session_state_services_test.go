package app

import (
	"path/filepath"
	"testing"
	"time"

	"crawler-ai/internal/domain"
	"crawler-ai/internal/events"
	"crawler-ai/internal/runtime"
	"crawler-ai/internal/session"
)

func TestSessionMessageServiceBuildProviderHistorySkipsEmptyAssistantEntries(t *testing.T) {
	t.Parallel()

	sessions := session.NewManager()
	if _, err := sessions.Create("session-1", "/workspace"); err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	service := newSessionMessageService(sessions, events.NewBus(), func() string { return "session-1" })
	if err := service.Append("session-1", domain.TranscriptEntry{ID: "user-1", Kind: domain.TranscriptUser, Message: "hello", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("Append(user) error: %v", err)
	}
	if err := service.Append("session-1", domain.TranscriptEntry{ID: "assistant-empty", Kind: domain.TranscriptAssistant, Message: "", CreatedAt: time.Now().UTC(), Metadata: map[string]string{"finish_reason": "error"}}); err != nil {
		t.Fatalf("Append(empty assistant) error: %v", err)
	}

	history, _, err := service.BuildProviderHistory("session-1", "")
	if err != nil {
		t.Fatalf("BuildProviderHistory() error: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected empty assistant entry to be omitted, got %#v", history)
	}
	if history[0].Role != "user" || history[0].Content != "hello" {
		t.Fatalf("unexpected history entry: %#v", history[0])
	}
}

func TestSessionMessageServicePersistsTranscriptAndPublishesOnlyForActiveSession(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	sessions := session.NewManager()
	sessions.SetDataDir(dataDir)
	if _, err := sessions.Create("active", "/workspace"); err != nil {
		t.Fatalf("Create(active) error: %v", err)
	}
	if _, err := sessions.Create("inactive", "/workspace"); err != nil {
		t.Fatalf("Create(inactive) error: %v", err)
	}

	bus := events.NewBus()
	transcriptEvents := make(chan domain.TranscriptEntry, 2)
	bus.Subscribe(events.EventTranscriptAdded, func(event events.Event) {
		if entry, ok := event.Payload.(domain.TranscriptEntry); ok {
			transcriptEvents <- entry
		}
	})

	service := newSessionMessageService(sessions, bus, func() string { return "active" })
	activeEntry := domain.TranscriptEntry{ID: "assistant-active", Kind: domain.TranscriptAssistant, Message: "active message", CreatedAt: time.Date(2026, 4, 4, 13, 0, 0, 0, time.UTC)}
	if err := service.Append("active", activeEntry); err != nil {
		t.Fatalf("Append(active) error: %v", err)
	}

	select {
	case published := <-transcriptEvents:
		if published.ID != activeEntry.ID || published.Message != activeEntry.Message {
			t.Fatalf("unexpected published transcript entry: %#v", published)
		}
	default:
		t.Fatal("expected active-session transcript event")
	}

	inactiveEntry := domain.TranscriptEntry{ID: "assistant-inactive", Kind: domain.TranscriptAssistant, Message: "inactive message", CreatedAt: time.Date(2026, 4, 4, 13, 0, 1, 0, time.UTC)}
	if err := service.Append("inactive", inactiveEntry); err != nil {
		t.Fatalf("Append(inactive) error: %v", err)
	}
	select {
	case published := <-transcriptEvents:
		t.Fatalf("did not expect inactive-session transcript event, got %#v", published)
	default:
	}

	reloaded := session.NewManager()
	reloaded.SetDataDir(dataDir)
	activeMessages, err := reloaded.ReadPersistedMessages("active")
	if err != nil {
		t.Fatalf("ReadPersistedMessages(active) error: %v", err)
	}
	inactiveMessages, err := reloaded.ReadPersistedMessages("inactive")
	if err != nil {
		t.Fatalf("ReadPersistedMessages(inactive) error: %v", err)
	}
	if len(activeMessages) != 1 || activeMessages[0].ID != activeEntry.ID {
		t.Fatalf("expected persisted active message, got %#v", activeMessages)
	}
	if len(inactiveMessages) != 1 || inactiveMessages[0].ID != inactiveEntry.ID {
		t.Fatalf("expected persisted inactive message, got %#v", inactiveMessages)
	}
}

func TestSessionTaskServicePersistsTasksAndPublishesOnlyForActiveSession(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	sessions := session.NewManager()
	sessions.SetDataDir(dataDir)
	if _, err := sessions.Create("active", "/workspace"); err != nil {
		t.Fatalf("Create(active) error: %v", err)
	}
	if _, err := sessions.Create("inactive", "/workspace"); err != nil {
		t.Fatalf("Create(inactive) error: %v", err)
	}

	bus := events.NewBus()
	taskEvents := make(chan []domain.Task, 2)
	bus.Subscribe(events.EventTasksUpdated, func(event events.Event) {
		if tasks, ok := event.Payload.([]domain.Task); ok {
			taskEvents <- append([]domain.Task(nil), tasks...)
		}
	})

	service := newSessionTaskService(sessions, bus, func() string { return "active" })
	activeTasks := []domain.Task{{ID: "task-active", Title: "Active task", Status: domain.TaskRunning}}
	if err := service.Set("active", activeTasks); err != nil {
		t.Fatalf("Set(active) error: %v", err)
	}
	select {
	case published := <-taskEvents:
		if len(published) != 1 || published[0].ID != "task-active" {
			t.Fatalf("unexpected active task event: %#v", published)
		}
	default:
		t.Fatal("expected active-session task event")
	}

	inactiveTasks := []domain.Task{{ID: "task-inactive", Title: "Inactive task", Status: domain.TaskCompleted}}
	if err := service.Set("inactive", inactiveTasks); err != nil {
		t.Fatalf("Set(inactive) error: %v", err)
	}
	select {
	case published := <-taskEvents:
		t.Fatalf("did not expect inactive-session task event, got %#v", published)
	default:
	}

	reloaded := session.NewManager()
	reloaded.SetDataDir(dataDir)
	persistedActive, err := reloaded.ReadPersistedTasks("active")
	if err != nil {
		t.Fatalf("ReadPersistedTasks(active) error: %v", err)
	}
	persistedInactive, err := reloaded.ReadPersistedTasks("inactive")
	if err != nil {
		t.Fatalf("ReadPersistedTasks(inactive) error: %v", err)
	}
	if len(persistedActive) != 1 || persistedActive[0].ID != "task-active" {
		t.Fatalf("expected persisted active tasks, got %#v", persistedActive)
	}
	if len(persistedInactive) != 1 || persistedInactive[0].ID != "task-inactive" {
		t.Fatalf("expected persisted inactive tasks, got %#v", persistedInactive)
	}
}

func TestSessionUsageServicePersistsUsageTotals(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	sessions := session.NewManager()
	sessions.SetDataDir(dataDir)
	if _, err := sessions.Create("usage-session", "/workspace"); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	service := newSessionUsageService(sessions)
	totals, err := service.Record("usage-session", session.UsageUpdate{InputTokens: 5, OutputTokens: 7, TotalCost: 0.02, PricingKnown: true, Provider: "openai", Model: "gpt-4o-mini"})
	if err != nil {
		t.Fatalf("Record() error: %v", err)
	}
	if totals.InputTokens != 5 || totals.OutputTokens != 7 || totals.TotalCost != 0.02 {
		t.Fatalf("unexpected in-memory totals: %#v", totals)
	}

	reloaded := session.NewManager()
	reloaded.SetDataDir(dataDir)
	persisted, err := reloaded.ReadPersistedUsage("usage-session")
	if err != nil {
		t.Fatalf("ReadPersistedUsage() error: %v", err)
	}
	if persisted.InputTokens != 5 || persisted.OutputTokens != 7 || persisted.TotalCost != 0.02 || persisted.LastProvider != "openai" {
		t.Fatalf("unexpected persisted usage totals: %#v", persisted)
	}
}

func TestSessionFileTrackingServicePersistsWorkspaceAndHistoryRecords(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	sessions := session.NewManager()
	sessions.SetDataDir(dataDir)
	if _, err := sessions.Create("file-session", "/workspace"); err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	service := newSessionFileTrackingService(sessions)
	result := runtime.ToolResult{
		Tool:   "edit_file",
		Path:   filepath.ToSlash(filepath.Join("notes", "todo.txt")),
		Output: "updated",
		Extra: map[string]string{
			"history_path": filepath.ToSlash(filepath.Join(".crawler-ai", "history", "notes_todo.txt.bak")),
		},
	}
	seed := 0
	if err := service.TrackToolResult("file-session", "edit_file", result, func(prefix string) string {
		seed++
		return prefix + "-" + string(rune('0'+seed))
	}, func() time.Time {
		return time.Date(2026, 4, 4, 14, 0, 0, 0, time.UTC)
	}); err != nil {
		t.Fatalf("TrackToolResult() error: %v", err)
	}

	reloaded := session.NewManager()
	reloaded.SetDataDir(dataDir)
	files, err := reloaded.ReadPersistedFiles("file-session")
	if err != nil {
		t.Fatalf("ReadPersistedFiles() error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected two persisted file records, got %#v", files)
	}
	if files[0].Kind != session.FileRecordWorkspace || files[0].Path != filepath.ToSlash(filepath.Join("notes", "todo.txt")) {
		t.Fatalf("expected workspace file record first, got %#v", files[0])
	}
	if files[1].Kind != session.FileRecordHistory || files[1].Metadata["source_path"] != filepath.ToSlash(filepath.Join("notes", "todo.txt")) {
		t.Fatalf("expected history snapshot metadata, got %#v", files[1])
	}
}
