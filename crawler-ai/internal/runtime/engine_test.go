package runtime

import (
	"context"
	"testing"

	apperrors "crawler-ai/internal/errors"
	"crawler-ai/internal/events"
)

func TestExecuteWriteFilePublishesEvents(t *testing.T) {
	t.Parallel()

	bus := events.NewBus()
	started := make(chan struct{}, 1)
	completed := make(chan struct{}, 1)
	bus.Subscribe(EventToolStarted, func(event events.Event) {
		started <- struct{}{}
	})
	bus.Subscribe(EventToolCompleted, func(event events.Event) {
		completed <- struct{}{}
	})

	engine, err := NewEngine(t.TempDir(), bus)
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}

	_, err = engine.Execute(context.Background(), ToolRequest{
		Name:    "write_file",
		Path:    "notes.txt",
		Content: "hello",
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	<-started
	<-completed
}

func TestExecuteRejectsUnknownTool(t *testing.T) {
	t.Parallel()

	engine, err := NewEngine(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected engine error: %v", err)
	}

	_, err = engine.Execute(context.Background(), ToolRequest{Name: "unknown"})
	if err == nil {
		t.Fatal("expected unknown tool error")
	}
	if !apperrors.IsCode(err, apperrors.CodeInvalidArgument) {
		t.Fatalf("expected invalid argument error, got %v", err)
	}
}
