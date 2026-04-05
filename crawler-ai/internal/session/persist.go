package session

import (
	"path/filepath"

	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/oauth"
)

// DefaultDataDir returns the default session persistence directory.
func DefaultDataDir() string {
	return filepath.Join(oauth.DefaultConfigDir(), "sessions")
}

// SetDataDir configures the directory where sessions are persisted.
// If empty, sessions remain in-memory only.
func (m *Manager) SetDataDir(dir string) {
	m.SetStore(NewSQLiteStore(dir))
}

// SetStore configures the persistence backend for sessions.
// Passing nil leaves the manager in memory-only mode.
func (m *Manager) SetStore(store Store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store = store
}

// Save persists the given session through the configured store.
func (m *Manager) Save(id string) error {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	store := m.store
	m.mu.RUnlock()

	if !ok {
		return apperrors.New("session.Save", apperrors.CodeInvalidArgument, "session not found")
	}
	if store == nil {
		return nil // no persistence configured
	}
	return store.Save(persistedSessionFromSession(cloneSession(sess)))
}

// SaveAll persists every session to disk.
func (m *Manager) SaveAll() error {
	m.mu.RLock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	for _, id := range ids {
		if err := m.Save(id); err != nil {
			return err
		}
	}
	return nil
}

// LoadAll reads all persisted sessions from the configured store and hydrates the manager.
func (m *Manager) LoadAll() error {
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()

	if store == nil {
		return nil
	}

	loaded, err := store.LoadAll()
	if err != nil {
		return err
	}

	for _, persisted := range loaded {
		sess := sessionFromPersisted(persisted)
		m.mu.Lock()
		m.sessions[sess.ID] = cloneSession(sess)
		m.mu.Unlock()
	}
	return nil
}

// Delete removes a session from memory and disk.
func (m *Manager) Delete(id string) error {
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()
	idsToDelete, err := m.collectDeleteIDs(id)
	if err != nil {
		return err
	}

	if store != nil {
		for _, sessionID := range idsToDelete {
			if err := store.Delete(sessionID); err != nil {
				return err
			}
		}
	}

	m.mu.Lock()
	for _, sessionID := range idsToDelete {
		delete(m.sessions, sessionID)
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) collectDeleteIDs(id string) ([]string, error) {
	m.mu.RLock()
	store := m.store
	inMemory := make([]Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		inMemory = append(inMemory, cloneSession(sess))
	}
	m.mu.RUnlock()

	childrenByParent := make(map[string][]string)
	known := false
	for _, sess := range inMemory {
		if sess.ID == id {
			known = true
		}
		if sess.ParentSessionID != "" {
			childrenByParent[sess.ParentSessionID] = append(childrenByParent[sess.ParentSessionID], sess.ID)
		}
	}
	if store != nil {
		summaries, err := store.ListSummaries()
		if err != nil {
			return nil, err
		}
		for _, summary := range summaries {
			if summary.ID == id {
				known = true
			}
			if summary.ParentSessionID != "" {
				childrenByParent[summary.ParentSessionID] = append(childrenByParent[summary.ParentSessionID], summary.ID)
			}
		}
	}
	if !known {
		return nil, apperrors.New("session.Delete", apperrors.CodeInvalidArgument, "session not found")
	}
	ids := make([]string, 0)
	var visit func(string)
	visit = func(sessionID string) {
		ids = append(ids, sessionID)
		for _, childID := range childrenByParent[sessionID] {
			visit(childID)
		}
	}
	visit(id)
	return ids, nil
}
