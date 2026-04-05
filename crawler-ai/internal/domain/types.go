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
	TranscriptReasoning TranscriptKind = "reasoning"
	TranscriptTool      TranscriptKind = "tool"
	TranscriptSystem    TranscriptKind = "system"
)

const TranscriptMetadataResponseID = "response_id"

type TranscriptEntry struct {
	ID        string            `json:"id"`
	Kind      TranscriptKind    `json:"kind"`
	Message   string            `json:"message"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
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
	ToolCallID  string
	Action      string
	Description string
	CreatedAt   time.Time
}
