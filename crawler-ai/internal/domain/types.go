package domain

import "time"

type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskBlocked   TaskStatus = "blocked"
)

type AgentRole string

const (
	RoleOrchestrator AgentRole = "orchestrator"
	RoleWorker       AgentRole = "worker"
	RoleReviewer     AgentRole = "reviewer"
)

type TranscriptKind string

const (
	TranscriptUser      TranscriptKind = "user"
	TranscriptAssistant TranscriptKind = "assistant"
	TranscriptTool      TranscriptKind = "tool"
	TranscriptSystem    TranscriptKind = "system"
)

type TranscriptEntry struct {
	ID        string
	Kind      TranscriptKind
	Message   string
	CreatedAt time.Time
	Metadata  map[string]string
}

type Task struct {
	ID          string
	Title       string
	Description string
	Status      TaskStatus
	Assignee    AgentRole
	DependsOn   []string
	Result      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ApprovalRequest struct {
	ID          string
	Action      string
	Description string
	CreatedAt   time.Time
}
