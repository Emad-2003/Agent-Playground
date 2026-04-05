package app

import (
	"testing"
	"time"

	"crawler-ai/internal/domain"
	"crawler-ai/internal/events"
	"crawler-ai/internal/runtime"
	"crawler-ai/internal/tui"
)

func TestActivityEntryFromEventMapsToolStates(t *testing.T) {
	t.Parallel()

	timestamp := time.Date(2026, 4, 1, 11, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		event  events.Event
		label  string
		level  tui.ActivityLevel
		detail string
	}{
		{
			name:   "tool started",
			event:  events.Event{Name: runtime.EventToolStarted, Timestamp: timestamp, Payload: map[string]any{"tool": "read_file"}},
			label:  "Tool running",
			level:  tui.ActivityPending,
			detail: "read_file",
		},
		{
			name:   "tool completed",
			event:  events.Event{Name: runtime.EventToolCompleted, Timestamp: timestamp, Payload: map[string]any{"tool": "write_file", "path": "notes.txt"}},
			label:  "Tool completed",
			level:  tui.ActivitySuccess,
			detail: "write_file notes.txt",
		},
		{
			name:   "tool failed",
			event:  events.Event{Name: runtime.EventToolFailed, Timestamp: timestamp, Payload: map[string]any{"tool": "shell", "error": "exit status 1"}},
			label:  "Tool failed",
			level:  tui.ActivityError,
			detail: "shell exit status 1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entry, ok := activityEntryFromEvent(test.event)
			if !ok {
				t.Fatal("expected event to map to activity entry")
			}
			if entry.Label != test.label {
				t.Fatalf("expected label %q, got %q", test.label, entry.Label)
			}
			if entry.Level != test.level {
				t.Fatalf("expected level %d, got %d", test.level, entry.Level)
			}
			if entry.Detail != test.detail {
				t.Fatalf("expected detail %q, got %q", test.detail, entry.Detail)
			}
			if !entry.CreatedAt.Equal(timestamp) {
				t.Fatalf("expected timestamp %v, got %v", timestamp, entry.CreatedAt)
			}
		})
	}
}

func TestActivityEntryFromEventMapsApprovalEvents(t *testing.T) {
	t.Parallel()

	requested, ok := activityEntryFromEvent(events.Event{
		Name:      events.EventApprovalRequested,
		Timestamp: time.Date(2026, 4, 1, 11, 5, 0, 0, time.UTC),
		Payload: domain.ApprovalRequest{
			ID:     "approval-1",
			Action: "write_file",
		},
	})
	if !ok {
		t.Fatal("expected approval requested event to map")
	}
	if requested.Label != "Approval requested" || requested.Level != tui.ActivityPending {
		t.Fatalf("unexpected approval requested entry: %+v", requested)
	}
	if requested.Detail != "write_file" {
		t.Fatalf("expected approval detail to be action, got %q", requested.Detail)
	}

	cleared, ok := activityEntryFromEvent(events.Event{
		Name:      events.EventApprovalCleared,
		Timestamp: time.Date(2026, 4, 1, 11, 6, 0, 0, time.UTC),
		Payload:   "approval-1",
	})
	if !ok {
		t.Fatal("expected approval cleared event to map")
	}
	if cleared.Label != "Approval resolved" || cleared.Level != tui.ActivityInfo {
		t.Fatalf("unexpected approval cleared entry: %+v", cleared)
	}
}

func TestActivityEntryFromEventRejectsUnknownEvents(t *testing.T) {
	t.Parallel()

	if _, ok := activityEntryFromEvent(events.Event{Name: "unknown"}); ok {
		t.Fatal("expected unknown event to be ignored")
	}
}
