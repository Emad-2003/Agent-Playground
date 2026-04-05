package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"crawler-ai/internal/config"
	"crawler-ai/internal/domain"
	"crawler-ai/internal/events"
	"crawler-ai/internal/orchestrator"
	"crawler-ai/internal/provider"
	"crawler-ai/internal/session"
)

type providerResolverFunc func(role provider.Role) (provider.Provider, error)

func (f providerResolverFunc) ForRole(role provider.Role) (provider.Provider, error) {
	return f(role)
}

type coordinatorTestState struct {
	sessions *session.Manager
	bus      *events.Bus
	messages messageStateService
	tasks    taskStateService
	usage    usageStateService
	tools    providerToolService
	lineage  sessionLineageService
}

func newCoordinatorTestState() coordinatorTestState {
	sessions := session.NewManager()
	bus := events.NewBus()
	services := newSessionStateServices(sessions, bus, nil)
	return coordinatorTestState{
		sessions: sessions,
		bus:      bus,
		messages: services.messages,
		tasks:    services.tasks,
		usage:    services.usage,
		lineage:  newSessionLineageService(sessions),
		tools:    nil,
	}
}

func newTestCoordinator(state coordinatorTestState, resolver func() providerResolver, models func() config.ModelConfig, now func() time.Time, nextID func(string) string) *sessionCoordinator {
	return newTestCoordinatorWithContext(state, resolver, models, now, nextID, nil)
}

type diagnosticsProviderStub struct {
	diagnostics []session.CodingContextDiagnostic
	err         error
}

func (s diagnosticsProviderStub) Diagnostics(string, []string) (session.WorkspaceDiagnosticsResult, error) {
	if s.err != nil {
		return session.WorkspaceDiagnosticsResult{}, s.err
	}
	return session.WorkspaceDiagnosticsResult{Diagnostics: append([]session.CodingContextDiagnostic(nil), s.diagnostics...)}, nil
}

func newTestCoordinatorWithContext(state coordinatorTestState, resolver func() providerResolver, models func() config.ModelConfig, now func() time.Time, nextID func(string) string, context codingContextPromptService) *sessionCoordinator {
	return newSessionCoordinator(sessionCoordinatorOptions{
		messages:  state.messages,
		tasks:     state.tasks,
		usage:     state.usage,
		tools:     state.tools,
		lineage:   state.lineage,
		context:   context,
		providers: resolver,
		models:    models,
		bus:       state.bus,
		loop:      newSessionLoop(),
		planner:   orchestrator.NewPlanner(),
		now:       now,
		nextID:    nextID,
		status:    func(string) {},
	})
}

type providerToolServiceStub struct {
	definitions []provider.ToolDefinition
	execute     func(context.Context, string, provider.ToolCall) (provider.ToolResult, error)
}

func (s providerToolServiceStub) Definitions() []provider.ToolDefinition {
	return append([]provider.ToolDefinition(nil), s.definitions...)
}

func (s providerToolServiceStub) ExecuteToolCall(ctx context.Context, sessionID string, call provider.ToolCall) (provider.ToolResult, error) {
	if s.execute == nil {
		return provider.ToolResult{ToolCallID: call.ID, Name: call.Name, Content: "stub result"}, nil
	}
	return s.execute(ctx, sessionID, call)
}

func TestSessionCoordinatorInjectsCodingContextIntoSystemPrompt(t *testing.T) {
	state := newCoordinatorTestState()
	_, err := state.sessions.Create("session-1", t.TempDir())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := state.messages.Append("session-1", domain.TranscriptEntry{ID: "user-1", Kind: domain.TranscriptUser, Message: "build the game", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("messages.Append() error: %v", err)
	}
	if err := session.NewFileMutationService(state.sessions).TrackAll("session-1", []session.FileRecord{{ID: "workspace-1", Kind: session.FileRecordWorkspace, Path: "web/index.html", Tool: "write_file", CreatedAt: time.Date(2026, 4, 4, 15, 0, 0, 0, time.UTC)}}); err != nil {
		t.Fatalf("TrackAll() error: %v", err)
	}
	contextService := session.NewCodingContextService(state.sessions, diagnosticsProviderStub{diagnostics: []session.CodingContextDiagnostic{{Path: "web/index.html", Severity: "warning", Message: "missing aria-label", Line: 8}}})

	var captured provider.Request
	coordinator := newTestCoordinatorWithContext(state, func() providerResolver {
		return providerResolverFunc(func(role provider.Role) (provider.Provider, error) {
			return providerStub{name: "stub", complete: func(ctx context.Context, request provider.Request) (provider.Response, error) {
				captured = request
				return provider.Response{Text: "ok", FinishReason: "stop"}, nil
			}}, nil
		})
	}, func() config.ModelConfig { return config.DefaultModelConfig() }, time.Now, func(prefix string) string { return prefix + "-1" }, contextService)

	if err := coordinator.RunNaturalPrompt(context.Background(), "session-1", "build the game"); err != nil {
		t.Fatalf("RunNaturalPrompt() error: %v", err)
	}
	if !strings.Contains(captured.SystemPrompt, "Recent workspace files:") || !strings.Contains(captured.SystemPrompt, "web/index.html") {
		t.Fatalf("expected tracked file context in system prompt, got %q", captured.SystemPrompt)
	}
	if !strings.Contains(captured.SystemPrompt, "Workspace diagnostics:") || !strings.Contains(captured.SystemPrompt, "missing aria-label") {
		t.Fatalf("expected diagnostics context in system prompt, got %q", captured.SystemPrompt)
	}
}

type providerStub struct {
	name     string
	complete func(context.Context, provider.Request) (provider.Response, error)
}

type streamingProviderStub struct {
	name   string
	stream func(context.Context, provider.Request, func(provider.StreamChunk)) (provider.Response, error)
}

func (p providerStub) Name() string { return p.name }

func (p providerStub) Complete(ctx context.Context, request provider.Request) (provider.Response, error) {
	if p.complete == nil {
		return provider.Response{}, errors.New("complete not configured")
	}
	return p.complete(ctx, request)
}

func (p streamingProviderStub) Name() string { return p.name }

func (p streamingProviderStub) Complete(ctx context.Context, request provider.Request) (provider.Response, error) {
	return provider.Response{}, errors.New("complete not configured")
}

func (p streamingProviderStub) CompleteStream(ctx context.Context, request provider.Request, onChunk func(provider.StreamChunk)) (provider.Response, error) {
	if p.stream == nil {
		return provider.Response{}, errors.New("stream not configured")
	}
	return p.stream(ctx, request, onChunk)
}

func TestSessionCoordinatorPersistsStreamingIncrementally(t *testing.T) {
	state := newCoordinatorTestState()
	_, err := state.sessions.Create("session-1", t.TempDir())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := state.messages.Append("session-1", domain.TranscriptEntry{ID: "user-1", Kind: domain.TranscriptUser, Message: "hello", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("messages.Append() error: %v", err)
	}

	chunkSeen := make(chan struct{}, 1)
	coordinator := newTestCoordinator(state, func() providerResolver {
		return providerResolverFunc(func(role provider.Role) (provider.Provider, error) {
			return streamingProviderStub{
				name: "stub",
				stream: func(ctx context.Context, request provider.Request, onChunk func(provider.StreamChunk)) (provider.Response, error) {
					onChunk(provider.StreamChunk{Text: "hel"})
					chunkSeen <- struct{}{}
					<-ctx.Done()
					return provider.Response{}, ctx.Err()
				},
			}, nil
		})
	}, func() config.ModelConfig { return config.DefaultModelConfig() }, time.Now, func(prefix string) string {
		return prefix + "-1"
	})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- coordinator.RunNaturalPrompt(ctx, "session-1", "hello")
	}()

	select {
	case <-chunkSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first chunk")
	}

	loaded, _ := state.sessions.Get("session-1")
	if len(loaded.Transcript) < 2 {
		t.Fatalf("expected assistant entry after stream started, got %#v", loaded.Transcript)
	}
	assistant := loaded.Transcript[len(loaded.Transcript)-1]
	if assistant.Message != "hel" {
		t.Fatalf("expected partial transcript persistence, got %q", assistant.Message)
	}

	cancel()
	if !errors.Is(<-errCh, context.Canceled) {
		t.Fatal("expected canceled request to return context canceled")
	}
}

func TestSessionCoordinatorPersistsReasoningSeparately(t *testing.T) {
	state := newCoordinatorTestState()
	_, err := state.sessions.Create("session-1", t.TempDir())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := state.messages.Append("session-1", domain.TranscriptEntry{ID: "user-1", Kind: domain.TranscriptUser, Message: "hello", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("messages.Append() error: %v", err)
	}

	coordinator := newTestCoordinator(state, func() providerResolver {
		return providerResolverFunc(func(role provider.Role) (provider.Provider, error) {
			return streamingProviderStub{
				name: "stub",
				stream: func(ctx context.Context, request provider.Request, onChunk func(provider.StreamChunk)) (provider.Response, error) {
					onChunk(provider.StreamChunk{Content: []provider.ContentBlock{provider.ReasoningBlock("Inspecting repository state.")}})
					onChunk(provider.StreamChunk{Content: []provider.ContentBlock{provider.TextBlock("Done")}})
					return provider.Response{
						Text:         "Done",
						Reasoning:    "Inspecting repository state.",
						Content:      []provider.ContentBlock{provider.ReasoningBlock("Inspecting repository state."), provider.TextBlock("Done")},
						FinishReason: "stop",
					}, nil
				},
			}, nil
		})
	}, func() config.ModelConfig { return config.DefaultModelConfig() }, time.Now, func(prefix string) string { return prefix + "-1" })

	if err := coordinator.RunNaturalPrompt(context.Background(), "session-1", "hello"); err != nil {
		t.Fatalf("RunNaturalPrompt() error: %v", err)
	}

	loaded, _ := state.sessions.Get("session-1")
	if len(loaded.Transcript) != 3 {
		t.Fatalf("expected user, assistant, and reasoning entries; got %#v", loaded.Transcript)
	}
	if loaded.Transcript[1].Kind != domain.TranscriptAssistant || loaded.Transcript[1].Message != "Done" {
		t.Fatalf("unexpected assistant transcript entry: %#v", loaded.Transcript[1])
	}
	if loaded.Transcript[1].Metadata[domain.TranscriptMetadataResponseID] != loaded.Transcript[1].ID {
		t.Fatalf("expected assistant response metadata to reference assistant ID, got %#v", loaded.Transcript[1].Metadata)
	}
	if loaded.Transcript[2].Kind != domain.TranscriptReasoning || loaded.Transcript[2].Message != "Inspecting repository state." {
		t.Fatalf("unexpected reasoning transcript entry: %#v", loaded.Transcript[2])
	}
	if loaded.Transcript[2].Metadata[domain.TranscriptMetadataResponseID] != loaded.Transcript[1].ID {
		t.Fatalf("expected reasoning entry to share assistant response ID, got %#v", loaded.Transcript[2].Metadata)
	}
	if loaded.Transcript[2].Metadata["finish_reason"] != "stop" {
		t.Fatalf("expected reasoning finish reason to persist, got %#v", loaded.Transcript[2].Metadata)
	}
	if history, _, err := coordinator.buildHistory("session-1", ""); err != nil {
		t.Fatalf("buildHistory() error: %v", err)
	} else if len(history) != 2 {
		t.Fatalf("expected reasoning entry to be excluded from history, got %#v", history)
	}
}

func TestSessionCoordinatorPersistsToolCallLifecycle(t *testing.T) {
	state := newCoordinatorTestState()
	_, err := state.sessions.Create("session-1", t.TempDir())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := state.messages.Append("session-1", domain.TranscriptEntry{ID: "user-1", Kind: domain.TranscriptUser, Message: "hello", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("messages.Append() error: %v", err)
	}

	coordinator := newTestCoordinator(state, func() providerResolver {
		return providerResolverFunc(func(role provider.Role) (provider.Provider, error) {
			return streamingProviderStub{
				name: "stub",
				stream: func(ctx context.Context, request provider.Request, onChunk func(provider.StreamChunk)) (provider.Response, error) {
					onChunk(provider.StreamChunk{Content: []provider.ContentBlock{provider.ToolCallBlock(provider.ToolCall{ID: "call-1", Name: "grep", Input: "TODO", Finished: false})}})
					onChunk(provider.StreamChunk{Content: []provider.ContentBlock{provider.ToolResultBlock(provider.ToolResult{ToolCallID: "call-1", Name: "grep", Content: "README.md:1: TODO", IsError: false})}})
					return provider.Response{Content: []provider.ContentBlock{provider.ToolCallBlock(provider.ToolCall{ID: "call-1", Name: "grep", Input: "TODO", Finished: true}), provider.ToolResultBlock(provider.ToolResult{ToolCallID: "call-1", Name: "grep", Content: "README.md:1: TODO", IsError: false})}, FinishReason: "tool_use"}, nil
				},
			}, nil
		})
	}, func() config.ModelConfig { return config.DefaultModelConfig() }, time.Now, func(prefix string) string { return prefix + "-1" })

	if err := coordinator.RunNaturalPrompt(context.Background(), "session-1", "hello"); err != nil {
		t.Fatalf("RunNaturalPrompt() error: %v", err)
	}

	loaded, _ := state.sessions.Get("session-1")
	var toolEntry *domain.TranscriptEntry
	for index := range loaded.Transcript {
		if loaded.Transcript[index].Kind == domain.TranscriptTool {
			toolEntry = &loaded.Transcript[index]
			break
		}
	}
	if toolEntry == nil {
		t.Fatalf("expected persisted tool entry, got %#v", loaded.Transcript)
	}
	if toolEntry.ID != toolTranscriptEntryID("call-1") {
		t.Fatalf("unexpected tool transcript ID: %#v", toolEntry)
	}
	if toolEntry.Metadata["tool_call_id"] != "call-1" || toolEntry.Metadata["status"] != toolStatusCompleted {
		t.Fatalf("unexpected tool transcript metadata: %#v", toolEntry.Metadata)
	}
	if toolEntry.Metadata[domain.TranscriptMetadataResponseID] != "assistant-1" {
		t.Fatalf("expected tool transcript to share assistant response ID, got %#v", toolEntry.Metadata)
	}
	if !strings.Contains(toolEntry.Message, "Result:\nREADME.md:1: TODO") {
		t.Fatalf("expected tool result in transcript, got %q", toolEntry.Message)
	}
}

func TestSessionCoordinatorExecutesAutonomousToolLoop(t *testing.T) {
	state := newCoordinatorTestState()
	_, err := state.sessions.Create("session-1", t.TempDir())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := state.messages.Append("session-1", domain.TranscriptEntry{ID: "user-1", Kind: domain.TranscriptUser, Message: "build app", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("messages.Append() error: %v", err)
	}
	state.tools = providerToolServiceStub{
		definitions: []provider.ToolDefinition{{Name: "write_file", Description: "write file", Parameters: map[string]any{"type": "object"}}},
		execute: func(ctx context.Context, sessionID string, call provider.ToolCall) (provider.ToolResult, error) {
			if sessionID != "session-1" {
				t.Fatalf("unexpected session for tool execution: %s", sessionID)
			}
			if call.Name != "write_file" {
				t.Fatalf("unexpected tool call: %#v", call)
			}
			return provider.ToolResult{ToolCallID: call.ID, Name: call.Name, Content: "index.html\nwrote 12 bytes"}, nil
		},
	}

	callCount := 0
	coordinator := newTestCoordinator(state, func() providerResolver {
		return providerResolverFunc(func(role provider.Role) (provider.Provider, error) {
			return providerStub{name: "stub", complete: func(ctx context.Context, request provider.Request) (provider.Response, error) {
				callCount++
				if callCount == 1 {
					if len(request.Tools) == 0 {
						t.Fatalf("expected tools to be advertised on first request")
					}
					return provider.Response{Content: []provider.ContentBlock{provider.ToolCallBlock(provider.ToolCall{ID: "call-1", Name: "write_file", Input: `{"path":"index.html","content":"<html></html>"}`, Finished: true})}, FinishReason: "tool_calls"}, nil
				}
				if len(request.Messages) < 3 {
					t.Fatalf("expected assistant and tool follow-up messages, got %#v", request.Messages)
				}
				last := request.Messages[len(request.Messages)-1]
				if last.Role != "tool" || last.ToolCallID != "call-1" || !strings.Contains(last.Content, "wrote 12 bytes") {
					t.Fatalf("unexpected follow-up tool message: %#v", last)
				}
				return provider.Response{Text: "Created index.html successfully.", FinishReason: "stop"}, nil
			}}, nil
		})
	}, func() config.ModelConfig { return config.DefaultModelConfig() }, time.Now, func(prefix string) string { return prefix + "-1" })

	if err := coordinator.RunNaturalPrompt(context.Background(), "session-1", "build app"); err != nil {
		t.Fatalf("RunNaturalPrompt() error: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected two provider turns, got %d", callCount)
	}
}

func TestSessionCoordinatorReducesAutonomousFollowUpTokenBudget(t *testing.T) {
	state := newCoordinatorTestState()
	_, err := state.sessions.Create("session-1", t.TempDir())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := state.messages.Append("session-1", domain.TranscriptEntry{ID: "user-1", Kind: domain.TranscriptUser, Message: "build app", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("messages.Append() error: %v", err)
	}
	state.tools = providerToolServiceStub{
		definitions: []provider.ToolDefinition{{Name: "write_file", Description: "write file", Parameters: map[string]any{"type": "object"}}},
		execute: func(ctx context.Context, sessionID string, call provider.ToolCall) (provider.ToolResult, error) {
			return provider.ToolResult{ToolCallID: call.ID, Name: call.Name, Content: "index.html\nwrote 12 bytes"}, nil
		},
	}

	budgets := make([]int, 0, 2)
	callCount := 0
	coordinator := newTestCoordinator(state, func() providerResolver {
		return providerResolverFunc(func(role provider.Role) (provider.Provider, error) {
			return providerStub{name: "stub", complete: func(ctx context.Context, request provider.Request) (provider.Response, error) {
				callCount++
				budgets = append(budgets, request.MaxTokens)
				if callCount == 1 {
					return provider.Response{Content: []provider.ContentBlock{provider.ToolCallBlock(provider.ToolCall{ID: "call-1", Name: "write_file", Input: `{"path":"index.html","content":"<html></html>"}`, Finished: true})}, FinishReason: "tool_calls"}, nil
				}
				return provider.Response{Text: "Created index.html successfully.", FinishReason: "stop"}, nil
			}}, nil
		})
	}, func() config.ModelConfig { return config.DefaultModelConfig() }, time.Now, func(prefix string) string { return prefix + "-1" })

	if err := coordinator.RunNaturalPrompt(context.Background(), "session-1", "build app"); err != nil {
		t.Fatalf("RunNaturalPrompt() error: %v", err)
	}
	if len(budgets) != 2 {
		t.Fatalf("expected two provider budgets, got %#v", budgets)
	}
	if budgets[0] != 1024 {
		t.Fatalf("expected initial budget 1024, got %d", budgets[0])
	}
	if budgets[1] >= budgets[0] {
		t.Fatalf("expected follow-up budget reduction, got %#v", budgets)
	}
	if budgets[1] != autonomousRetryMaxTokens {
		t.Fatalf("expected follow-up budget %d, got %#v", autonomousRetryMaxTokens, budgets)
	}
}

func TestSessionCoordinatorStopsRepeatedMalformedToolCallsEarly(t *testing.T) {
	state := newCoordinatorTestState()
	_, err := state.sessions.Create("session-1", t.TempDir())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := state.messages.Append("session-1", domain.TranscriptEntry{ID: "user-1", Kind: domain.TranscriptUser, Message: "fix app", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("messages.Append() error: %v", err)
	}
	state.tools = providerToolServiceStub{
		definitions: []provider.ToolDefinition{{Name: "edit_file", Description: "edit file", Parameters: map[string]any{"type": "object"}}},
		execute: func(ctx context.Context, sessionID string, call provider.ToolCall) (provider.ToolResult, error) {
			return provider.ToolResult{
				ToolCallID: call.ID,
				Name:       call.Name,
				Content:    "app.validateToolRequest: edit_file requires path (invalid_argument)",
				IsError:    true,
			}, nil
		},
	}

	attempts := 0
	budgets := make([]int, 0, 2)
	coordinator := newTestCoordinator(state, func() providerResolver {
		return providerResolverFunc(func(role provider.Role) (provider.Provider, error) {
			return providerStub{name: "stub", complete: func(ctx context.Context, request provider.Request) (provider.Response, error) {
				attempts++
				budgets = append(budgets, request.MaxTokens)
				return provider.Response{Content: []provider.ContentBlock{provider.ToolCallBlock(provider.ToolCall{ID: fmt.Sprintf("call-%d", attempts), Name: "edit_file", Input: `{"old_text":"a","new_text":"b"}`, Finished: true})}, FinishReason: "tool_calls"}, nil
			}}, nil
		})
	}, func() config.ModelConfig { return config.DefaultModelConfig() }, time.Now, func(prefix string) string { return prefix + "-1" })

	err = coordinator.RunNaturalPrompt(context.Background(), "session-1", "fix app")
	if err == nil {
		t.Fatal("expected malformed autonomous loop to abort")
	}
	if !strings.Contains(err.Error(), "repeated malformed tool calls") {
		t.Fatalf("expected malformed tool call error, got %v", err)
	}
	if attempts != autonomousMalformedRepeats+1 {
		t.Fatalf("expected %d provider turns before abort, got %d", autonomousMalformedRepeats+1, attempts)
	}
	if len(budgets) != autonomousMalformedRepeats+1 || budgets[1] != autonomousRepairMaxTokens || budgets[2] != autonomousRepairMaxTokens {
		t.Fatalf("expected repair budget on second turn, got %#v", budgets)
	}
}

func TestSessionCoordinatorDetectsRepeatedToolInteractionLoops(t *testing.T) {
	state := newCoordinatorTestState()
	_, err := state.sessions.Create("session-1", t.TempDir())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := state.messages.Append("session-1", domain.TranscriptEntry{ID: "user-1", Kind: domain.TranscriptUser, Message: "search", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("messages.Append() error: %v", err)
	}
	state.tools = providerToolServiceStub{
		definitions: []provider.ToolDefinition{{Name: "grep", Description: "grep", Parameters: map[string]any{"type": "object"}}},
		execute: func(ctx context.Context, sessionID string, call provider.ToolCall) (provider.ToolResult, error) {
			return provider.ToolResult{ToolCallID: call.ID, Name: call.Name, Content: "README.md:1: TODO"}, nil
		},
	}

	attempts := 0
	coordinator := newTestCoordinator(state, func() providerResolver {
		return providerResolverFunc(func(role provider.Role) (provider.Provider, error) {
			return providerStub{name: "stub", complete: func(ctx context.Context, request provider.Request) (provider.Response, error) {
				attempts++
				return provider.Response{Content: []provider.ContentBlock{provider.ToolCallBlock(provider.ToolCall{ID: fmt.Sprintf("call-%d", attempts), Name: "grep", Input: `{"pattern":"TODO"}`, Finished: true})}, FinishReason: "tool_calls"}, nil
			}}, nil
		})
	}, func() config.ModelConfig { return config.DefaultModelConfig() }, time.Now, func(prefix string) string { return prefix + "-1" })

	err = coordinator.RunNaturalPrompt(context.Background(), "session-1", "search")
	if err == nil {
		t.Fatal("expected repeated tool interaction loop to abort")
	}
	if !strings.Contains(err.Error(), "repeated tool interactions") {
		t.Fatalf("expected repeated interaction error, got %v", err)
	}
	if attempts != autonomousLoopWindowSize {
		t.Fatalf("expected abort after %d repeated turns, got %d", autonomousLoopWindowSize, attempts)
	}
}

func TestSplitProviderAttemptContextSplitsLongSessionBudget(t *testing.T) {
	parentCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	parentDeadline, ok := parentCtx.Deadline()
	if !ok {
		t.Fatal("expected parent deadline")
	}

	childCtx, childCancel := splitProviderAttemptContext(parentCtx)
	defer childCancel()
	childDeadline, ok := childCtx.Deadline()
	if !ok {
		t.Fatal("expected child deadline")
	}
	if !childDeadline.Before(parentDeadline) {
		t.Fatalf("expected split child deadline before parent deadline, child=%v parent=%v", childDeadline, parentDeadline)
	}
	remaining := time.Until(childDeadline)
	if remaining <= 0 || remaining > maxProviderAttemptTimeout+5*time.Second {
		t.Fatalf("expected bounded child timeout around %s, got %s", maxProviderAttemptTimeout, remaining)
	}
}

func TestSplitProviderAttemptContextPreservesShortSessionBudget(t *testing.T) {
	parentCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	parentDeadline, ok := parentCtx.Deadline()
	if !ok {
		t.Fatal("expected parent deadline")
	}

	childCtx, childCancel := splitProviderAttemptContext(parentCtx)
	defer childCancel()
	childDeadline, ok := childCtx.Deadline()
	if !ok {
		t.Fatal("expected child deadline")
	}
	delta := childDeadline.Sub(parentDeadline)
	if delta < -time.Second || delta > time.Second {
		t.Fatalf("expected short session budget to stay intact, child=%v parent=%v delta=%s", childDeadline, parentDeadline, delta)
	}
}

func TestSessionCoordinatorRetriesTransientErrors(t *testing.T) {
	state := newCoordinatorTestState()
	_, _ = state.sessions.Create("session-1", t.TempDir())
	_ = state.messages.Append("session-1", domain.TranscriptEntry{ID: "user-1", Kind: domain.TranscriptUser, Message: "hello", CreatedAt: time.Now().UTC()})
	attempts := 0

	coordinator := newTestCoordinator(state, func() providerResolver {
		return providerResolverFunc(func(role provider.Role) (provider.Provider, error) {
			return providerStub{
				name: "stub",
				complete: func(ctx context.Context, request provider.Request) (provider.Response, error) {
					attempts++
					if attempts == 1 {
						return provider.Response{}, &provider.StatusError{StatusCode: 429, Message: "rate limited"}
					}
					return provider.Response{Text: "ok", FinishReason: "stop"}, nil
				},
			}, nil
		})
	}, func() config.ModelConfig { return config.DefaultModelConfig() }, time.Now, func(prefix string) string { return prefix + "-1" })

	if err := coordinator.RunNaturalPrompt(context.Background(), "session-1", "hello"); err != nil {
		t.Fatalf("RunNaturalPrompt() error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected one retry, got %d attempts", attempts)
	}
}

func TestSessionCoordinatorCompactsTranscript(t *testing.T) {
	state := newCoordinatorTestState()
	_, _ = state.sessions.Create("session-1", t.TempDir())
	for index := 0; index < maxContextTranscriptEntries+2; index++ {
		kind := domain.TranscriptUser
		if index%2 == 1 {
			kind = domain.TranscriptAssistant
		}
		_ = state.messages.Append("session-1", domain.TranscriptEntry{ID: "entry", Kind: kind, Message: "message"})
	}

	coordinator := newTestCoordinator(state, func() providerResolver {
		return providerResolverFunc(func(role provider.Role) (provider.Provider, error) {
			return providerStub{name: "stub", complete: func(ctx context.Context, request provider.Request) (provider.Response, error) {
				return provider.Response{Text: "ok", FinishReason: "stop"}, nil
			}}, nil
		})
	}, func() config.ModelConfig { return config.DefaultModelConfig() }, time.Now, func(prefix string) string { return prefix + "-1" })

	if err := coordinator.compactTranscriptIfNeeded("session-1"); err != nil {
		t.Fatalf("compactTranscriptIfNeeded() error: %v", err)
	}
	loaded, _ := state.sessions.Get("session-1")
	if len(loaded.Transcript) > summaryTailEntries+1 {
		t.Fatalf("expected compacted transcript, got %d entries", len(loaded.Transcript))
	}
	if loaded.Transcript[0].Metadata["summary"] != "true" {
		t.Fatalf("expected summary entry, got %#v", loaded.Transcript[0].Metadata)
	}
}

func TestSessionCoordinatorPersistsPricedUsageImmediately(t *testing.T) {
	dataDir := t.TempDir()
	state := newCoordinatorTestState()
	state.sessions.SetDataDir(dataDir)
	_, err := state.sessions.Create("session-1", t.TempDir())
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if err := state.messages.Append("session-1", domain.TranscriptEntry{ID: "user-1", Kind: domain.TranscriptUser, Message: "hello", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("messages.Append() error: %v", err)
	}

	var usagePayload map[string]any
	state.bus.Subscribe(events.EventTokenUsage, func(event events.Event) {
		if payload, ok := event.Payload.(map[string]any); ok {
			usagePayload = payload
		}
	})

	coordinator := newTestCoordinator(state, func() providerResolver {
		return providerResolverFunc(func(role provider.Role) (provider.Provider, error) {
			return providerStub{name: "openai", complete: func(ctx context.Context, request provider.Request) (provider.Response, error) {
				return provider.Response{Text: "ok", FinishReason: "stop", Usage: provider.Usage{InputTokens: 1000, OutputTokens: 2000}}, nil
			}}, nil
		})
	}, func() config.ModelConfig {
		return config.ModelConfig{Orchestrator: config.ProviderConfig{Provider: "openai", Model: "gpt-4o"}, Worker: config.DefaultModelConfig().Worker}
	}, time.Now, func(prefix string) string { return prefix + "-1" })

	if err := coordinator.RunNaturalPrompt(context.Background(), "session-1", "hello"); err != nil {
		t.Fatalf("RunNaturalPrompt() error: %v", err)
	}
	if usagePayload == nil {
		t.Fatal("expected token usage payload to be published")
	}
	if usagePayload["pricing_known"] != true {
		t.Fatalf("expected priced response payload, got %#v", usagePayload)
	}
	if usagePayload["priced_responses"] != int64(1) {
		t.Fatalf("expected priced response count in payload, got %#v", usagePayload)
	}

	reloaded := session.NewManager()
	reloaded.SetDataDir(dataDir)
	if err := reloaded.LoadAll(); err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}
	loaded, ok := reloaded.Get("session-1")
	if !ok {
		t.Fatal("expected session after reload")
	}
	if loaded.Usage.PricedResponses != 1 || loaded.Usage.TotalCost <= 0 {
		t.Fatalf("expected priced usage to persist immediately, got %#v", loaded.Usage)
	}
}
