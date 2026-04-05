package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"crawler-ai/internal/config"
	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/events"
	"crawler-ai/internal/orchestrator"
	"crawler-ai/internal/provider"
	"crawler-ai/internal/providercatalog"
	"crawler-ai/internal/session"
)

const (
	maxContextTranscriptEntries = 24
	summaryTailEntries          = 8
	maxQueuedPromptsPerSession  = 32
	transientRetryAttempts      = 3
	maxPlanIterations           = 32
	repeatedPromptLoopThreshold = 4
	maxAutonomousToolRounds     = 20
	autonomousLoopWindowSize    = 3
	autonomousLoopMaxRepeats    = 2
	autonomousMalformedRepeats  = 2
	autonomousRetryMaxTokens    = 512
	autonomousRepairMaxTokens   = 256
	maxProviderAttemptTimeout   = 90 * time.Second
	providerAttemptReserve      = 15 * time.Second
)

type providerResolver interface {
	ForRole(role provider.Role) (provider.Provider, error)
}

type codingContextPromptService interface {
	Prompt(sessionID string) (string, error)
}

type sessionCoordinatorOptions struct {
	messages  messageStateService
	tasks     taskStateService
	usage     usageStateService
	tools     providerToolService
	lineage   sessionLineageService
	context   codingContextPromptService
	providers func() providerResolver
	models    func() config.ModelConfig
	bus       *events.Bus
	loop      *sessionLoop
	planner   *orchestrator.Planner
	now       func() time.Time
	nextID    func(string) string
	status    func(string)
}

type sessionCoordinator struct {
	messages  messageStateService
	tasks     taskStateService
	usage     usageStateService
	tools     providerToolService
	lineage   sessionLineageService
	context   codingContextPromptService
	providers func() providerResolver
	models    func() config.ModelConfig
	bus       *events.Bus
	loop      *sessionLoop
	planner   *orchestrator.Planner
	now       func() time.Time
	nextID    func(string) string
	status    func(string)
}

func newSessionCoordinator(opts sessionCoordinatorOptions) *sessionCoordinator {
	coordinator := &sessionCoordinator{
		messages:  opts.messages,
		tasks:     opts.tasks,
		usage:     opts.usage,
		tools:     opts.tools,
		lineage:   opts.lineage,
		context:   opts.context,
		providers: opts.providers,
		models:    opts.models,
		bus:       opts.bus,
		loop:      opts.loop,
		planner:   opts.planner,
		now:       opts.now,
		nextID:    opts.nextID,
		status:    opts.status,
	}
	if coordinator.bus == nil {
		coordinator.bus = events.NewBus()
	}
	if coordinator.loop == nil {
		coordinator.loop = newSessionLoop()
	}
	if coordinator.planner == nil {
		coordinator.planner = orchestrator.NewPlanner()
	}
	if coordinator.now == nil {
		coordinator.now = func() time.Time { return time.Now().UTC() }
	}
	if coordinator.status == nil {
		coordinator.status = func(string) {}
	}
	if coordinator.context == nil {
		coordinator.context = noopCodingContextPromptService{}
	}
	if coordinator.messages == nil {
		panic("app.newSessionCoordinator requires a message state service")
	}
	if coordinator.tasks == nil {
		panic("app.newSessionCoordinator requires a task state service")
	}
	if coordinator.usage == nil {
		panic("app.newSessionCoordinator requires a usage state service")
	}
	if coordinator.lineage == nil {
		panic("app.newSessionCoordinator requires a session lineage service")
	}
	return coordinator
}

func (c *sessionCoordinator) RunNaturalPrompt(ctx context.Context, sessionID, prompt string) error {
	if strings.TrimSpace(prompt) == "" {
		return apperrors.New("app.sessionCoordinator.RunNaturalPrompt", apperrors.CodeInvalidArgument, "prompt must not be empty")
	}
	if c.loop.Queued(sessionID) >= maxQueuedPromptsPerSession {
		return apperrors.New("app.sessionCoordinator.RunNaturalPrompt", apperrors.CodeInvalidArgument, "session prompt queue is full")
	}
	if err := c.messages.DetectRepeatedPromptLoop(sessionID, prompt); err != nil {
		return err
	}
	return c.loop.Run(sessionID, ctx, func(runCtx context.Context) error {
		c.bus.Publish(events.EventSessionBusy, true)
		defer c.bus.Publish(events.EventSessionBusy, false)
		return c.executeNaturalPrompt(runCtx, sessionID)
	})
}

func (c *sessionCoordinator) PlanPrompt(sessionID, prompt string) error {
	tasks := c.planner.Build(prompt)
	if err := c.tasks.Set(sessionID, tasks); err != nil {
		return err
	}
	if err := c.messages.Append(sessionID, domain.TranscriptEntry{
		ID:        c.nextID("system"),
		Kind:      domain.TranscriptSystem,
		Message:   fmt.Sprintf("Plan created with %d tasks.", len(tasks)),
		CreatedAt: c.now(),
		UpdatedAt: c.now(),
	}); err != nil {
		return err
	}
	c.status("Plan created")
	return nil
}

func (c *sessionCoordinator) RunPlan(ctx context.Context, sessionID, prompt string) error {
	if c.loop.Queued(sessionID) >= maxQueuedPromptsPerSession {
		return apperrors.New("app.sessionCoordinator.RunPlan", apperrors.CodeInvalidArgument, "session prompt queue is full")
	}
	return c.loop.Run(sessionID, ctx, func(runCtx context.Context) error {
		c.bus.Publish(events.EventSessionBusy, true)
		defer c.bus.Publish(events.EventSessionBusy, false)
		return c.executePlan(runCtx, sessionID, prompt)
	})
}

func (c *sessionCoordinator) IsSessionBusy(sessionID string) bool {
	return c.loop.IsBusy(sessionID)
}

func (c *sessionCoordinator) QueuedPrompts(sessionID string) int {
	return c.loop.Queued(sessionID)
}

func (c *sessionCoordinator) CancelSession(sessionID string) bool {
	return c.loop.Cancel(sessionID)
}

func (c *sessionCoordinator) executeNaturalPrompt(ctx context.Context, sessionID string) error {
	if err := c.messages.CompactIfNeeded(sessionID, c.nextID, c.now, c.status); err != nil {
		return err
	}
	history, systemPrompt, err := c.messages.BuildProviderHistory(sessionID, "")
	if err != nil {
		return err
	}
	contextPrompt, err := c.codingContextPrompt(sessionID)
	if err != nil {
		return err
	}
	models := c.models()
	selected, err := c.providers().ForRole(provider.RoleOrchestrator)
	if err != nil {
		return err
	}
	_, err = c.runProviderRequest(ctx, sessionID, "", selected, provider.Request{
		Model:        models.Orchestrator.Model,
		Messages:     history,
		Tools:        c.toolDefinitions(),
		SystemPrompt: joinPrompts(systemPrompt, contextPrompt, naturalPromptToolSystemPrompt()),
		MaxTokens:    1024,
		Temperature:  0.2,
	}, map[string]string{
		"provider": selected.Name(),
		"model":    models.Orchestrator.Model,
		"role":     string(provider.RoleOrchestrator),
	})
	if err != nil {
		return err
	}
	c.status("Prompt completed")
	return nil
}

func (c *sessionCoordinator) executePlan(ctx context.Context, sessionID, prompt string) error {
	queue := orchestrator.NewQueue(c.planner.Build(prompt))
	if err := c.tasks.Set(sessionID, queue.List()); err != nil {
		return err
	}
	iterations := 0

	for {
		if iterations >= maxPlanIterations {
			return apperrors.New("app.sessionCoordinator.executePlan", apperrors.CodeToolFailed, "plan aborted because it exceeded the maximum iteration limit")
		}
		iterations++

		task, ok := queue.NextReady()
		if !ok {
			break
		}
		if err := queue.Start(task.ID); err != nil {
			return err
		}
		if err := c.tasks.Set(sessionID, queue.List()); err != nil {
			return err
		}
		c.status("Running task: " + task.Title)

		result, err := c.executePlannedTask(ctx, sessionID, task)
		if err != nil {
			_ = queue.Fail(task.ID, err.Error())
			_ = c.tasks.Set(sessionID, queue.List())
			return err
		}

		if err := queue.Complete(task.ID, result); err != nil {
			return err
		}
		if err := c.tasks.Set(sessionID, queue.List()); err != nil {
			return err
		}
	}

	if err := c.messages.Append(sessionID, domain.TranscriptEntry{
		ID:        c.nextID("system"),
		Kind:      domain.TranscriptSystem,
		Message:   "Planned run completed.",
		CreatedAt: c.now(),
		UpdatedAt: c.now(),
	}); err != nil {
		return err
	}
	c.status("Planned run completed")
	return nil
}

func (c *sessionCoordinator) executePlannedTask(ctx context.Context, sessionID string, task domain.Task) (string, error) {
	childSessionID := c.nextID("task-session")
	childSession, err := c.lineage.CreateChild(sessionID, childSessionID)
	if err != nil {
		return "", err
	}
	if err := c.messages.Append(childSession.ID, domain.TranscriptEntry{
		ID:        c.nextID("system"),
		Kind:      domain.TranscriptSystem,
		Message:   fmt.Sprintf("Delegated task session for parent %s and task %s.", sessionID, task.Title),
		CreatedAt: c.now(),
		UpdatedAt: c.now(),
		Metadata: map[string]string{
			"parent_session_id": sessionID,
			"task_id":           task.ID,
		},
	}); err != nil {
		return "", err
	}
	if err := c.messages.Append(childSession.ID, domain.TranscriptEntry{
		ID:        c.nextID("user"),
		Kind:      domain.TranscriptUser,
		Message:   task.Description,
		CreatedAt: c.now(),
		UpdatedAt: c.now(),
		Metadata: map[string]string{
			"parent_session_id": sessionID,
			"task_id":           task.ID,
		},
	}); err != nil {
		return "", err
	}

	models := c.models()
	role := provider.RoleWorker
	modelID := models.Worker.Model
	if task.Assignee == domain.RoleOrchestrator {
		role = provider.RoleOrchestrator
		modelID = models.Orchestrator.Model
	}
	selected, err := c.providers().ForRole(role)
	if err != nil {
		return "", err
	}
	if err := c.messages.CompactIfNeeded(sessionID, c.nextID, c.now, c.status); err != nil {
		return "", err
	}
	history, systemPrompt, err := c.messages.BuildProviderHistory(sessionID, task.Description)
	if err != nil {
		return "", err
	}
	contextPrompt, err := c.codingContextPrompt(sessionID)
	if err != nil {
		return "", err
	}
	response, err := c.runProviderRequest(ctx, childSession.ID, sessionID, selected, provider.Request{
		Model:        modelID,
		Messages:     history,
		Tools:        c.toolDefinitions(),
		SystemPrompt: joinPrompts(systemPrompt, contextPrompt, autonomousToolSystemPrompt(task), fmt.Sprintf("You are acting as the %s role for task %s.", task.Assignee, task.Title)),
		MaxTokens:    1024,
		Temperature:  0.2,
	}, map[string]string{
		"task_id":    task.ID,
		"task_title": task.Title,
		"provider":   selected.Name(),
		"model":      modelID,
		"role":       string(task.Assignee),
	})
	if err != nil {
		return "", err
	}
	if err := c.messages.Append(sessionID, domain.TranscriptEntry{
		ID:        c.nextID("system"),
		Kind:      domain.TranscriptSystem,
		Message:   fmt.Sprintf("Task %q completed in child session %s.", task.Title, childSession.ID),
		CreatedAt: c.now(),
		UpdatedAt: c.now(),
		Metadata: map[string]string{
			"child_session_id": childSession.ID,
			"task_id":          task.ID,
		},
	}); err != nil {
		return "", err
	}
	return response.Text, nil
}

func (c *sessionCoordinator) compactTranscriptIfNeeded(sessionID string) error {
	return c.messages.CompactIfNeeded(sessionID, c.nextID, c.now, c.status)
}

func (c *sessionCoordinator) buildHistory(sessionID string, extraUser string) ([]provider.Message, string, error) {
	return c.messages.BuildProviderHistory(sessionID, extraUser)
}

func (c *sessionCoordinator) codingContextPrompt(sessionID string) (string, error) {
	if c == nil || c.context == nil {
		return "", nil
	}
	prompt, err := c.context.Prompt(sessionID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(prompt), nil
}

func (c *sessionCoordinator) runProviderRequest(ctx context.Context, transcriptSessionID, aggregateUsageSessionID string, selected provider.Provider, request provider.Request, metadata map[string]string) (provider.Response, error) {
	working := cloneProviderRequest(request)
	if len(working.Tools) == 0 {
		working.Tools = c.toolDefinitions()
	}
	roundSignatures := make([]string, 0, maxAutonomousToolRounds)
	lastMalformedSignature := ""
	malformedRepeats := 0
	previousRoundAllErrors := false
	for round := 0; round < maxAutonomousToolRounds; round++ {
		working.MaxTokens = autonomousRoundMaxTokens(request.MaxTokens, round, previousRoundAllErrors)
		response, err := c.runProviderTurn(ctx, transcriptSessionID, aggregateUsageSessionID, selected, working, metadata)
		if err != nil {
			return response, err
		}
		toolCalls := finishedToolCalls(response.ContentBlocks())
		if c.tools == nil || len(toolCalls) == 0 {
			return response, nil
		}
		working.Messages = append(working.Messages, provider.Message{Role: "assistant", Content: response.Text, ContentBlocks: response.ContentBlocks()})
		roundResults := make([]provider.ToolResult, 0, len(toolCalls))
		roundAllErrors := true
		for _, call := range toolCalls {
			result, execErr := c.tools.ExecuteToolCall(ctx, transcriptSessionID, call)
			if execErr != nil {
				return response, execErr
			}
			roundResults = append(roundResults, result)
			if !result.IsError {
				roundAllErrors = false
			}
			working.Messages = append(working.Messages, provider.Message{Role: "tool", Content: result.Content, ContentBlocks: []provider.ContentBlock{provider.ToolResultBlock(result)}, ToolCallID: result.ToolCallID})
		}
		roundSignature := autonomousInteractionSignature(toolCalls, roundResults)
		if roundContainsOnlySchemaValidationErrors(roundResults) {
			if roundSignature != "" && roundSignature == lastMalformedSignature {
				malformedRepeats++
			} else {
				malformedRepeats = 1
			}
			if roundSignature != "" && malformedRepeats > autonomousMalformedRepeats {
				return provider.Response{}, apperrors.New("app.sessionCoordinator.runProviderRequest", apperrors.CodeToolFailed, "autonomous tool loop aborted after repeated malformed tool calls")
			}
			lastMalformedSignature = roundSignature
		} else {
			lastMalformedSignature = ""
			malformedRepeats = 0
		}
		if roundSignature != "" {
			roundSignatures = append(roundSignatures, roundSignature)
			if hasRepeatedAutonomousInteractions(roundSignatures, autonomousLoopWindowSize, autonomousLoopMaxRepeats) {
				return provider.Response{}, apperrors.New("app.sessionCoordinator.runProviderRequest", apperrors.CodeToolFailed, "autonomous tool loop aborted after repeated tool interactions")
			}
		}
		previousRoundAllErrors = roundAllErrors
	}
	return provider.Response{}, apperrors.New("app.sessionCoordinator.runProviderRequest", apperrors.CodeToolFailed, "autonomous tool loop exceeded maximum rounds")
}

func (c *sessionCoordinator) runProviderTurn(ctx context.Context, transcriptSessionID, aggregateUsageSessionID string, selected provider.Provider, request provider.Request, metadata map[string]string) (provider.Response, error) {
	entry := domain.TranscriptEntry{
		ID:        c.nextID("assistant"),
		Kind:      domain.TranscriptAssistant,
		CreatedAt: c.now(),
		UpdatedAt: c.now(),
		Metadata:  cloneMetadata(metadata),
	}
	if entry.Metadata == nil {
		entry.Metadata = make(map[string]string)
	}
	entry.Metadata["status"] = "in_progress"
	entry.Metadata[domain.TranscriptMetadataResponseID] = entry.ID
	if err := c.messages.Append(transcriptSessionID, entry); err != nil {
		return provider.Response{}, err
	}

	var reasoningEntry *domain.TranscriptEntry
	ensureReasoningEntry := func() *domain.TranscriptEntry {
		if reasoningEntry != nil {
			return reasoningEntry
		}
		created := domain.TranscriptEntry{
			ID:        c.nextID("reasoning"),
			Kind:      domain.TranscriptReasoning,
			CreatedAt: c.now(),
			UpdatedAt: c.now(),
			Metadata:  cloneMetadata(entry.Metadata),
		}
		if created.Metadata == nil {
			created.Metadata = make(map[string]string)
		}
		created.Metadata["status"] = "in_progress"
		if err := c.messages.Append(transcriptSessionID, created); err != nil {
			logServiceFailure("append reasoning transcript", err)
		}
		reasoningEntry = &created
		return reasoningEntry
	}
	upsertEntry := func(entry domain.TranscriptEntry) {
		logServiceFailure("upsert provider transcript", c.messages.Upsert(transcriptSessionID, entry))
	}
	applyContent := func(blocks []provider.ContentBlock, replace bool) bool {
		updated := false
		for _, block := range blocks {
			switch block.Kind {
			case provider.ContentBlockText:
				if block.Text == "" {
					continue
				}
				if replace {
					entry.Message = block.Text
				} else {
					entry.Message += block.Text
				}
				entry.UpdatedAt = c.now()
				logServiceFailure("update assistant transcript", c.messages.Update(transcriptSessionID, entry))
				updated = true
			case provider.ContentBlockReasoning:
				if block.Text == "" {
					continue
				}
				reasoningRecord := ensureReasoningEntry()
				if replace {
					reasoningRecord.Message = block.Text
				} else {
					reasoningRecord.Message += block.Text
				}
				reasoningRecord.UpdatedAt = c.now()
				logServiceFailure("update reasoning transcript", c.messages.Update(transcriptSessionID, *reasoningRecord))
				updated = true
			case provider.ContentBlockToolCall:
				if block.ToolCall == nil || strings.TrimSpace(block.ToolCall.ID) == "" {
					continue
				}
				toolEntry := newProviderToolTranscriptEntry(*block.ToolCall, c.now(), entry.Metadata)
				upsertEntry(toolEntry)
				updated = true
			case provider.ContentBlockToolResult:
				if block.ToolResult == nil || strings.TrimSpace(block.ToolResult.ToolCallID) == "" {
					continue
				}
				toolEntry := newProviderToolTranscriptEntry(provider.ToolCall{
					ID:       block.ToolResult.ToolCallID,
					Name:     block.ToolResult.Name,
					Finished: true,
				}, c.now(), entry.Metadata)
				toolEntry = applyToolTranscriptResult(toolEntry, block.ToolResult.Content, map[bool]string{true: toolStatusFailed, false: toolStatusCompleted}[block.ToolResult.IsError], c.now())
				upsertEntry(toolEntry)
				updated = true
			}
		}
		return updated
	}

	var response provider.Response
	err := c.invokeWithRetry(ctx, func(callCtx context.Context) error {
		if streaming, ok := provider.SupportsStreaming(selected); ok {
			receivedChunk := false
			resp, runErr := streaming.CompleteStream(callCtx, request, func(chunk provider.StreamChunk) {
				if applyContent(chunk.ContentBlocks(), false) {
					receivedChunk = true
				}
				if chunk.FinishReason != "" {
					entry.Metadata["finish_reason"] = chunk.FinishReason
					if reasoningEntry != nil {
						reasoningEntry.Metadata["finish_reason"] = chunk.FinishReason
					}
				}
			})
			response = resp
			if runErr != nil && receivedChunk {
				return nonRetryableError{cause: runErr}
			}
			return runErr
		}

		resp, runErr := selected.Complete(callCtx, request)
		response = resp
		return runErr
	})

	delete(entry.Metadata, "status")
	entry.UpdatedAt = c.now()
	if reasoningEntry != nil {
		delete(reasoningEntry.Metadata, "status")
		reasoningEntry.UpdatedAt = c.now()
	}
	if err != nil {
		if wrapped, ok := err.(nonRetryableError); ok {
			err = wrapped.cause
		}
		if errors.Is(err, context.Canceled) {
			entry.Metadata["finish_reason"] = "canceled"
			if reasoningEntry != nil {
				reasoningEntry.Metadata["finish_reason"] = "canceled"
			}
		} else {
			entry.Metadata["finish_reason"] = "error"
			if reasoningEntry != nil {
				reasoningEntry.Metadata["finish_reason"] = "error"
			}
			if strings.TrimSpace(entry.Message) == "" {
				entry.Message = err.Error()
			}
		}
		if updateErr := c.messages.Update(transcriptSessionID, entry); updateErr != nil {
			logServiceFailure("update failed assistant transcript", updateErr)
		}
		if reasoningEntry != nil {
			if updateErr := c.messages.Update(transcriptSessionID, *reasoningEntry); updateErr != nil {
				logServiceFailure("update failed reasoning transcript", updateErr)
			}
		}
		return response, err
	}

	if blocks := response.ContentBlocks(); len(blocks) > 0 {
		applyContent(blocks, true)
	} else if response.Text != "" {
		entry.Message = response.Text
	}
	if response.FinishReason != "" {
		entry.Metadata["finish_reason"] = response.FinishReason
		if reasoningEntry != nil {
			reasoningEntry.Metadata["finish_reason"] = response.FinishReason
		}
	} else {
		entry.Metadata["finish_reason"] = "completed"
		if reasoningEntry != nil {
			reasoningEntry.Metadata["finish_reason"] = "completed"
		}
	}
	if err := c.messages.Update(transcriptSessionID, entry); err != nil {
		return response, err
	}
	if reasoningEntry != nil {
		if err := c.messages.Update(transcriptSessionID, *reasoningEntry); err != nil {
			return response, err
		}
	}
	estimatedCost, pricingKnown := providercatalog.EstimateCostUSD(selected.Name(), request.Model, response.Usage.InputTokens, response.Usage.OutputTokens)
	usageUpdate := session.UsageUpdate{
		InputTokens:  response.Usage.InputTokens,
		OutputTokens: response.Usage.OutputTokens,
		TotalCost:    estimatedCost,
		PricingKnown: pricingKnown,
		Provider:     selected.Name(),
		Model:        request.Model,
	}
	usageTotals, usageErr := c.usage.Record(transcriptSessionID, usageUpdate)
	aggregateTotals := usageTotals
	aggregateErr := usageErr
	if aggregateUsageSessionID != "" && aggregateUsageSessionID != transcriptSessionID {
		aggregateTotals, aggregateErr = c.usage.Record(aggregateUsageSessionID, usageUpdate)
	}
	payload := map[string]any{
		"input_tokens":   response.Usage.InputTokens,
		"output_tokens":  response.Usage.OutputTokens,
		"estimated_cost": estimatedCost,
		"pricing_known":  pricingKnown,
	}
	if aggregateErr == nil {
		payload["total_input_tokens"] = aggregateTotals.InputTokens
		payload["total_output_tokens"] = aggregateTotals.OutputTokens
		payload["response_count"] = aggregateTotals.ResponseCount
		payload["priced_responses"] = aggregateTotals.PricedResponses
		payload["unpriced_responses"] = aggregateTotals.UnpricedResponses
		payload["total_cost"] = aggregateTotals.TotalCost
	}
	c.bus.Publish(events.EventTokenUsage, payload)
	if usageErr != nil {
		return response, usageErr
	}
	if aggregateUsageSessionID != "" && aggregateUsageSessionID != transcriptSessionID && aggregateErr != nil {
		return response, aggregateErr
	}
	return response, nil
}

func (c *sessionCoordinator) toolDefinitions() []provider.ToolDefinition {
	if c == nil || c.tools == nil {
		return nil
	}
	return c.tools.Definitions()
}

func autonomousToolSystemPrompt(task domain.Task) string {
	base := fmt.Sprintf("Use the available tools to inspect and modify the workspace whenever task %q requires concrete file or shell work. Do not claim files were created, edited, or verified unless you actually used tools to do it.", task.Title)
	quality := toolQualitySystemPrompt()
	return joinPrompts(base, quality)
}

func naturalPromptToolSystemPrompt() string {
	base := "Use the available tools whenever the request requires reading files, writing files, editing code, running commands, or verifying results. Do not claim workspace changes or verification unless you actually used tools to perform them."
	return joinPrompts(base, toolQualitySystemPrompt())
}

func toolQualitySystemPrompt() string {
	return "When producing or editing code, prefer complete working implementations over placeholders. Keep files syntactically valid, update all related files consistently, and read back the key files after writing them. If HTML references a control, script, or style, make sure the related CSS and JavaScript are wired end-to-end rather than leaving dead elements. For browser apps, include basic responsive behavior and verify that event handlers and initialization code match the final HTML structure."
}

func cloneProviderRequest(request provider.Request) provider.Request {
	cloned := request
	cloned.Messages = append([]provider.Message(nil), request.Messages...)
	cloned.Tools = append([]provider.ToolDefinition(nil), request.Tools...)
	cloned.Headers = cloneMetadata(request.Headers)
	cloned.Body = cloneAnyMap(request.Body)
	cloned.ProviderOptions = cloneAnyMap(request.ProviderOptions)
	return cloned
}

func autonomousRoundMaxTokens(initial, round int, previousRoundAllErrors bool) int {
	if initial <= 0 {
		return initial
	}
	budget := initial
	if round > 0 && budget > autonomousRetryMaxTokens {
		budget = autonomousRetryMaxTokens
	}
	if previousRoundAllErrors && budget > autonomousRepairMaxTokens {
		budget = autonomousRepairMaxTokens
	}
	return budget
}

func hasRepeatedAutonomousInteractions(signatures []string, windowSize, maxRepeats int) bool {
	if len(signatures) < windowSize {
		return false
	}
	window := signatures[len(signatures)-windowSize:]
	counts := make(map[string]int, len(window))
	for _, signature := range window {
		if strings.TrimSpace(signature) == "" {
			continue
		}
		counts[signature]++
		if counts[signature] > maxRepeats {
			return true
		}
	}
	return false
}

func autonomousInteractionSignature(toolCalls []provider.ToolCall, results []provider.ToolResult) string {
	if len(toolCalls) == 0 {
		return ""
	}
	resultsByID := make(map[string]provider.ToolResult, len(results))
	for _, result := range results {
		resultsByID[result.ToolCallID] = result
	}
	h := sha256.New()
	for _, call := range toolCalls {
		result := resultsByID[call.ID]
		io.WriteString(h, call.Name)
		io.WriteString(h, "\x00")
		io.WriteString(h, call.Input)
		io.WriteString(h, "\x00")
		io.WriteString(h, result.Content)
		io.WriteString(h, "\x00")
		if result.IsError {
			io.WriteString(h, "1")
		} else {
			io.WriteString(h, "0")
		}
		io.WriteString(h, "\x00")
	}
	return hex.EncodeToString(h.Sum(nil))
}

func roundContainsOnlySchemaValidationErrors(results []provider.ToolResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, result := range results {
		if !result.IsError || !isSchemaValidationToolFailure(result.Content) {
			return false
		}
	}
	return true
}

func isSchemaValidationToolFailure(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	return strings.Contains(trimmed, "app.validateToolRequest:") || strings.Contains(trimmed, "app.runtimeToolRequestFromCall:")
}

func finishedToolCalls(blocks []provider.ContentBlock) []provider.ToolCall {
	seen := make(map[string]struct{})
	toolCalls := make([]provider.ToolCall, 0)
	for _, block := range blocks {
		if block.Kind != provider.ContentBlockToolCall || block.ToolCall == nil || !block.ToolCall.Finished {
			continue
		}
		if _, ok := seen[block.ToolCall.ID]; ok {
			continue
		}
		seen[block.ToolCall.ID] = struct{}{}
		toolCalls = append(toolCalls, *block.ToolCall)
	}
	return toolCalls
}

func (c *sessionCoordinator) invokeWithRetry(ctx context.Context, call func(context.Context) error) error {
	var lastErr error
	for attempt := 1; attempt <= transientRetryAttempts; attempt++ {
		attemptCtx, cancel := splitProviderAttemptContext(ctx)
		lastErr = call(attemptCtx)
		attemptTimedOut := ctx.Err() == nil && errors.Is(attemptCtx.Err(), context.DeadlineExceeded) && errors.Is(lastErr, context.DeadlineExceeded)
		cancel()
		if lastErr == nil {
			return nil
		}
		if wrapped, ok := lastErr.(nonRetryableError); ok {
			return wrapped
		}
		if attemptTimedOut {
			if attempt == transientRetryAttempts {
				return lastErr
			}
			c.status(fmt.Sprintf("Provider attempt timed out; retrying (%d/%d)", attempt, transientRetryAttempts))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
			}
			continue
		}
		if !isTransientProviderError(lastErr) || attempt == transientRetryAttempts {
			return lastErr
		}
		c.status(fmt.Sprintf("Transient provider failure; retrying (%d/%d)", attempt, transientRetryAttempts))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
		}
	}
	return lastErr
}

func splitProviderAttemptContext(parentCtx context.Context) (context.Context, context.CancelFunc) {
	if timeout, ok := providerAttemptTimeout(parentCtx); ok {
		return context.WithTimeout(parentCtx, timeout)
	}
	return context.WithCancel(parentCtx)
}

func providerAttemptTimeout(parentCtx context.Context) (time.Duration, bool) {
	if parentCtx == nil {
		return 0, false
	}
	if deadline, ok := parentCtx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, false
		}
		if remaining <= maxProviderAttemptTimeout+providerAttemptReserve {
			return 0, false
		}
		timeout := maxProviderAttemptTimeout
		if remaining-timeout < providerAttemptReserve {
			timeout = remaining - providerAttemptReserve
		}
		if timeout <= 0 {
			return 0, false
		}
		return timeout, true
	}
	return maxProviderAttemptTimeout, true
}

type nonRetryableError struct {
	cause error
}

func (e nonRetryableError) Error() string {
	if e.cause == nil {
		return "non-retryable provider error"
	}
	return e.cause.Error()
}

func summarizeTranscript(entries []domain.TranscriptEntry) string {
	var builder strings.Builder
	builder.WriteString("Session summary of earlier context:\n")
	for _, entry := range entries {
		if entry.Kind == domain.TranscriptReasoning {
			continue
		}
		role := strings.ToUpper(string(entry.Kind))
		text := strings.TrimSpace(entry.Message)
		if text == "" {
			continue
		}
		if len(text) > 180 {
			text = text[:180] + "..."
		}
		builder.WriteString("- ")
		builder.WriteString(role)
		builder.WriteString(": ")
		builder.WriteString(text)
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func joinPrompts(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	return strings.Join(filtered, "\n\n")
}

func cloneMetadata(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func cloneAnyMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneAnyValue(value)
	}
	return cloned
}

func cloneAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneAnyValue(item)
		}
		return cloned
	default:
		return typed
	}
}

func isTransientProviderError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var statusErr *provider.StatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode == 429 || statusErr.StatusCode >= 500
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	return false
}

type noopCodingContextPromptService struct{}

func (noopCodingContextPromptService) Prompt(string) (string, error) {
	return "", nil
}
