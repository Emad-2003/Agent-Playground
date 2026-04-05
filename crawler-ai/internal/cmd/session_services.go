package cmd

import (
	"path/filepath"

	"crawler-ai/internal/oauth"
	"crawler-ai/internal/session"
)

type storedSessionServices struct {
	Manager       *session.Manager
	Lifecycle     *session.LifecycleService
	Queries       *session.QueryService
	History       *session.HistoryQueryService
	PromptHistory *session.PromptHistoryService
	FileHistory   *session.FileHistoryService
	Tasks         *session.TaskQueryService
	Usage         *session.UsageQueryService
}

func loadStoredSessionServices() (storedSessionServices, error) {
	mgr := session.NewManager()
	dataDir := session.DefaultDataDir()
	if dataDir == "" {
		dataDir = filepath.Join(oauth.DefaultConfigDir(), "sessions")
	}
	mgr.SetDataDir(dataDir)
	queries := session.NewQueryService(mgr)
	return storedSessionServices{
		Manager:       mgr,
		Lifecycle:     session.NewLifecycleService(mgr),
		Queries:       queries,
		History:       queries.History(),
		PromptHistory: queries.PromptHistoryService(),
		FileHistory:   queries.FileHistoryService(),
		Tasks:         queries.Tasks(),
		Usage:         queries.UsageService(),
	}, nil
}
