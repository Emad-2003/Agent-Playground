package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/events"
	"crawler-ai/internal/logging"
	"crawler-ai/internal/provider"
	"crawler-ai/internal/runtime"
	"crawler-ai/internal/session"
)

type activeSessionResolver func() string

type messageStateService interface {
	Session(sessionID string) (session.Session, bool)
	Append(sessionID string, entry domain.TranscriptEntry) error
	Update(sessionID string, entry domain.TranscriptEntry) error
	Replace(sessionID string, entries []domain.TranscriptEntry) error
	Upsert(sessionID string, entry domain.TranscriptEntry) error
	CompactIfNeeded(sessionID string, nextID func(string) string, now func() time.Time, status func(string)) error
	BuildProviderHistory(sessionID, extraUser string) ([]provider.Message, string, error)
	DetectRepeatedPromptLoop(sessionID, prompt string) error
}

type taskStateService interface {
	Set(sessionID string, tasks []domain.Task) error
}

type usageStateService interface {
	Record(sessionID string, update session.UsageUpdate) (session.UsageTotals, error)
}

type fileTrackingStateService interface {
	TrackToolResult(sessionID, tool string, result runtime.ToolResult, nextID func(string) string, now func() time.Time) error
}

type sessionStateServices struct {
	messages    messageStateService
	tasks       taskStateService
	usage       usageStateService
	fileTracker fileTrackingStateService
}

type sessionMessageService struct {
	store     sessionMessageStore
	publisher transcriptEventPublisher
}

type sessionTaskService struct {
	store     sessionTaskStore
	publisher taskEventPublisher
}

type sessionUsageService struct {
	store sessionUsageStore
}

type sessionFileTrackingService struct {
	store sessionFileTrackingStore
}

type sessionMessageStore interface {
	Session(sessionID string) (session.Session, bool)
	Append(sessionID string, entry domain.TranscriptEntry) error
	Update(sessionID string, entry domain.TranscriptEntry) error
	Replace(sessionID string, entries []domain.TranscriptEntry) error
}

type sessionTaskStore interface {
	Set(sessionID string, tasks []domain.Task) error
}

type sessionUsageStore interface {
	Record(sessionID string, update session.UsageUpdate) (session.UsageTotals, error)
}

type sessionFileTrackingStore interface {
	TrackAll(sessionID string, files []session.FileRecord) error
}

func newSessionStateServices(sessions *session.Manager, bus *events.Bus, activeSession activeSessionResolver) sessionStateServices {
	return sessionStateServices{
		messages:    newSessionMessageService(sessions, bus, activeSession),
		tasks:       newSessionTaskService(sessions, bus, activeSession),
		usage:       newSessionUsageService(sessions),
		fileTracker: newSessionFileTrackingService(sessions),
	}
}

func newSessionMessageService(sessions *session.Manager, bus *events.Bus, activeSession activeSessionResolver) *sessionMessageService {
	return &sessionMessageService{
		store:     session.NewTranscriptMutationService(sessions),
		publisher: newActiveSessionEventPublisher(bus, activeSession),
	}
}

func newSessionTaskService(sessions *session.Manager, bus *events.Bus, activeSession activeSessionResolver) *sessionTaskService {
	return &sessionTaskService{
		store:     session.NewTaskMutationService(sessions),
		publisher: newActiveSessionEventPublisher(bus, activeSession),
	}
}

func newSessionUsageService(sessions *session.Manager) *sessionUsageService {
	return &sessionUsageService{store: session.NewUsageMutationService(sessions)}
}

func newSessionFileTrackingService(sessions *session.Manager) *sessionFileTrackingService {
	return &sessionFileTrackingService{store: session.NewFileMutationService(sessions)}
}

func (s *sessionMessageService) Session(sessionID string) (session.Session, bool) {
	if s == nil || s.store == nil {
		return session.Session{}, false
	}
	return s.store.Session(sessionID)
}

func (s *sessionMessageService) Append(sessionID string, entry domain.TranscriptEntry) error {
	if s == nil || s.store == nil {
		return apperrors.New("app.sessionMessageService.Append", apperrors.CodeStartupFailed, "session message service is not configured")
	}
	if err := s.store.Append(sessionID, entry); err != nil {
		return err
	}
	if s.publisher != nil {
		s.publisher.PublishTranscriptAdded(sessionID, entry)
	}
	return nil
}

func (s *sessionMessageService) Update(sessionID string, entry domain.TranscriptEntry) error {
	if s == nil || s.store == nil {
		return apperrors.New("app.sessionMessageService.Update", apperrors.CodeStartupFailed, "session message service is not configured")
	}
	if err := s.store.Update(sessionID, entry); err != nil {
		return err
	}
	if s.publisher != nil {
		s.publisher.PublishTranscriptUpdated(sessionID, entry)
	}
	return nil
}

func (s *sessionMessageService) Replace(sessionID string, entries []domain.TranscriptEntry) error {
	if s == nil || s.store == nil {
		return apperrors.New("app.sessionMessageService.Replace", apperrors.CodeStartupFailed, "session message service is not configured")
	}
	if err := s.store.Replace(sessionID, entries); err != nil {
		return err
	}
	if s.publisher != nil {
		s.publisher.PublishTranscriptReset(sessionID, entries)
	}
	return nil
}

func (s *sessionMessageService) Upsert(sessionID string, entry domain.TranscriptEntry) error {
	current, ok := s.Session(sessionID)
	if !ok {
		return apperrors.New("app.sessionMessageService.Upsert", apperrors.CodeInvalidArgument, "session not found")
	}
	for _, existing := range current.Transcript {
		if existing.ID == entry.ID {
			return s.Update(sessionID, entry)
		}
	}
	return s.Append(sessionID, entry)
}

func (s *sessionMessageService) CompactIfNeeded(sessionID string, nextID func(string) string, now func() time.Time, status func(string)) error {
	current, ok := s.Session(sessionID)
	if !ok {
		return apperrors.New("app.sessionMessageService.CompactIfNeeded", apperrors.CodeInvalidArgument, "session not found")
	}
	if len(current.Transcript) <= maxContextTranscriptEntries {
		return nil
	}

	cutoff := len(current.Transcript) - summaryTailEntries
	if cutoff <= 1 {
		return nil
	}
	summary := domain.TranscriptEntry{
		ID:        nextID("summary"),
		Kind:      domain.TranscriptSystem,
		Message:   summarizeTranscript(current.Transcript[:cutoff]),
		CreatedAt: now(),
		UpdatedAt: now(),
		Metadata: map[string]string{
			"summary":     "true",
			"entry_count": strconv.Itoa(cutoff),
		},
	}
	replacement := make([]domain.TranscriptEntry, 0, 1+len(current.Transcript[cutoff:]))
	replacement = append(replacement, summary)
	replacement = append(replacement, current.Transcript[cutoff:]...)
	if err := s.Replace(sessionID, replacement); err != nil {
		return err
	}
	if status != nil {
		status("Session context compacted")
	}
	return nil
}

func (s *sessionMessageService) BuildProviderHistory(sessionID, extraUser string) ([]provider.Message, string, error) {
	current, ok := s.Session(sessionID)
	if !ok {
		return nil, "", apperrors.New("app.sessionMessageService.BuildProviderHistory", apperrors.CodeInvalidArgument, "session not found")
	}

	messages := make([]provider.Message, 0, len(current.Transcript)+1)
	systemLines := make([]string, 0)
	for _, entry := range current.Transcript {
		if entry.Metadata != nil && entry.Metadata["status"] == "in_progress" {
			continue
		}
		switch entry.Kind {
		case domain.TranscriptSystem:
			systemLines = append(systemLines, entry.Message)
		case domain.TranscriptUser:
			messages = append(messages, provider.Message{Role: "user", Content: entry.Message})
		case domain.TranscriptAssistant:
			if strings.TrimSpace(entry.Message) == "" {
				continue
			}
			messages = append(messages, provider.Message{Role: "assistant", Content: entry.Message})
		case domain.TranscriptReasoning:
			continue
		case domain.TranscriptTool:
			messages = append(messages, provider.Message{Role: "assistant", Content: "Tool result:\n" + entry.Message})
		}
	}
	if strings.TrimSpace(extraUser) != "" {
		messages = append(messages, provider.Message{Role: "user", Content: extraUser})
	}
	return messages, strings.TrimSpace(strings.Join(systemLines, "\n\n")), nil
}

func (s *sessionMessageService) DetectRepeatedPromptLoop(sessionID, prompt string) error {
	current, ok := s.Session(sessionID)
	if !ok {
		return apperrors.New("app.sessionMessageService.DetectRepeatedPromptLoop", apperrors.CodeInvalidArgument, "session not found")
	}
	count := 0
	for index := len(current.Transcript) - 1; index >= 0; index-- {
		entry := current.Transcript[index]
		if entry.Kind != domain.TranscriptUser {
			continue
		}
		if strings.TrimSpace(entry.Message) != strings.TrimSpace(prompt) {
			break
		}
		count++
		if count >= repeatedPromptLoopThreshold {
			return apperrors.New("app.sessionMessageService.DetectRepeatedPromptLoop", apperrors.CodeToolFailed, "repeated prompt loop detected for this session")
		}
	}
	return nil
}

func (s *sessionTaskService) Set(sessionID string, tasks []domain.Task) error {
	if s == nil || s.store == nil {
		return apperrors.New("app.sessionTaskService.Set", apperrors.CodeStartupFailed, "session task service is not configured")
	}
	if err := s.store.Set(sessionID, tasks); err != nil {
		return err
	}
	if s.publisher != nil {
		s.publisher.PublishTasksUpdated(sessionID, tasks)
	}
	return nil
}

func (s *sessionUsageService) Record(sessionID string, update session.UsageUpdate) (session.UsageTotals, error) {
	if s == nil || s.store == nil {
		return session.UsageTotals{}, apperrors.New("app.sessionUsageService.Record", apperrors.CodeStartupFailed, "session usage service is not configured")
	}
	return s.store.Record(sessionID, update)
}

func (s *sessionFileTrackingService) TrackToolResult(sessionID, tool string, result runtime.ToolResult, nextID func(string) string, now func() time.Time) error {
	if s == nil || s.store == nil {
		return apperrors.New("app.sessionFileTrackingService.TrackToolResult", apperrors.CodeStartupFailed, "session file tracking service is not configured")
	}
	records := buildTrackedFileRecords(sessionID, tool, result, nextID, now)
	if len(records) == 0 {
		return nil
	}
	return s.store.TrackAll(sessionID, records)
}

func buildTrackedFileRecords(sessionID, tool string, result runtime.ToolResult, nextID func(string) string, now func() time.Time) []session.FileRecord {
	if nextID == nil || now == nil {
		return nil
	}
	records := make([]session.FileRecord, 0, 2)
	timestamp := now()
	if path := strings.TrimSpace(result.Path); path != "" {
		kind := session.FileRecordWorkspace
		if tool == "fetch" {
			kind = session.FileRecordFetched
		}
		records = append(records, session.FileRecord{
			ID:        nextID("file"),
			SessionID: sessionID,
			Kind:      kind,
			Path:      path,
			Tool:      tool,
			CreatedAt: timestamp,
			UpdatedAt: timestamp,
		})
	}
	if historyPath := strings.TrimSpace(result.Extra["history_path"]); historyPath != "" {
		records = append(records, session.FileRecord{
			ID:        nextID("history"),
			SessionID: sessionID,
			Kind:      session.FileRecordHistory,
			Path:      historyPath,
			Tool:      tool,
			CreatedAt: timestamp,
			UpdatedAt: timestamp,
			Metadata: map[string]string{
				"source_path": strings.TrimSpace(result.Path),
			},
		})
	}
	return records
}

func cloneTranscriptEntry(entry domain.TranscriptEntry) domain.TranscriptEntry {
	entry.Metadata = cloneStringMap(entry.Metadata)
	return entry
}

func cloneTranscriptEntries(entries []domain.TranscriptEntry) []domain.TranscriptEntry {
	cloned := make([]domain.TranscriptEntry, 0, len(entries))
	for _, entry := range entries {
		cloned = append(cloned, cloneTranscriptEntry(entry))
	}
	return cloned
}

func cloneTasks(tasks []domain.Task) []domain.Task {
	cloned := make([]domain.Task, 0, len(tasks))
	for _, task := range tasks {
		copied := task
		copied.DependsOn = append([]string(nil), task.DependsOn...)
		cloned = append(cloned, copied)
	}
	return cloned
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func logServiceFailure(scope string, err error) {
	if err == nil {
		return
	}
	logging.Warn(fmt.Sprintf("%s failed", scope), "error", err)
}
