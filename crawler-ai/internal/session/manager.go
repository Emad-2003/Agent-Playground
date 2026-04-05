package session

import (
	"sync"
	"time"

	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
)

type Session struct {
	ID              string                   `json:"id"`
	ParentSessionID string                   `json:"parent_session_id,omitempty"`
	WorkspaceRoot   string                   `json:"workspace_root"`
	CreatedAt       time.Time                `json:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at"`
	Transcript      []domain.TranscriptEntry `json:"transcript"`
	Tasks           []domain.Task            `json:"tasks,omitempty"`
	ActiveTaskIDs   []string                 `json:"active_task_ids"`
	Files           []FileRecord             `json:"files,omitempty"`
	Usage           UsageTotals              `json:"usage,omitempty"`
}

type UsageTotals struct {
	InputTokens       int64     `json:"input_tokens,omitempty"`
	OutputTokens      int64     `json:"output_tokens,omitempty"`
	ResponseCount     int64     `json:"response_count,omitempty"`
	PricedResponses   int64     `json:"priced_responses,omitempty"`
	UnpricedResponses int64     `json:"unpriced_responses,omitempty"`
	TotalCost         float64   `json:"total_cost,omitempty"`
	LastProvider      string    `json:"last_provider,omitempty"`
	LastModel         string    `json:"last_model,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
}

type UsageUpdate struct {
	InputTokens  int
	OutputTokens int
	TotalCost    float64
	PricingKnown bool
	Provider     string
	Model        string
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]Session
	now      func() time.Time
	store    Store
}

func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]Session),
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (m *Manager) Create(id, workspaceRoot string) (Session, error) {
	return m.CreateChild(id, workspaceRoot, "")
}

func (m *Manager) CreateChild(id, workspaceRoot, parentSessionID string) (Session, error) {
	if id == "" {
		return Session{}, apperrors.New("session.CreateChild", apperrors.CodeInvalidArgument, "session id must not be empty")
	}
	if workspaceRoot == "" {
		return Session{}, apperrors.New("session.CreateChild", apperrors.CodeInvalidArgument, "workspace root must not be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[id]; exists {
		return Session{}, apperrors.New("session.CreateChild", apperrors.CodeInvalidArgument, "session id already exists")
	}
	if parentSessionID != "" {
		if _, exists := m.sessions[parentSessionID]; !exists {
			return Session{}, apperrors.New("session.CreateChild", apperrors.CodeInvalidArgument, "parent session not found")
		}
	}

	now := m.now()
	session := Session{
		ID:              id,
		ParentSessionID: parentSessionID,
		WorkspaceRoot:   workspaceRoot,
		CreatedAt:       now,
		UpdatedAt:       now,
		Transcript:      make([]domain.TranscriptEntry, 0),
		Tasks:           make([]domain.Task, 0),
		ActiveTaskIDs:   make([]string, 0),
		Files:           make([]FileRecord, 0),
	}
	m.sessions[id] = session
	return session, nil
}

func (m *Manager) Get(id string) (Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[id]
	if !ok {
		return Session{}, false
	}
	return cloneSession(session), true
}

func (m *Manager) List() []Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		items = append(items, cloneSession(session))
	}

	return items
}

func (m *Manager) AppendTranscript(sessionID string, entry domain.TranscriptEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return apperrors.New("session.AppendTranscript", apperrors.CodeInvalidArgument, "session not found")
	}

	session.Transcript = append(session.Transcript, entry)
	session.UpdatedAt = m.now()
	m.sessions[sessionID] = session
	return nil
}

func (m *Manager) UpdateTranscript(sessionID string, entry domain.TranscriptEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return apperrors.New("session.UpdateTranscript", apperrors.CodeInvalidArgument, "session not found")
	}

	for index := range session.Transcript {
		if session.Transcript[index].ID != entry.ID {
			continue
		}
		session.Transcript[index] = entry
		session.UpdatedAt = m.now()
		m.sessions[sessionID] = session
		return nil
	}

	return apperrors.New("session.UpdateTranscript", apperrors.CodeInvalidArgument, "transcript entry not found")
}

func (m *Manager) ReplaceTranscript(sessionID string, entries []domain.TranscriptEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return apperrors.New("session.ReplaceTranscript", apperrors.CodeInvalidArgument, "session not found")
	}

	session.Transcript = append([]domain.TranscriptEntry(nil), entries...)
	session.UpdatedAt = m.now()
	m.sessions[sessionID] = session
	return nil
}

func (m *Manager) SetActiveTasks(sessionID string, taskIDs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return apperrors.New("session.SetActiveTasks", apperrors.CodeInvalidArgument, "session not found")
	}

	cloned := append([]string(nil), taskIDs...)
	session.ActiveTaskIDs = cloned
	session.UpdatedAt = m.now()
	m.sessions[sessionID] = session
	return nil
}

func (m *Manager) SetTasks(sessionID string, tasks []domain.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return apperrors.New("session.SetTasks", apperrors.CodeInvalidArgument, "session not found")
	}

	clonedTasks := cloneTasks(tasks)
	activeIDs := make([]string, 0, len(clonedTasks))
	for _, task := range clonedTasks {
		activeIDs = append(activeIDs, task.ID)
	}

	session.Tasks = clonedTasks
	session.ActiveTaskIDs = activeIDs
	session.UpdatedAt = m.now()
	m.sessions[sessionID] = session
	return nil
}

func (m *Manager) TrackFile(sessionID string, file FileRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return apperrors.New("session.TrackFile", apperrors.CodeInvalidArgument, "session not found")
	}

	now := m.now()
	if file.CreatedAt.IsZero() {
		file.CreatedAt = now
	}
	if file.UpdatedAt.IsZero() {
		file.UpdatedAt = file.CreatedAt
	}
	file.Metadata = cloneStringMap(file.Metadata)

	if file.ID != "" {
		for index := range session.Files {
			if session.Files[index].ID != file.ID {
				continue
			}
			session.Files[index] = cloneFileRecord(file)
			session.UpdatedAt = now
			m.sessions[sessionID] = session
			return nil
		}
	}

	session.Files = append(session.Files, cloneFileRecord(file))
	session.UpdatedAt = now
	m.sessions[sessionID] = session
	return nil
}

func (m *Manager) RecordUsage(sessionID string, update UsageUpdate) (UsageTotals, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return UsageTotals{}, apperrors.New("session.RecordUsage", apperrors.CodeInvalidArgument, "session not found")
	}

	usage := session.Usage
	usage.InputTokens += int64(update.InputTokens)
	usage.OutputTokens += int64(update.OutputTokens)
	usage.ResponseCount++
	if update.PricingKnown {
		usage.TotalCost += update.TotalCost
		usage.PricedResponses++
	} else {
		usage.UnpricedResponses++
	}
	usage.LastProvider = update.Provider
	usage.LastModel = update.Model
	usage.UpdatedAt = m.now()

	session.Usage = usage
	session.UpdatedAt = usage.UpdatedAt
	m.sessions[sessionID] = session
	return usage, nil
}

func cloneSession(session Session) Session {
	session.Transcript = cloneTranscript(session.Transcript)
	session.Tasks = cloneTasks(session.Tasks)
	session.ActiveTaskIDs = append([]string(nil), session.ActiveTaskIDs...)
	session.Files = cloneFiles(session.Files)
	return session
}

func cloneTranscript(entries []domain.TranscriptEntry) []domain.TranscriptEntry {
	cloned := make([]domain.TranscriptEntry, 0, len(entries))
	for _, entry := range entries {
		copied := entry
		if entry.Metadata != nil {
			copied.Metadata = make(map[string]string, len(entry.Metadata))
			for key, value := range entry.Metadata {
				copied.Metadata[key] = value
			}
		}
		cloned = append(cloned, copied)
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

func cloneFiles(files []FileRecord) []FileRecord {
	cloned := make([]FileRecord, 0, len(files))
	for _, file := range files {
		cloned = append(cloned, cloneFileRecord(file))
	}
	return cloned
}
