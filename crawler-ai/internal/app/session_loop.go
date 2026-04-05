package app

import (
	"context"
	"sync"
)

type sessionLoop struct {
	mu       sync.Mutex
	sessions map[string]*sessionLoopState
}

type sessionLoopState struct {
	cancel context.CancelFunc
	queue  []sessionLoopCall
}

type sessionLoopCall struct {
	ctx  context.Context
	fn   func(context.Context) error
	done chan error
}

func newSessionLoop() *sessionLoop {
	return &sessionLoop{sessions: make(map[string]*sessionLoopState)}
}

func (l *sessionLoop) Run(sessionID string, ctx context.Context, fn func(context.Context) error) error {
	call := sessionLoopCall{
		ctx:  ctx,
		fn:   fn,
		done: make(chan error, 1),
	}

	l.mu.Lock()
	state := l.sessions[sessionID]
	if state == nil {
		state = &sessionLoopState{}
		l.sessions[sessionID] = state
		l.mu.Unlock()
		go l.run(sessionID, call)
		return <-call.done
	}
	state.queue = append(state.queue, call)
	l.mu.Unlock()
	return <-call.done
}

func (l *sessionLoop) run(sessionID string, current sessionLoopCall) {
	for {
		runCtx, cancel := context.WithCancel(current.ctx)
		l.mu.Lock()
		state := l.sessions[sessionID]
		if state != nil {
			state.cancel = cancel
		}
		l.mu.Unlock()

		err := current.fn(runCtx)
		cancel()
		current.done <- err

		l.mu.Lock()
		state = l.sessions[sessionID]
		if state == nil {
			l.mu.Unlock()
			return
		}
		state.cancel = nil
		if len(state.queue) == 0 {
			delete(l.sessions, sessionID)
			l.mu.Unlock()
			return
		}
		current = state.queue[0]
		state.queue = state.queue[1:]
		l.mu.Unlock()
	}
}

func (l *sessionLoop) IsBusy(sessionID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.sessions[sessionID]
	return ok
}

func (l *sessionLoop) Queued(sessionID string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.sessions[sessionID]
	if state == nil {
		return 0
	}
	return len(state.queue)
}

func (l *sessionLoop) Cancel(sessionID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.sessions[sessionID]
	if state == nil || state.cancel == nil {
		return false
	}
	state.cancel()
	return true
}
