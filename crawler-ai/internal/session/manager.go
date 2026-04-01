package session

import (
	"sync"
	"time"

	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
)

type Session struct {
	ID            string
	WorkspaceRoot string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Transcript    []domain.TranscriptEntry
	ActiveTaskIDs []string
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]Session
	now      func() time.Time
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
	if id == "" {
		return Session{}, apperrors.New("session.Create", apperrors.CodeInvalidArgument, "session id must not be empty")
	}
	if workspaceRoot == "" {
		return Session{}, apperrors.New("session.Create", apperrors.CodeInvalidArgument, "workspace root must not be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[id]; exists {
		return Session{}, apperrors.New("session.Create", apperrors.CodeInvalidArgument, "session id already exists")
	}

	now := m.now()
	session := Session{
		ID:            id,
		WorkspaceRoot: workspaceRoot,
		CreatedAt:     now,
		UpdatedAt:     now,
		Transcript:    make([]domain.TranscriptEntry, 0),
		ActiveTaskIDs: make([]string, 0),
	}
	m.sessions[id] = session
	return session, nil
}

func (m *Manager) Get(id string) (Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[id]
	return session, ok
}

func (m *Manager) List() []Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		items = append(items, session)
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
