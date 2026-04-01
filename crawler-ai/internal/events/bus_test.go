package events

import (
	"sync"
	"testing"
)

func TestPublishNotifiesEventSubscribers(t *testing.T) {
	t.Parallel()

	bus := NewBus()
	called := make(chan Event, 1)
	bus.Subscribe("task.started", func(event Event) {
		called <- event
	})

	bus.Publish("task.started", "payload")

	event := <-called
	if event.Name != "task.started" {
		t.Fatalf("expected task.started, got %s", event.Name)
	}
	if event.Payload != "payload" {
		t.Fatalf("expected payload, got %v", event.Payload)
	}
}

func TestPublishNotifiesWildcardSubscribers(t *testing.T) {
	t.Parallel()

	bus := NewBus()
	called := make(chan Event, 1)
	bus.Subscribe(EventAll, func(event Event) {
		called <- event
	})

	bus.Publish("app.ready", 42)

	event := <-called
	if event.Name != "app.ready" {
		t.Fatalf("expected app.ready, got %s", event.Name)
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	t.Parallel()

	bus := NewBus()
	var mu sync.Mutex
	count := 0
	unsubscribe := bus.Subscribe("task.done", func(Event) {
		mu.Lock()
		defer mu.Unlock()
		count++
	})

	bus.Publish("task.done", nil)
	unsubscribe()
	bus.Publish("task.done", nil)

	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Fatalf("expected one callback before unsubscribe, got %d", count)
	}
}
