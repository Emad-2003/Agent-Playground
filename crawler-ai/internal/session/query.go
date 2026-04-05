package session

import (
	"crawler-ai/internal/domain"
	apperrors "crawler-ai/internal/errors"
)

type storageHealthProvider interface {
	StorageHealth() (StorageHealth, error)
}

func (m *Manager) ReadPersistedSummary(id string) (SessionSummary, error) {
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()

	if store == nil {
		sess, ok := m.Get(id)
		if !ok {
			return SessionSummary{}, apperrors.New("session.ReadPersistedSummary", apperrors.CodeInvalidArgument, "session not found")
		}
		return summaryFromPersisted(persistedSessionFromSession(sess)), nil
	}

	return store.LoadSummary(id)
}

func (m *Manager) ListPersistedSummaries() ([]SessionSummary, error) {
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()

	if store == nil {
		sessions := m.List()
		summaries := make([]SessionSummary, 0, len(sessions))
		for _, sess := range sessions {
			summaries = append(summaries, summaryFromPersisted(persistedSessionFromSession(sess)))
		}
		return summaries, nil
	}

	return store.ListSummaries()
}

func (m *Manager) ReadPersistedMessages(id string) ([]domain.TranscriptEntry, error) {
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()

	if store == nil {
		sess, ok := m.Get(id)
		if !ok {
			return nil, apperrors.New("session.ReadPersistedMessages", apperrors.CodeInvalidArgument, "session not found")
		}
		return cloneTranscript(sess.Transcript), nil
	}

	records, err := store.LoadMessages(id)
	if err != nil {
		return nil, err
	}
	return transcriptFromMessageRecords(records), nil
}

func (m *Manager) ReadPersistedTasks(id string) ([]domain.Task, error) {
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()

	if store == nil {
		sess, ok := m.Get(id)
		if !ok {
			return nil, apperrors.New("session.ReadPersistedTasks", apperrors.CodeInvalidArgument, "session not found")
		}
		return cloneTasks(sess.Tasks), nil
	}

	records, err := store.LoadTasks(id)
	if err != nil {
		return nil, err
	}
	return tasksFromTaskRecords(records), nil
}

func (m *Manager) ReadPersistedFiles(id string) ([]FileRecord, error) {
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()

	if store == nil {
		sess, ok := m.Get(id)
		if !ok {
			return nil, apperrors.New("session.ReadPersistedFiles", apperrors.CodeInvalidArgument, "session not found")
		}
		return cloneFiles(sess.Files), nil
	}

	records, err := store.LoadFiles(id)
	if err != nil {
		return nil, err
	}
	return filesFromFileRecords(records), nil
}

func (m *Manager) ReadPersistedUsage(id string) (UsageTotals, error) {
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()

	if store == nil {
		sess, ok := m.Get(id)
		if !ok {
			return UsageTotals{}, apperrors.New("session.ReadPersistedUsage", apperrors.CodeInvalidArgument, "session not found")
		}
		return sess.Usage, nil
	}

	return store.LoadUsage(id)
}

func (m *Manager) ReadStorageHealth() (StorageHealth, error) {
	m.mu.RLock()
	store := m.store
	sessionCount := len(m.sessions)
	m.mu.RUnlock()

	if store == nil {
		return StorageHealth{
			Backend: "memory",
			Counts: StorageRecordCounts{
				Sessions: sessionCount,
			},
			Notes: []string{"sessions are only held in memory; persisted storage diagnostics are unavailable"},
		}, nil
	}

	if provider, ok := store.(storageHealthProvider); ok {
		return provider.StorageHealth()
	}

	summaries, err := store.ListSummaries()
	if err != nil {
		return StorageHealth{}, err
	}
	return StorageHealth{
		Backend: "custom",
		Counts: StorageRecordCounts{
			Sessions: len(summaries),
		},
		Notes: []string{"storage backend does not expose detailed health metrics"},
	}, nil
}
