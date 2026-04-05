package session

// Store persists normalized session state outside the in-memory manager.
// The default implementation is SQLite, while JSON remains available for
// compatibility and migration coverage behind the same seam.
type Store interface {
	Save(PersistedSession) error
	LoadAll() ([]PersistedSession, error)
	LoadSession(id string) (PersistedSession, error)
	LoadSummary(id string) (SessionSummary, error)
	ListSummaries() ([]SessionSummary, error)
	LoadMessages(id string) ([]MessageRecord, error)
	LoadTasks(id string) ([]TaskRecord, error)
	LoadFiles(id string) ([]FileRecord, error)
	LoadUsage(id string) (UsageTotals, error)
	Delete(id string) error
}
