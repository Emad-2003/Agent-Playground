package ui

import (
	"strings"
	"testing"
	"time"

	"crawler-ai/internal/domain"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewModelHasDefaultState(t *testing.T) {
	t.Parallel()

	model := NewModel()
	if model.status == "" {
		t.Fatal("expected default status")
	}
	if len(model.transcript) != 0 {
		t.Fatalf("expected empty transcript, got %d entries", len(model.transcript))
	}
}

func TestUpdateAppendsTranscriptEntry(t *testing.T) {
	t.Parallel()

	updated, _ := NewModel().Update(AddTranscriptMsg{Entry: domain.TranscriptEntry{
		ID:        "entry-1",
		Kind:      domain.TranscriptAssistant,
		Message:   "hello",
		CreatedAt: time.Now().UTC(),
	}})

	model := updated.(Model)
	if len(model.transcript) != 1 {
		t.Fatalf("expected 1 transcript entry, got %d", len(model.transcript))
	}
}

func TestUpdateSetsTasks(t *testing.T) {
	t.Parallel()

	tasks := []domain.Task{{ID: "task-1", Title: "Implement app", Status: domain.TaskRunning, Assignee: domain.RoleWorker}}
	updated, _ := NewModel().Update(SetTasksMsg{Tasks: tasks})
	model := updated.(Model)

	if len(model.tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(model.tasks))
	}
	if model.tasks[0].Title != "Implement app" {
		t.Fatalf("expected task title Implement app, got %s", model.tasks[0].Title)
	}

	view := model.View()
	if strings.Contains(view, "Loading crawler-ai UI") {
		updated, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		model = updated.(Model)
		view = model.View()
	}
	if !strings.Contains(view, "<worker>") {
		t.Fatalf("expected task assignee in view, got %s", view)
	}
}

func TestRenderTasksShowsResultSummary(t *testing.T) {
	t.Parallel()

	model := NewModel()
	updated, _ := model.Update(SetTasksMsg{Tasks: []domain.Task{{
		ID:       "task-1",
		Title:    "Implement app",
		Status:   domain.TaskCompleted,
		Assignee: domain.RoleWorker,
		Result:   "updated the planner output\nwith test coverage",
	}}})
	model = updated.(Model)

	pane := model.renderTasks(80, 10)
	if !strings.Contains(pane, "result: updated the planner output with test coverage") {
		t.Fatalf("expected summarized task result in task pane, got %s", pane)
	}
}

func TestUpdateWindowSizeAffectsView(t *testing.T) {
	t.Parallel()

	updated, _ := NewModel().Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := updated.(Model)

	view := model.View()
	if !strings.Contains(view, "Transcript") {
		t.Fatalf("expected transcript pane in view, got %s", view)
	}
	if !strings.Contains(view, "Composer") {
		t.Fatalf("expected composer pane in view, got %s", view)
	}
	if !strings.Contains(view, "Tasks") {
		t.Fatalf("expected tasks pane in view, got %s", view)
	}
}

func TestCtrlSSubmitsPromptAndClearsInput(t *testing.T) {
	t.Parallel()

	submitted := ""
	model := NewModel(WithSubmitHandler(func(prompt string) {
		submitted = prompt
	}))
	model.input.SetValue("run the task")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	result := updated.(Model)

	if submitted != "run the task" {
		t.Fatalf("expected submitted prompt, got %q", submitted)
	}
	if result.input.Value() != "" {
		t.Fatalf("expected input to be cleared, got %q", result.input.Value())
	}
}

func TestApprovalOverlayRendersAndApproves(t *testing.T) {
	t.Parallel()

	approved := false
	model := NewModel(WithApprovalHandler(func(request domain.ApprovalRequest, ok bool) {
		approved = ok && request.ID == "approval-1"
	}))

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = updated.(Model)
	updated, _ = model.Update(ShowApprovalMsg{Request: domain.ApprovalRequest{
		ID:          "approval-1",
		Action:      "write_file",
		Description: "Write to README.md",
	}})
	model = updated.(Model)

	view := model.View()
	if !strings.Contains(view, "Approval Required") {
		t.Fatalf("expected approval overlay in view, got %s", view)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = updated.(Model)
	if !approved {
		t.Fatal("expected approval callback to run")
	}
	if model.pending != nil {
		t.Fatal("expected approval to clear after decision")
	}
}
