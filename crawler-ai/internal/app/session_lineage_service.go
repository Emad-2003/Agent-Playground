package app

import (
	"crawler-ai/internal/session"
)

type sessionLineageService interface {
	CreateChild(parentSessionID, childSessionID string) (session.Session, error)
}

func newSessionLineageService(sessions *session.Manager) *session.LifecycleService {
	return session.NewLifecycleService(sessions)
}
