package app

import (
	"crawler-ai/internal/domain"
	"crawler-ai/internal/events"
)

type transcriptEventPublisher interface {
	PublishTranscriptAdded(sessionID string, entry domain.TranscriptEntry)
	PublishTranscriptUpdated(sessionID string, entry domain.TranscriptEntry)
	PublishTranscriptReset(sessionID string, entries []domain.TranscriptEntry)
}

type taskEventPublisher interface {
	PublishTasksUpdated(sessionID string, tasks []domain.Task)
}

type activeSessionEventPublisher struct {
	bus           *events.Bus
	activeSession activeSessionResolver
}

func newActiveSessionEventPublisher(bus *events.Bus, activeSession activeSessionResolver) *activeSessionEventPublisher {
	return &activeSessionEventPublisher{bus: bus, activeSession: activeSession}
}

func (p *activeSessionEventPublisher) PublishTranscriptAdded(sessionID string, entry domain.TranscriptEntry) {
	if !p.isActive(sessionID) {
		return
	}
	p.bus.Publish(events.EventTranscriptAdded, cloneTranscriptEntry(entry))
}

func (p *activeSessionEventPublisher) PublishTranscriptUpdated(sessionID string, entry domain.TranscriptEntry) {
	if !p.isActive(sessionID) {
		return
	}
	p.bus.Publish(events.EventTranscriptUpdated, cloneTranscriptEntry(entry))
}

func (p *activeSessionEventPublisher) PublishTranscriptReset(sessionID string, entries []domain.TranscriptEntry) {
	if !p.isActive(sessionID) {
		return
	}
	p.bus.Publish(events.EventTranscriptReset, cloneTranscriptEntries(entries))
}

func (p *activeSessionEventPublisher) PublishTasksUpdated(sessionID string, tasks []domain.Task) {
	if !p.isActive(sessionID) {
		return
	}
	p.bus.Publish(events.EventTasksUpdated, cloneTasks(tasks))
}

func (p *activeSessionEventPublisher) isActive(sessionID string) bool {
	if p == nil || p.bus == nil || p.activeSession == nil {
		return false
	}
	return sessionID == p.activeSession()
}
