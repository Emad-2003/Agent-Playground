package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSessionLoopQueuesCallsPerSession(t *testing.T) {
	loop := newSessionLoop()
	started := make(chan string, 2)
	releaseFirst := make(chan struct{})
	results := make(chan string, 2)
	order := make([]string, 0, 3)
	var orderMu sync.Mutex

	appendOrder := func(value string) {
		orderMu.Lock()
		defer orderMu.Unlock()
		order = append(order, value)
	}

	go func() {
		err := loop.Run("session-1", context.Background(), func(ctx context.Context) error {
			appendOrder("first-start")
			started <- "first"
			<-releaseFirst
			appendOrder("first-end")
			return nil
		})
		if err == nil {
			results <- "first"
		}
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first call to start")
	}

	go func() {
		err := loop.Run("session-1", context.Background(), func(ctx context.Context) error {
			appendOrder("second")
			started <- "second"
			return nil
		})
		if err == nil {
			results <- "second"
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for loop.Queued("session-1") != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("expected one queued prompt, got %d", loop.Queued("session-1"))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !loop.IsBusy("session-1") {
		t.Fatal("expected session loop to report busy")
	}

	close(releaseFirst)

	seen := map[string]bool{}
	for len(seen) < 2 {
		select {
		case value := <-results:
			seen[value] = true
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for queued calls to complete")
		}
	}

	deadline = time.Now().Add(2 * time.Second)
	for loop.IsBusy("session-1") {
		if time.Now().After(deadline) {
			t.Fatal("expected session loop to become idle")
		}
		time.Sleep(10 * time.Millisecond)
	}

	orderMu.Lock()
	defer orderMu.Unlock()
	if len(order) != 3 {
		t.Fatalf("expected 3 order entries, got %#v", order)
	}
	if order[0] != "first-start" || order[1] != "first-end" || order[2] != "second" {
		t.Fatalf("unexpected execution order: %#v", order)
	}
}

func TestSessionLoopCancelsActiveCall(t *testing.T) {
	loop := newSessionLoop()
	started := make(chan struct{}, 1)
	errCh := make(chan error, 1)

	go func() {
		errCh <- loop.Run("session-1", context.Background(), func(ctx context.Context) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		})
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for call to start")
	}

	if !loop.Cancel("session-1") {
		t.Fatal("expected cancel to report success")
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for canceled call to finish")
	}
}
