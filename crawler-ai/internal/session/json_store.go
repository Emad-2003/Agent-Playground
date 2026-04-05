package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	apperrors "crawler-ai/internal/errors"
)

type JSONStore struct {
	dir string
}

const (
	sessionRecordFilename  = "session.json"
	messageRecordsFilename = "messages.json"
	taskRecordsFilename    = "tasks.json"
	fileRecordsFilename    = "files.json"
	usageRecordFilename    = "usage.json"
)

func NewJSONStore(dir string) *JSONStore {
	return &JSONStore{dir: strings.TrimSpace(dir)}
}

func (s *JSONStore) Save(sess PersistedSession) error {
	if s == nil || s.dir == "" {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return apperrors.Wrap("session.JSONStore.Save", apperrors.CodeToolFailed, err, "create session directory")
	}
	sessionDir := filepath.Join(s.dir, sanitizeFilename(sess.Session.ID))
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return apperrors.Wrap("session.JSONStore.Save", apperrors.CodeToolFailed, err, "create normalized session directory")
	}
	files := []struct {
		name  string
		value any
	}{
		{name: sessionRecordFilename, value: sess.Session},
		{name: messageRecordsFilename, value: sess.Messages},
		{name: taskRecordsFilename, value: sess.Tasks},
		{name: fileRecordsFilename, value: sess.Files},
		{name: usageRecordFilename, value: sess.Usage},
	}
	for _, item := range files {
		if err := writeJSONAtomically(filepath.Join(sessionDir, item.name), item.value); err != nil {
			return err
		}
	}
	_ = os.Remove(filepath.Join(s.dir, sanitizeFilename(sess.Session.ID)+".json"))
	return nil
}

func (s *JSONStore) LoadAll() ([]PersistedSession, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.Wrap("session.JSONStore.LoadAll", apperrors.CodeToolFailed, err, "read session directory")
	}

	sessions := make([]PersistedSession, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			sess, err := s.loadNormalizedSession(filepath.Join(s.dir, entry.Name()))
			if err != nil || strings.TrimSpace(sess.Session.ID) == "" {
				continue
			}
			sessions = append(sessions, sess)
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		legacy, err := s.loadLegacySession(filepath.Join(s.dir, entry.Name()))
		if err != nil || strings.TrimSpace(legacy.Session.ID) == "" {
			continue
		}
		sessions = append(sessions, legacy)
	}

	return sessions, nil
}

func (s *JSONStore) LoadSession(id string) (PersistedSession, error) {
	if s == nil || s.dir == "" {
		return PersistedSession{}, apperrors.New("session.JSONStore.LoadSession", apperrors.CodeInvalidArgument, "session store directory is not configured")
	}
	if persisted, err := s.loadNormalizedSession(s.sessionDir(id)); err == nil && strings.TrimSpace(persisted.Session.ID) != "" {
		return persisted, nil
	}
	if persisted, err := s.loadLegacySession(s.legacyPath(id)); err == nil && strings.TrimSpace(persisted.Session.ID) != "" {
		return persisted, nil
	}
	return PersistedSession{}, apperrors.New("session.JSONStore.LoadSession", apperrors.CodeInvalidArgument, "session not found")
}

func (s *JSONStore) LoadSummary(id string) (SessionSummary, error) {
	if s == nil || s.dir == "" {
		return SessionSummary{}, apperrors.New("session.JSONStore.LoadSummary", apperrors.CodeInvalidArgument, "session store directory is not configured")
	}
	if summary, err := s.loadNormalizedSummary(s.sessionDir(id)); err == nil && strings.TrimSpace(summary.ID) != "" {
		return summary, nil
	}
	if persisted, err := s.loadLegacySession(s.legacyPath(id)); err == nil && strings.TrimSpace(persisted.Session.ID) != "" {
		return summaryFromPersisted(persisted), nil
	}
	return SessionSummary{}, apperrors.New("session.JSONStore.LoadSummary", apperrors.CodeInvalidArgument, "session not found")
}

func (s *JSONStore) ListSummaries() ([]SessionSummary, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.Wrap("session.JSONStore.ListSummaries", apperrors.CodeToolFailed, err, "read session directory")
	}

	summaries := make([]SessionSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			summary, err := s.loadNormalizedSummary(filepath.Join(s.dir, entry.Name()))
			if err != nil || strings.TrimSpace(summary.ID) == "" {
				continue
			}
			summaries = append(summaries, summary)
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		persisted, err := s.loadLegacySession(filepath.Join(s.dir, entry.Name()))
		if err != nil || strings.TrimSpace(persisted.Session.ID) == "" {
			continue
		}
		summaries = append(summaries, summaryFromPersisted(persisted))
	}

	return summaries, nil
}

func (s *JSONStore) LoadMessages(id string) ([]MessageRecord, error) {
	if records, err := s.loadNormalizedMessages(s.sessionDir(id)); err == nil {
		return records, nil
	}
	persisted, err := s.loadLegacySession(s.legacyPath(id))
	if err != nil {
		return nil, apperrors.New("session.JSONStore.LoadMessages", apperrors.CodeInvalidArgument, "session not found")
	}
	return persisted.Messages, nil
}

func (s *JSONStore) LoadTasks(id string) ([]TaskRecord, error) {
	if records, err := s.loadNormalizedTasks(s.sessionDir(id)); err == nil {
		return records, nil
	}
	persisted, err := s.loadLegacySession(s.legacyPath(id))
	if err != nil {
		return nil, apperrors.New("session.JSONStore.LoadTasks", apperrors.CodeInvalidArgument, "session not found")
	}
	return persisted.Tasks, nil
}

func (s *JSONStore) LoadFiles(id string) ([]FileRecord, error) {
	if records, err := s.loadNormalizedFiles(s.sessionDir(id)); err == nil {
		return records, nil
	}
	persisted, err := s.loadLegacySession(s.legacyPath(id))
	if err != nil {
		return nil, apperrors.New("session.JSONStore.LoadFiles", apperrors.CodeInvalidArgument, "session not found")
	}
	return persisted.Files, nil
}

func (s *JSONStore) LoadUsage(id string) (UsageTotals, error) {
	if usage, err := s.loadNormalizedUsage(s.sessionDir(id)); err == nil {
		return usage, nil
	}
	persisted, err := s.loadLegacySession(s.legacyPath(id))
	if err != nil {
		return UsageTotals{}, apperrors.New("session.JSONStore.LoadUsage", apperrors.CodeInvalidArgument, "session not found")
	}
	return persisted.Usage, nil
}

func (s *JSONStore) Delete(id string) error {
	if s == nil || s.dir == "" {
		return nil
	}
	legacyPath := filepath.Join(s.dir, sanitizeFilename(id)+".json")
	if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
		return apperrors.Wrap("session.JSONStore.Delete", apperrors.CodeToolFailed, err, "delete session file")
	}
	path := filepath.Join(s.dir, sanitizeFilename(id))
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return apperrors.Wrap("session.JSONStore.Delete", apperrors.CodeToolFailed, err, "delete normalized session directory")
	}
	return nil
}

func (s *JSONStore) sessionDir(id string) string {
	return filepath.Join(s.dir, sanitizeFilename(id))
}

func (s *JSONStore) legacyPath(id string) string {
	return filepath.Join(s.dir, sanitizeFilename(id)+".json")
}

func (s *JSONStore) loadNormalizedSummary(dir string) (SessionSummary, error) {
	var record SessionRecord
	if err := readJSONFile(filepath.Join(dir, sessionRecordFilename), &record); err != nil {
		return SessionSummary{}, err
	}
	usage, err := s.loadNormalizedUsage(dir)
	if err != nil && !os.IsNotExist(err) {
		return SessionSummary{}, err
	}
	return SessionSummary{
		ID:            record.ID,
		WorkspaceRoot: record.WorkspaceRoot,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
		ActiveTaskIDs: append([]string(nil), record.ActiveTaskIDs...),
		MessageCount:  record.MessageCount,
		TaskCount:     record.TaskCount,
		FileCount:     record.FileCount,
		Usage:         usage,
	}, nil
}

func (s *JSONStore) loadNormalizedMessages(dir string) ([]MessageRecord, error) {
	var records []MessageRecord
	if err := readJSONFile(filepath.Join(dir, messageRecordsFilename), &records); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *JSONStore) loadNormalizedTasks(dir string) ([]TaskRecord, error) {
	var records []TaskRecord
	if err := readJSONFile(filepath.Join(dir, taskRecordsFilename), &records); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *JSONStore) loadNormalizedFiles(dir string) ([]FileRecord, error) {
	var records []FileRecord
	if err := readJSONFile(filepath.Join(dir, fileRecordsFilename), &records); err != nil {
		return nil, err
	}
	return records, nil
}

func (s *JSONStore) loadNormalizedUsage(dir string) (UsageTotals, error) {
	var usage UsageTotals
	if err := readJSONFile(filepath.Join(dir, usageRecordFilename), &usage); err != nil {
		return UsageTotals{}, err
	}
	return usage, nil
}

func (s *JSONStore) loadNormalizedSession(dir string) (PersistedSession, error) {
	var persisted PersistedSession
	if err := readJSONFile(filepath.Join(dir, sessionRecordFilename), &persisted.Session); err != nil {
		return PersistedSession{}, err
	}
	if err := readJSONFile(filepath.Join(dir, messageRecordsFilename), &persisted.Messages); err != nil && !os.IsNotExist(err) {
		return PersistedSession{}, err
	}
	if err := readJSONFile(filepath.Join(dir, taskRecordsFilename), &persisted.Tasks); err != nil && !os.IsNotExist(err) {
		return PersistedSession{}, err
	}
	if err := readJSONFile(filepath.Join(dir, fileRecordsFilename), &persisted.Files); err != nil && !os.IsNotExist(err) {
		return PersistedSession{}, err
	}
	if err := readJSONFile(filepath.Join(dir, usageRecordFilename), &persisted.Usage); err != nil && !os.IsNotExist(err) {
		return PersistedSession{}, err
	}
	return persisted, nil
}

func (s *JSONStore) loadLegacySession(path string) (PersistedSession, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PersistedSession{}, err
	}
	var legacy Session
	if err := json.Unmarshal(data, &legacy); err != nil {
		return PersistedSession{}, err
	}
	return persistedSessionFromSession(legacy), nil
}

func writeJSONAtomically(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return apperrors.Wrap("session.writeJSONAtomically", apperrors.CodeToolFailed, err, "marshal normalized session record")
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return apperrors.Wrap("session.writeJSONAtomically", apperrors.CodeToolFailed, err, "write normalized session record")
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return apperrors.Wrap("session.writeJSONAtomically", apperrors.CodeToolFailed, err, "replace normalized session record")
	}
	return nil
}

func readJSONFile(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, target)
}

func sanitizeFilename(id string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return r.Replace(id)
}
