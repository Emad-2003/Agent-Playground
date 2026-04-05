package session

import (
	"time"

	"crawler-ai/internal/domain"
)

type FileRecordKind string

const (
	FileRecordWorkspace FileRecordKind = "workspace"
	FileRecordHistory   FileRecordKind = "history"
	FileRecordFetched   FileRecordKind = "fetched"
)

type PersistedSession struct {
	Session  SessionRecord   `json:"session"`
	Messages []MessageRecord `json:"messages,omitempty"`
	Tasks    []TaskRecord    `json:"tasks,omitempty"`
	Files    []FileRecord    `json:"files,omitempty"`
	Usage    UsageTotals     `json:"usage,omitempty"`
}

type SessionSummary struct {
	ID              string      `json:"id"`
	ParentSessionID string      `json:"parent_session_id,omitempty"`
	WorkspaceRoot   string      `json:"workspace_root"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
	ActiveTaskIDs   []string    `json:"active_task_ids,omitempty"`
	MessageCount    int         `json:"message_count"`
	TaskCount       int         `json:"task_count"`
	FileCount       int         `json:"file_count"`
	Usage           UsageTotals `json:"usage,omitempty"`
}

type SessionRecord struct {
	ID              string    `json:"id"`
	ParentSessionID string    `json:"parent_session_id,omitempty"`
	WorkspaceRoot   string    `json:"workspace_root"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	ActiveTaskIDs   []string  `json:"active_task_ids,omitempty"`
	MessageCount    int       `json:"message_count,omitempty"`
	TaskCount       int       `json:"task_count,omitempty"`
	FileCount       int       `json:"file_count,omitempty"`
}

type MessageRecord struct {
	ID        string                `json:"id"`
	SessionID string                `json:"session_id,omitempty"`
	Kind      domain.TranscriptKind `json:"kind"`
	Message   string                `json:"message"`
	CreatedAt time.Time             `json:"created_at"`
	UpdatedAt time.Time             `json:"updated_at,omitempty"`
	Metadata  map[string]string     `json:"metadata,omitempty"`
}

type TaskRecord struct {
	ID          string            `json:"id"`
	SessionID   string            `json:"session_id,omitempty"`
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Status      domain.TaskStatus `json:"status"`
	Assignee    domain.AgentRole  `json:"assignee,omitempty"`
	DependsOn   []string          `json:"depends_on,omitempty"`
	Result      string            `json:"result,omitempty"`
	CreatedAt   time.Time         `json:"created_at,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at,omitempty"`
}

type FileRecord struct {
	ID        string            `json:"id"`
	SessionID string            `json:"session_id,omitempty"`
	Kind      FileRecordKind    `json:"kind,omitempty"`
	Path      string            `json:"path"`
	Tool      string            `json:"tool,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

func persistedSessionFromSession(sess Session) PersistedSession {
	return PersistedSession{
		Session: SessionRecord{
			ID:              sess.ID,
			ParentSessionID: sess.ParentSessionID,
			WorkspaceRoot:   sess.WorkspaceRoot,
			CreatedAt:       sess.CreatedAt,
			UpdatedAt:       sess.UpdatedAt,
			ActiveTaskIDs:   append([]string(nil), sess.ActiveTaskIDs...),
			MessageCount:    len(sess.Transcript),
			TaskCount:       len(sess.Tasks),
			FileCount:       len(sess.Files),
		},
		Messages: messageRecordsFromTranscript(sess.ID, sess.Transcript),
		Tasks:    taskRecordsFromTasks(sess.ID, sess.Tasks),
		Files:    fileRecordsFromFiles(sess.ID, sess.Files),
		Usage:    sess.Usage,
	}
}

func summaryFromPersisted(persisted PersistedSession) SessionSummary {
	return SessionSummary{
		ID:              persisted.Session.ID,
		ParentSessionID: persisted.Session.ParentSessionID,
		WorkspaceRoot:   persisted.Session.WorkspaceRoot,
		CreatedAt:       persisted.Session.CreatedAt,
		UpdatedAt:       persisted.Session.UpdatedAt,
		ActiveTaskIDs:   append([]string(nil), persisted.Session.ActiveTaskIDs...),
		MessageCount:    persisted.Session.MessageCount,
		TaskCount:       persisted.Session.TaskCount,
		FileCount:       persisted.Session.FileCount,
		Usage:           persisted.Usage,
	}
}

func sessionFromPersisted(persisted PersistedSession) Session {
	return Session{
		ID:              persisted.Session.ID,
		ParentSessionID: persisted.Session.ParentSessionID,
		WorkspaceRoot:   persisted.Session.WorkspaceRoot,
		CreatedAt:       persisted.Session.CreatedAt,
		UpdatedAt:       persisted.Session.UpdatedAt,
		Transcript:      transcriptFromMessageRecords(persisted.Messages),
		Tasks:           tasksFromTaskRecords(persisted.Tasks),
		ActiveTaskIDs:   append([]string(nil), persisted.Session.ActiveTaskIDs...),
		Files:           filesFromFileRecords(persisted.Files),
		Usage:           persisted.Usage,
	}
}

func messageRecordsFromTranscript(sessionID string, entries []domain.TranscriptEntry) []MessageRecord {
	records := make([]MessageRecord, 0, len(entries))
	for _, entry := range entries {
		records = append(records, MessageRecord{
			ID:        entry.ID,
			SessionID: sessionID,
			Kind:      entry.Kind,
			Message:   entry.Message,
			CreatedAt: entry.CreatedAt,
			UpdatedAt: entry.UpdatedAt,
			Metadata:  cloneStringMap(entry.Metadata),
		})
	}
	return records
}

func transcriptFromMessageRecords(records []MessageRecord) []domain.TranscriptEntry {
	entries := make([]domain.TranscriptEntry, 0, len(records))
	for _, record := range records {
		entries = append(entries, domain.TranscriptEntry{
			ID:        record.ID,
			Kind:      record.Kind,
			Message:   record.Message,
			CreatedAt: record.CreatedAt,
			UpdatedAt: record.UpdatedAt,
			Metadata:  cloneStringMap(record.Metadata),
		})
	}
	return entries
}

func taskRecordsFromTasks(sessionID string, tasks []domain.Task) []TaskRecord {
	records := make([]TaskRecord, 0, len(tasks))
	for _, task := range tasks {
		records = append(records, TaskRecord{
			ID:          task.ID,
			SessionID:   sessionID,
			Title:       task.Title,
			Description: task.Description,
			Status:      task.Status,
			Assignee:    task.Assignee,
			DependsOn:   append([]string(nil), task.DependsOn...),
			Result:      task.Result,
			CreatedAt:   task.CreatedAt,
			UpdatedAt:   task.UpdatedAt,
		})
	}
	return records
}

func tasksFromTaskRecords(records []TaskRecord) []domain.Task {
	tasks := make([]domain.Task, 0, len(records))
	for _, record := range records {
		tasks = append(tasks, domain.Task{
			ID:          record.ID,
			Title:       record.Title,
			Description: record.Description,
			Status:      record.Status,
			Assignee:    record.Assignee,
			DependsOn:   append([]string(nil), record.DependsOn...),
			Result:      record.Result,
			CreatedAt:   record.CreatedAt,
			UpdatedAt:   record.UpdatedAt,
		})
	}
	return tasks
}

func fileRecordsFromFiles(sessionID string, files []FileRecord) []FileRecord {
	records := make([]FileRecord, 0, len(files))
	for _, file := range files {
		copied := cloneFileRecord(file)
		copied.SessionID = sessionID
		records = append(records, copied)
	}
	return records
}

func filesFromFileRecords(records []FileRecord) []FileRecord {
	files := make([]FileRecord, 0, len(records))
	for _, record := range records {
		files = append(files, cloneFileRecord(record))
	}
	return files
}

func cloneFileRecord(record FileRecord) FileRecord {
	record.Metadata = cloneStringMap(record.Metadata)
	return record
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
