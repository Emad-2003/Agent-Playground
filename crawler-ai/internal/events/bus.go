package events

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	EventAll               = "*"
	EventAppStarted        = "app.started"
	EventStatusUpdated     = "status.updated"
	EventTranscriptAdded   = "transcript.added"
	EventTranscriptUpdated = "transcript.updated"
	EventTranscriptReset   = "transcript.reset"
	EventTasksUpdated      = "tasks.updated"
	EventApprovalRequested = "approval.requested"
	EventApprovalCleared   = "approval.cleared"
	EventTokenUsage        = "token.usage"
	EventSessionChanged    = "session.changed"
	EventStreamDelta       = "stream.delta"
	EventSessionBusy       = "session.busy"
)

type Event struct {
	Name      string
	Payload   any
	Timestamp time.Time
}

type Handler func(Event)

type Bus struct {
	mu          sync.RWMutex
	subscribers map[string]map[uint64]Handler
	nextID      atomic.Uint64
}

func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[string]map[uint64]Handler),
	}
}

func (b *Bus) Subscribe(name string, handler Handler) func() {
	if handler == nil {
		return func() {}
	}

	id := b.nextID.Add(1)

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.subscribers[name]; !ok {
		b.subscribers[name] = make(map[uint64]Handler)
	}
	b.subscribers[name][id] = handler

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if subscribers, ok := b.subscribers[name]; ok {
			delete(subscribers, id)
			if len(subscribers) == 0 {
				delete(b.subscribers, name)
			}
		}
	}
}

func (b *Bus) Publish(name string, payload any) {
	event := Event{
		Name:      name,
		Payload:   payload,
		Timestamp: time.Now().UTC(),
	}

	for _, handler := range b.snapshot(name) {
		handler(event)
	}

	if name == EventAll {
		return
	}

	for _, handler := range b.snapshot(EventAll) {
		handler(event)
	}
}

func (b *Bus) snapshot(name string) []Handler {
	b.mu.RLock()
	defer b.mu.RUnlock()

	subscribers := b.subscribers[name]
	if len(subscribers) == 0 {
		return nil
	}

	handlers := make([]Handler, 0, len(subscribers))
	for _, handler := range subscribers {
		handlers = append(handlers, handler)
	}

	return handlers
}
