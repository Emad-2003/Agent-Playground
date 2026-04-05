package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	apperrors "crawler-ai/internal/errors"

	_ "modernc.org/sqlite"
)

const sqliteDatabaseFilename = "sessions.db"

const sqliteLatestSchemaVersion = 3

const sqliteInitializationPragmas = `
PRAGMA journal_mode = WAL;
PRAGMA page_size = 4096;
PRAGMA cache_size = -8000;
PRAGMA synchronous = NORMAL;
PRAGMA secure_delete = ON;
`

const sqliteConnectionPragmas = `
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 30000;
`

const sqliteSchemaV1 = `
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	parent_session_id TEXT NOT NULL DEFAULT '',
	workspace_root TEXT NOT NULL,
	created_at_ns INTEGER NOT NULL,
	updated_at_ns INTEGER NOT NULL,
	active_task_ids_json TEXT NOT NULL DEFAULT '[]',
	message_count INTEGER NOT NULL DEFAULT 0,
	task_count INTEGER NOT NULL DEFAULT 0,
	file_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS messages (
	session_id TEXT NOT NULL,
	ordinal INTEGER NOT NULL,
	id TEXT NOT NULL,
	kind TEXT NOT NULL,
	message TEXT NOT NULL,
	created_at_ns INTEGER NOT NULL,
	updated_at_ns INTEGER NOT NULL DEFAULT 0,
	metadata_json TEXT NOT NULL DEFAULT '{}',
	PRIMARY KEY (session_id, ordinal),
	FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS tasks (
	session_id TEXT NOT NULL,
	ordinal INTEGER NOT NULL,
	id TEXT NOT NULL,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	assignee TEXT NOT NULL DEFAULT '',
	depends_on_json TEXT NOT NULL DEFAULT '[]',
	result TEXT NOT NULL DEFAULT '',
	created_at_ns INTEGER NOT NULL DEFAULT 0,
	updated_at_ns INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (session_id, ordinal),
	FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS files (
	session_id TEXT NOT NULL,
	ordinal INTEGER NOT NULL,
	id TEXT NOT NULL,
	kind TEXT NOT NULL DEFAULT '',
	path TEXT NOT NULL,
	tool TEXT NOT NULL DEFAULT '',
	created_at_ns INTEGER NOT NULL,
	updated_at_ns INTEGER NOT NULL DEFAULT 0,
	metadata_json TEXT NOT NULL DEFAULT '{}',
	PRIMARY KEY (session_id, ordinal),
	FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS usage_totals (
	session_id TEXT PRIMARY KEY,
	input_tokens INTEGER NOT NULL DEFAULT 0,
	output_tokens INTEGER NOT NULL DEFAULT 0,
	response_count INTEGER NOT NULL DEFAULT 0,
	priced_responses INTEGER NOT NULL DEFAULT 0,
	unpriced_responses INTEGER NOT NULL DEFAULT 0,
	total_cost REAL NOT NULL DEFAULT 0,
	last_provider TEXT NOT NULL DEFAULT '',
	last_model TEXT NOT NULL DEFAULT '',
	updated_at_ns INTEGER NOT NULL DEFAULT 0,
	FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);
`

const sqliteSchemaMetadataTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	version INTEGER NOT NULL,
	applied_at_ns INTEGER NOT NULL
);
`

var sqliteV2IndexStatements = []string{
	`CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions (updated_at_ns DESC, created_at_ns DESC, id ASC)`,
	`CREATE INDEX IF NOT EXISTS idx_usage_totals_updated_at ON usage_totals (updated_at_ns DESC, session_id ASC)`,
}

var sqliteCoreTableColumns = map[string][]string{
	"sessions": {
		"id",
		"parent_session_id",
		"workspace_root",
		"created_at_ns",
		"updated_at_ns",
		"active_task_ids_json",
		"message_count",
		"task_count",
		"file_count",
	},
	"messages": {
		"session_id",
		"ordinal",
		"id",
		"kind",
		"message",
		"created_at_ns",
		"updated_at_ns",
		"metadata_json",
	},
	"tasks": {
		"session_id",
		"ordinal",
		"id",
		"title",
		"description",
		"status",
		"assignee",
		"depends_on_json",
		"result",
		"created_at_ns",
		"updated_at_ns",
	},
	"files": {
		"session_id",
		"ordinal",
		"id",
		"kind",
		"path",
		"tool",
		"created_at_ns",
		"updated_at_ns",
		"metadata_json",
	},
	"usage_totals": {
		"session_id",
		"input_tokens",
		"output_tokens",
		"response_count",
		"priced_responses",
		"unpriced_responses",
		"total_cost",
		"last_provider",
		"last_model",
		"updated_at_ns",
	},
}

type sqliteMigration struct {
	version int
	name    string
	apply   func(tx *sql.Tx) error
}

var sqliteMigrations = []sqliteMigration{
	{version: 1, name: "create base session schema", apply: applySQLiteMigrationV1},
	{version: 2, name: "record schema version and add indexes", apply: applySQLiteMigrationV2},
	{version: 3, name: "add parent session lineage", apply: applySQLiteMigrationV3},
}

const sqliteSummarySelect = `
SELECT
	s.id,
	s.parent_session_id,
	s.workspace_root,
	s.created_at_ns,
	s.updated_at_ns,
	s.active_task_ids_json,
	s.message_count,
	s.task_count,
	s.file_count,
	COALESCE(u.input_tokens, 0),
	COALESCE(u.output_tokens, 0),
	COALESCE(u.response_count, 0),
	COALESCE(u.priced_responses, 0),
	COALESCE(u.unpriced_responses, 0),
	COALESCE(u.total_cost, 0),
	COALESCE(u.last_provider, ''),
	COALESCE(u.last_model, ''),
	COALESCE(u.updated_at_ns, 0)
FROM sessions s
LEFT JOIN usage_totals u ON u.session_id = s.id
`

type SQLiteStore struct {
	dir string

	mu          sync.Mutex
	path        string
	initialized bool
}

func NewSQLiteStore(dir string) *SQLiteStore {
	trimmed := strings.TrimSpace(dir)
	return &SQLiteStore{
		dir:  trimmed,
		path: filepath.Join(trimmed, sqliteDatabaseFilename),
	}
}

func (s *SQLiteStore) DatabasePath() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *SQLiteStore) StorageHealth() (StorageHealth, error) {
	db, err := s.ensureReady()
	if err != nil {
		return StorageHealth{}, err
	}
	defer db.Close()

	schemaVersion, err := detectSQLiteSchemaVersion(db)
	if err != nil {
		return StorageHealth{}, err
	}

	counts := StorageRecordCounts{}
	if counts.Sessions, err = countSQLiteRows(db, "sessions"); err != nil {
		return StorageHealth{}, err
	}
	if counts.Messages, err = countSQLiteRows(db, "messages"); err != nil {
		return StorageHealth{}, err
	}
	if counts.Tasks, err = countSQLiteRows(db, "tasks"); err != nil {
		return StorageHealth{}, err
	}
	if counts.Files, err = countSQLiteRows(db, "files"); err != nil {
		return StorageHealth{}, err
	}
	if counts.UsageTotals, err = countSQLiteRows(db, "usage_totals"); err != nil {
		return StorageHealth{}, err
	}

	health := StorageHealth{
		Backend:             "sqlite",
		DataDir:             s.dir,
		Path:                s.DatabasePath(),
		SchemaVersion:       schemaVersion,
		LatestSchemaVersion: sqliteLatestSchemaVersion,
		Counts:              counts,
	}
	if schemaVersion != sqliteLatestSchemaVersion {
		health.Notes = append(health.Notes, "sqlite schema is not at the latest supported version")
	}
	return health, nil
}

func applySQLiteMigrationV1(tx *sql.Tx) error {
	if _, err := tx.Exec(sqliteSchemaV1); err != nil {
		return apperrors.Wrap("session.applySQLiteMigrationV1", apperrors.CodeToolFailed, err, "create sqlite session tables")
	}
	if _, err := tx.Exec(sqliteSchemaMetadataTable); err != nil {
		return apperrors.Wrap("session.applySQLiteMigrationV1", apperrors.CodeToolFailed, err, "create sqlite schema metadata table")
	}
	if err := writeSQLiteSchemaVersionTx(tx, 1); err != nil {
		return err
	}
	return nil
}

func applySQLiteMigrationV2(tx *sql.Tx) error {
	if _, err := tx.Exec(sqliteSchemaMetadataTable); err != nil {
		return apperrors.Wrap("session.applySQLiteMigrationV2", apperrors.CodeToolFailed, err, "create sqlite schema metadata table")
	}
	for _, statement := range sqliteV2IndexStatements {
		if _, err := tx.Exec(statement); err != nil {
			return apperrors.Wrap("session.applySQLiteMigrationV2", apperrors.CodeToolFailed, err, "apply sqlite schema index migration")
		}
	}
	if err := writeSQLiteSchemaVersionTx(tx, 2); err != nil {
		return err
	}
	return nil
}

func applySQLiteMigrationV3(tx *sql.Tx) error {
	if _, err := tx.Exec(`ALTER TABLE sessions ADD COLUMN parent_session_id TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
		return apperrors.Wrap("session.applySQLiteMigrationV3", apperrors.CodeToolFailed, err, "add parent_session_id column to sqlite sessions table")
	}
	if err := writeSQLiteSchemaVersionTx(tx, 3); err != nil {
		return err
	}
	return nil
}

func writeSQLiteSchemaVersionTx(tx *sql.Tx, version int) error {
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations (id, version, applied_at_ns) VALUES (1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			version = excluded.version,
			applied_at_ns = excluded.applied_at_ns`,
		version,
		time.Now().UTC().UnixNano(),
	); err != nil {
		return apperrors.Wrap("session.writeSQLiteSchemaVersionTx", apperrors.CodeToolFailed, err, "persist sqlite schema version")
	}
	return nil
}

func ensureSQLiteSchema(db *sql.DB) error {
	currentVersion, err := detectSQLiteSchemaVersion(db)
	if err != nil {
		return err
	}
	if currentVersion > sqliteLatestSchemaVersion {
		return apperrors.New("session.ensureSQLiteSchema", apperrors.CodeToolFailed, "sqlite session schema version is newer than this build supports")
	}
	for _, migration := range sqliteMigrations {
		if migration.version <= currentVersion {
			continue
		}
		if err := applySQLiteMigration(db, migration); err != nil {
			return err
		}
		currentVersion = migration.version
	}
	if currentVersion != sqliteLatestSchemaVersion {
		return apperrors.New("session.ensureSQLiteSchema", apperrors.CodeToolFailed, "sqlite session schema did not reach the expected version")
	}
	return nil
}

func applySQLiteMigration(db *sql.DB, migration sqliteMigration) error {
	tx, err := db.Begin()
	if err != nil {
		return apperrors.Wrap("session.applySQLiteMigration", apperrors.CodeToolFailed, err, "begin sqlite schema migration transaction")
	}
	if err := migration.apply(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return apperrors.Wrap("session.applySQLiteMigration", apperrors.CodeToolFailed, err, "commit sqlite schema migration transaction")
	}
	return nil
}

func detectSQLiteSchemaVersion(db *sql.DB) (int, error) {
	hasMetadataTable, err := sqliteTableExists(db, "schema_migrations")
	if err != nil {
		return 0, err
	}
	if hasMetadataTable {
		version, err := readSQLiteSchemaVersion(db)
		if err != nil {
			return 0, err
		}
		return version, nil
	}

	return detectLegacySQLiteSchemaVersion(db)
}

func readSQLiteSchemaVersion(db *sql.DB) (int, error) {
	var version int
	err := db.QueryRow(`SELECT version FROM schema_migrations WHERE id = 1`).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, apperrors.New("session.readSQLiteSchemaVersion", apperrors.CodeToolFailed, "sqlite schema metadata is missing the current version row")
	}
	if err != nil {
		return 0, apperrors.Wrap("session.readSQLiteSchemaVersion", apperrors.CodeToolFailed, err, "read sqlite schema version")
	}
	if version < 1 {
		return 0, apperrors.New("session.readSQLiteSchemaVersion", apperrors.CodeToolFailed, "sqlite schema version is invalid")
	}
	return version, nil
}

func countSQLiteRows(db *sql.DB, table string) (int, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
		return 0, apperrors.Wrap("session.countSQLiteRows", apperrors.CodeToolFailed, err, "count rows from sqlite table")
	}
	return count, nil
}

func detectLegacySQLiteSchemaVersion(db *sql.DB) (int, error) {
	presentCount := 0
	for tableName, columns := range sqliteCoreTableColumns {
		exists, err := sqliteTableExists(db, tableName)
		if err != nil {
			return 0, err
		}
		if !exists {
			continue
		}
		presentCount++
		if err := sqliteRequireColumns(db, tableName, columns); err != nil {
			return 0, err
		}
	}

	if presentCount == 0 {
		return 0, nil
	}
	if presentCount != len(sqliteCoreTableColumns) {
		return 0, apperrors.New("session.detectLegacySQLiteSchemaVersion", apperrors.CodeToolFailed, "partial sqlite session schema detected; manual recovery is required")
	}
	return 1, nil
}

func sqliteTableExists(db *sql.DB, tableName string) (bool, error) {
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, apperrors.Wrap("session.sqliteTableExists", apperrors.CodeToolFailed, err, "inspect sqlite schema tables")
	}
	return true, nil
}

func sqliteRequireColumns(db *sql.DB, tableName string, required []string) error {
	rows, err := db.Query(`PRAGMA table_info(` + tableName + `)`)
	if err != nil {
		return apperrors.Wrap("session.sqliteRequireColumns", apperrors.CodeToolFailed, err, "inspect sqlite table columns")
	}
	defer rows.Close()

	columns := make(map[string]struct{}, len(required))
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return apperrors.Wrap("session.sqliteRequireColumns", apperrors.CodeToolFailed, err, "scan sqlite table columns")
		}
		columns[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return apperrors.Wrap("session.sqliteRequireColumns", apperrors.CodeToolFailed, err, "iterate sqlite table columns")
	}
	for _, columnName := range required {
		if _, ok := columns[columnName]; !ok {
			return apperrors.Wrap("session.sqliteRequireColumns", apperrors.CodeToolFailed, fmt.Errorf("table %s is missing column %s", tableName, columnName), "sqlite session schema is missing required columns")
		}
	}
	return nil
}

func (s *SQLiteStore) Save(sess PersistedSession) error {
	if s == nil || s.dir == "" {
		return nil
	}
	db, err := s.ensureReady()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return apperrors.Wrap("session.SQLiteStore.Save", apperrors.CodeToolFailed, err, "begin sqlite session transaction")
	}
	if err := savePersistedSessionTx(tx, sess); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return apperrors.Wrap("session.SQLiteStore.Save", apperrors.CodeToolFailed, err, "commit sqlite session transaction")
	}
	_ = NewJSONStore(s.dir).Delete(sess.Session.ID)
	return nil
}

func (s *SQLiteStore) LoadAll() ([]PersistedSession, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}
	db, err := s.ensureReady()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id FROM sessions ORDER BY updated_at_ns DESC, created_at_ns DESC, id ASC`)
	if err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.LoadAll", apperrors.CodeToolFailed, err, "query sqlite sessions")
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, apperrors.Wrap("session.SQLiteStore.LoadAll", apperrors.CodeToolFailed, err, "scan sqlite session id")
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.LoadAll", apperrors.CodeToolFailed, err, "iterate sqlite session ids")
	}

	sessions := make([]PersistedSession, 0, len(ids))
	for _, id := range ids {
		persisted, err := s.loadSessionWithDB(db, id)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, persisted)
	}
	return sessions, nil
}

func (s *SQLiteStore) LoadSession(id string) (PersistedSession, error) {
	if s == nil || s.dir == "" {
		return PersistedSession{}, apperrors.New("session.SQLiteStore.LoadSession", apperrors.CodeInvalidArgument, "session store directory is not configured")
	}
	db, err := s.ensureReady()
	if err != nil {
		return PersistedSession{}, err
	}
	defer db.Close()
	return s.loadSessionWithDB(db, id)
}

func (s *SQLiteStore) LoadSummary(id string) (SessionSummary, error) {
	if s == nil || s.dir == "" {
		return SessionSummary{}, apperrors.New("session.SQLiteStore.LoadSummary", apperrors.CodeInvalidArgument, "session store directory is not configured")
	}
	db, err := s.ensureReady()
	if err != nil {
		return SessionSummary{}, err
	}
	defer db.Close()
	return loadSummaryWithQuery(db.QueryRow(sqliteSummarySelect+` WHERE s.id = ?`, id), "session.SQLiteStore.LoadSummary")
}

func (s *SQLiteStore) ListSummaries() ([]SessionSummary, error) {
	if s == nil || s.dir == "" {
		return nil, nil
	}
	db, err := s.ensureReady()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(sqliteSummarySelect + ` ORDER BY s.updated_at_ns DESC, s.created_at_ns DESC, s.id ASC`)
	if err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.ListSummaries", apperrors.CodeToolFailed, err, "query sqlite session summaries")
	}
	defer rows.Close()

	summaries := make([]SessionSummary, 0)
	for rows.Next() {
		summary, err := loadSummaryWithRows(rows, "session.SQLiteStore.ListSummaries")
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.ListSummaries", apperrors.CodeToolFailed, err, "iterate sqlite session summaries")
	}
	return summaries, nil
}

func (s *SQLiteStore) LoadMessages(id string) ([]MessageRecord, error) {
	if s == nil || s.dir == "" {
		return nil, apperrors.New("session.SQLiteStore.LoadMessages", apperrors.CodeInvalidArgument, "session store directory is not configured")
	}
	db, err := s.ensureReady()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := s.loadSessionRecord(db, id); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT id, kind, message, created_at_ns, updated_at_ns, metadata_json FROM messages WHERE session_id = ? ORDER BY ordinal ASC`, id)
	if err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.LoadMessages", apperrors.CodeToolFailed, err, "query sqlite message records")
	}
	defer rows.Close()

	records := make([]MessageRecord, 0)
	for rows.Next() {
		var record MessageRecord
		var metadataJSON string
		var createdAtNS int64
		var updatedAtNS int64
		if err := rows.Scan(&record.ID, &record.Kind, &record.Message, &createdAtNS, &updatedAtNS, &metadataJSON); err != nil {
			return nil, apperrors.Wrap("session.SQLiteStore.LoadMessages", apperrors.CodeToolFailed, err, "scan sqlite message record")
		}
		record.SessionID = id
		record.CreatedAt = timeFromUnixNano(createdAtNS)
		record.UpdatedAt = timeFromUnixNano(updatedAtNS)
		record.Metadata = unmarshalStringMap(metadataJSON)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.LoadMessages", apperrors.CodeToolFailed, err, "iterate sqlite message records")
	}
	return records, nil
}

func (s *SQLiteStore) LoadTasks(id string) ([]TaskRecord, error) {
	if s == nil || s.dir == "" {
		return nil, apperrors.New("session.SQLiteStore.LoadTasks", apperrors.CodeInvalidArgument, "session store directory is not configured")
	}
	db, err := s.ensureReady()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := s.loadSessionRecord(db, id); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT id, title, description, status, assignee, depends_on_json, result, created_at_ns, updated_at_ns FROM tasks WHERE session_id = ? ORDER BY ordinal ASC`, id)
	if err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.LoadTasks", apperrors.CodeToolFailed, err, "query sqlite task records")
	}
	defer rows.Close()

	records := make([]TaskRecord, 0)
	for rows.Next() {
		var record TaskRecord
		var dependsOnJSON string
		var createdAtNS int64
		var updatedAtNS int64
		if err := rows.Scan(&record.ID, &record.Title, &record.Description, &record.Status, &record.Assignee, &dependsOnJSON, &record.Result, &createdAtNS, &updatedAtNS); err != nil {
			return nil, apperrors.Wrap("session.SQLiteStore.LoadTasks", apperrors.CodeToolFailed, err, "scan sqlite task record")
		}
		record.SessionID = id
		record.DependsOn = unmarshalStringSlice(dependsOnJSON)
		record.CreatedAt = timeFromUnixNano(createdAtNS)
		record.UpdatedAt = timeFromUnixNano(updatedAtNS)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.LoadTasks", apperrors.CodeToolFailed, err, "iterate sqlite task records")
	}
	return records, nil
}

func (s *SQLiteStore) LoadFiles(id string) ([]FileRecord, error) {
	if s == nil || s.dir == "" {
		return nil, apperrors.New("session.SQLiteStore.LoadFiles", apperrors.CodeInvalidArgument, "session store directory is not configured")
	}
	db, err := s.ensureReady()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if _, err := s.loadSessionRecord(db, id); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT id, kind, path, tool, created_at_ns, updated_at_ns, metadata_json FROM files WHERE session_id = ? ORDER BY ordinal ASC`, id)
	if err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.LoadFiles", apperrors.CodeToolFailed, err, "query sqlite file records")
	}
	defer rows.Close()

	records := make([]FileRecord, 0)
	for rows.Next() {
		var record FileRecord
		var metadataJSON string
		var createdAtNS int64
		var updatedAtNS int64
		if err := rows.Scan(&record.ID, &record.Kind, &record.Path, &record.Tool, &createdAtNS, &updatedAtNS, &metadataJSON); err != nil {
			return nil, apperrors.Wrap("session.SQLiteStore.LoadFiles", apperrors.CodeToolFailed, err, "scan sqlite file record")
		}
		record.SessionID = id
		record.CreatedAt = timeFromUnixNano(createdAtNS)
		record.UpdatedAt = timeFromUnixNano(updatedAtNS)
		record.Metadata = unmarshalStringMap(metadataJSON)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.LoadFiles", apperrors.CodeToolFailed, err, "iterate sqlite file records")
	}
	return records, nil
}

func (s *SQLiteStore) LoadUsage(id string) (UsageTotals, error) {
	if s == nil || s.dir == "" {
		return UsageTotals{}, apperrors.New("session.SQLiteStore.LoadUsage", apperrors.CodeInvalidArgument, "session store directory is not configured")
	}
	db, err := s.ensureReady()
	if err != nil {
		return UsageTotals{}, err
	}
	defer db.Close()
	if _, err := s.loadSessionRecord(db, id); err != nil {
		return UsageTotals{}, err
	}
	return s.loadUsageWithDB(db, id)
}

func (s *SQLiteStore) Delete(id string) error {
	if s == nil || s.dir == "" {
		return nil
	}
	db, err := s.ensureReady()
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return apperrors.Wrap("session.SQLiteStore.Delete", apperrors.CodeToolFailed, err, "begin sqlite session delete transaction")
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		return apperrors.Wrap("session.SQLiteStore.Delete", apperrors.CodeToolFailed, err, "delete sqlite session record")
	}
	if err := tx.Commit(); err != nil {
		return apperrors.Wrap("session.SQLiteStore.Delete", apperrors.CodeToolFailed, err, "commit sqlite session delete transaction")
	}
	_ = NewJSONStore(s.dir).Delete(id)
	return nil
}

func (s *SQLiteStore) ensureReady() (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.ensureReady", apperrors.CodeToolFailed, err, "create sqlite session directory")
	}
	db, err := sql.Open("sqlite", s.path)
	if err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.ensureReady", apperrors.CodeToolFailed, err, "open sqlite session database")
	}
	if !s.initialized {
		if _, err := db.Exec(sqliteInitializationPragmas); err != nil {
			_ = db.Close()
			return nil, apperrors.Wrap("session.SQLiteStore.ensureReady", apperrors.CodeToolFailed, err, "initialize sqlite session database pragmas")
		}
	}
	if _, err := db.Exec(sqliteConnectionPragmas); err != nil {
		_ = db.Close()
		return nil, apperrors.Wrap("session.SQLiteStore.ensureReady", apperrors.CodeToolFailed, err, "configure sqlite session database")
	}
	if err := ensureSQLiteSchema(db); err != nil {
		_ = db.Close()
		return nil, apperrors.Wrap("session.SQLiteStore.ensureReady", apperrors.CodeToolFailed, err, "initialize sqlite session schema")
	}
	if err := s.importJSONIfNeeded(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	s.initialized = true
	return db, nil
}

func (s *SQLiteStore) importJSONIfNeeded(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&count); err != nil {
		return apperrors.Wrap("session.SQLiteStore.importJSONIfNeeded", apperrors.CodeToolFailed, err, "count sqlite sessions")
	}
	if count > 0 {
		return nil
	}

	legacySessions, err := NewJSONStore(s.dir).LoadAll()
	if err != nil {
		return apperrors.Wrap("session.SQLiteStore.importJSONIfNeeded", apperrors.CodeToolFailed, err, "load legacy json sessions")
	}
	if len(legacySessions) == 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return apperrors.Wrap("session.SQLiteStore.importJSONIfNeeded", apperrors.CodeToolFailed, err, "begin sqlite import transaction")
	}
	for _, persisted := range legacySessions {
		if err := savePersistedSessionTx(tx, persisted); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return apperrors.Wrap("session.SQLiteStore.importJSONIfNeeded", apperrors.CodeToolFailed, err, "commit sqlite import transaction")
	}
	return nil
}

func (s *SQLiteStore) loadSessionWithDB(db *sql.DB, id string) (PersistedSession, error) {
	record, err := s.loadSessionRecord(db, id)
	if err != nil {
		return PersistedSession{}, err
	}
	messages, err := s.loadMessagesWithDB(db, id)
	if err != nil {
		return PersistedSession{}, err
	}
	tasks, err := s.loadTasksWithDB(db, id)
	if err != nil {
		return PersistedSession{}, err
	}
	files, err := s.loadFilesWithDB(db, id)
	if err != nil {
		return PersistedSession{}, err
	}
	usage, err := s.loadUsageWithDB(db, id)
	if err != nil {
		return PersistedSession{}, err
	}
	return PersistedSession{Session: record, Messages: messages, Tasks: tasks, Files: files, Usage: usage}, nil
}

func (s *SQLiteStore) loadMessagesWithDB(db *sql.DB, id string) ([]MessageRecord, error) {
	rows, err := db.Query(`SELECT id, kind, message, created_at_ns, updated_at_ns, metadata_json FROM messages WHERE session_id = ? ORDER BY ordinal ASC`, id)
	if err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.loadMessagesWithDB", apperrors.CodeToolFailed, err, "query sqlite message records")
	}
	defer rows.Close()

	records := make([]MessageRecord, 0)
	for rows.Next() {
		var record MessageRecord
		var metadataJSON string
		var createdAtNS int64
		var updatedAtNS int64
		if err := rows.Scan(&record.ID, &record.Kind, &record.Message, &createdAtNS, &updatedAtNS, &metadataJSON); err != nil {
			return nil, apperrors.Wrap("session.SQLiteStore.loadMessagesWithDB", apperrors.CodeToolFailed, err, "scan sqlite message record")
		}
		record.SessionID = id
		record.CreatedAt = timeFromUnixNano(createdAtNS)
		record.UpdatedAt = timeFromUnixNano(updatedAtNS)
		record.Metadata = unmarshalStringMap(metadataJSON)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.loadMessagesWithDB", apperrors.CodeToolFailed, err, "iterate sqlite message records")
	}
	return records, nil
}

func (s *SQLiteStore) loadTasksWithDB(db *sql.DB, id string) ([]TaskRecord, error) {
	rows, err := db.Query(`SELECT id, title, description, status, assignee, depends_on_json, result, created_at_ns, updated_at_ns FROM tasks WHERE session_id = ? ORDER BY ordinal ASC`, id)
	if err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.loadTasksWithDB", apperrors.CodeToolFailed, err, "query sqlite task records")
	}
	defer rows.Close()

	records := make([]TaskRecord, 0)
	for rows.Next() {
		var record TaskRecord
		var dependsOnJSON string
		var createdAtNS int64
		var updatedAtNS int64
		if err := rows.Scan(&record.ID, &record.Title, &record.Description, &record.Status, &record.Assignee, &dependsOnJSON, &record.Result, &createdAtNS, &updatedAtNS); err != nil {
			return nil, apperrors.Wrap("session.SQLiteStore.loadTasksWithDB", apperrors.CodeToolFailed, err, "scan sqlite task record")
		}
		record.SessionID = id
		record.DependsOn = unmarshalStringSlice(dependsOnJSON)
		record.CreatedAt = timeFromUnixNano(createdAtNS)
		record.UpdatedAt = timeFromUnixNano(updatedAtNS)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.loadTasksWithDB", apperrors.CodeToolFailed, err, "iterate sqlite task records")
	}
	return records, nil
}

func (s *SQLiteStore) loadFilesWithDB(db *sql.DB, id string) ([]FileRecord, error) {
	rows, err := db.Query(`SELECT id, kind, path, tool, created_at_ns, updated_at_ns, metadata_json FROM files WHERE session_id = ? ORDER BY ordinal ASC`, id)
	if err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.loadFilesWithDB", apperrors.CodeToolFailed, err, "query sqlite file records")
	}
	defer rows.Close()

	records := make([]FileRecord, 0)
	for rows.Next() {
		var record FileRecord
		var metadataJSON string
		var createdAtNS int64
		var updatedAtNS int64
		if err := rows.Scan(&record.ID, &record.Kind, &record.Path, &record.Tool, &createdAtNS, &updatedAtNS, &metadataJSON); err != nil {
			return nil, apperrors.Wrap("session.SQLiteStore.loadFilesWithDB", apperrors.CodeToolFailed, err, "scan sqlite file record")
		}
		record.SessionID = id
		record.CreatedAt = timeFromUnixNano(createdAtNS)
		record.UpdatedAt = timeFromUnixNano(updatedAtNS)
		record.Metadata = unmarshalStringMap(metadataJSON)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, apperrors.Wrap("session.SQLiteStore.loadFilesWithDB", apperrors.CodeToolFailed, err, "iterate sqlite file records")
	}
	return records, nil
}

func (s *SQLiteStore) loadSessionRecord(db *sql.DB, id string) (SessionRecord, error) {
	var record SessionRecord
	var activeTaskIDsJSON string
	var createdAtNS int64
	var updatedAtNS int64
	err := db.QueryRow(`SELECT id, parent_session_id, workspace_root, created_at_ns, updated_at_ns, active_task_ids_json, message_count, task_count, file_count FROM sessions WHERE id = ?`, id).Scan(
		&record.ID,
		&record.ParentSessionID,
		&record.WorkspaceRoot,
		&createdAtNS,
		&updatedAtNS,
		&activeTaskIDsJSON,
		&record.MessageCount,
		&record.TaskCount,
		&record.FileCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, apperrors.New("session.SQLiteStore.loadSessionRecord", apperrors.CodeInvalidArgument, "session not found")
	}
	if err != nil {
		return SessionRecord{}, apperrors.Wrap("session.SQLiteStore.loadSessionRecord", apperrors.CodeToolFailed, err, "query sqlite session record")
	}
	record.CreatedAt = timeFromUnixNano(createdAtNS)
	record.UpdatedAt = timeFromUnixNano(updatedAtNS)
	record.ActiveTaskIDs = unmarshalStringSlice(activeTaskIDsJSON)
	return record, nil
}

func (s *SQLiteStore) loadUsageWithDB(db *sql.DB, id string) (UsageTotals, error) {
	var usage UsageTotals
	var updatedAtNS int64
	err := db.QueryRow(`SELECT input_tokens, output_tokens, response_count, priced_responses, unpriced_responses, total_cost, last_provider, last_model, updated_at_ns FROM usage_totals WHERE session_id = ?`, id).Scan(
		&usage.InputTokens,
		&usage.OutputTokens,
		&usage.ResponseCount,
		&usage.PricedResponses,
		&usage.UnpricedResponses,
		&usage.TotalCost,
		&usage.LastProvider,
		&usage.LastModel,
		&updatedAtNS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return UsageTotals{}, nil
	}
	if err != nil {
		return UsageTotals{}, apperrors.Wrap("session.SQLiteStore.loadUsageWithDB", apperrors.CodeToolFailed, err, "query sqlite usage totals")
	}
	usage.UpdatedAt = timeFromUnixNano(updatedAtNS)
	return usage, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func loadSummaryWithQuery(scanner rowScanner, op string) (SessionSummary, error) {
	var summary SessionSummary
	var activeTaskIDsJSON string
	var createdAtNS int64
	var updatedAtNS int64
	var usageUpdatedAtNS int64
	err := scanner.Scan(
		&summary.ID,
		&summary.ParentSessionID,
		&summary.WorkspaceRoot,
		&createdAtNS,
		&updatedAtNS,
		&activeTaskIDsJSON,
		&summary.MessageCount,
		&summary.TaskCount,
		&summary.FileCount,
		&summary.Usage.InputTokens,
		&summary.Usage.OutputTokens,
		&summary.Usage.ResponseCount,
		&summary.Usage.PricedResponses,
		&summary.Usage.UnpricedResponses,
		&summary.Usage.TotalCost,
		&summary.Usage.LastProvider,
		&summary.Usage.LastModel,
		&usageUpdatedAtNS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionSummary{}, apperrors.New(op, apperrors.CodeInvalidArgument, "session not found")
	}
	if err != nil {
		return SessionSummary{}, apperrors.Wrap(op, apperrors.CodeToolFailed, err, "scan sqlite session summary")
	}
	summary.CreatedAt = timeFromUnixNano(createdAtNS)
	summary.UpdatedAt = timeFromUnixNano(updatedAtNS)
	summary.ActiveTaskIDs = unmarshalStringSlice(activeTaskIDsJSON)
	summary.Usage.UpdatedAt = timeFromUnixNano(usageUpdatedAtNS)
	return summary, nil
}

func loadSummaryWithRows(rows *sql.Rows, op string) (SessionSummary, error) {
	return loadSummaryWithQuery(rows, op)
}

func savePersistedSessionTx(tx *sql.Tx, sess PersistedSession) error {
	sessionRecord := sess.Session
	sessionRecord.MessageCount = len(sess.Messages)
	sessionRecord.TaskCount = len(sess.Tasks)
	sessionRecord.FileCount = len(sess.Files)
	activeTaskIDsJSON, err := marshalJSON(sessionRecord.ActiveTaskIDs)
	if err != nil {
		return apperrors.Wrap("session.savePersistedSessionTx", apperrors.CodeToolFailed, err, "marshal active task ids for sqlite session record")
	}
	if _, err := tx.Exec(
		`INSERT INTO sessions (id, parent_session_id, workspace_root, created_at_ns, updated_at_ns, active_task_ids_json, message_count, task_count, file_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			parent_session_id = excluded.parent_session_id,
			workspace_root = excluded.workspace_root,
			created_at_ns = excluded.created_at_ns,
			updated_at_ns = excluded.updated_at_ns,
			active_task_ids_json = excluded.active_task_ids_json,
			message_count = excluded.message_count,
			task_count = excluded.task_count,
			file_count = excluded.file_count`,
		sessionRecord.ID,
		sessionRecord.ParentSessionID,
		sessionRecord.WorkspaceRoot,
		timeToUnixNano(sessionRecord.CreatedAt),
		timeToUnixNano(sessionRecord.UpdatedAt),
		activeTaskIDsJSON,
		sessionRecord.MessageCount,
		sessionRecord.TaskCount,
		sessionRecord.FileCount,
	); err != nil {
		return apperrors.Wrap("session.savePersistedSessionTx", apperrors.CodeToolFailed, err, "upsert sqlite session record")
	}
	if err := replaceMessageRowsTx(tx, sessionRecord.ID, sess.Messages); err != nil {
		return err
	}
	if err := replaceTaskRowsTx(tx, sessionRecord.ID, sess.Tasks); err != nil {
		return err
	}
	if err := replaceFileRowsTx(tx, sessionRecord.ID, sess.Files); err != nil {
		return err
	}
	if err := replaceUsageRowTx(tx, sessionRecord.ID, sess.Usage); err != nil {
		return err
	}
	return nil
}

func replaceMessageRowsTx(tx *sql.Tx, sessionID string, records []MessageRecord) error {
	if _, err := tx.Exec(`DELETE FROM messages WHERE session_id = ?`, sessionID); err != nil {
		return apperrors.Wrap("session.replaceMessageRowsTx", apperrors.CodeToolFailed, err, "delete sqlite message records")
	}
	for index, record := range records {
		metadataJSON, err := marshalJSON(record.Metadata)
		if err != nil {
			return apperrors.Wrap("session.replaceMessageRowsTx", apperrors.CodeToolFailed, err, "marshal sqlite message metadata")
		}
		if _, err := tx.Exec(
			`INSERT INTO messages (session_id, ordinal, id, kind, message, created_at_ns, updated_at_ns, metadata_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			sessionID,
			index,
			record.ID,
			string(record.Kind),
			record.Message,
			timeToUnixNano(record.CreatedAt),
			timeToUnixNano(record.UpdatedAt),
			metadataJSON,
		); err != nil {
			return apperrors.Wrap("session.replaceMessageRowsTx", apperrors.CodeToolFailed, err, "insert sqlite message record")
		}
	}
	return nil
}

func replaceTaskRowsTx(tx *sql.Tx, sessionID string, records []TaskRecord) error {
	if _, err := tx.Exec(`DELETE FROM tasks WHERE session_id = ?`, sessionID); err != nil {
		return apperrors.Wrap("session.replaceTaskRowsTx", apperrors.CodeToolFailed, err, "delete sqlite task records")
	}
	for index, record := range records {
		dependsOnJSON, err := marshalJSON(record.DependsOn)
		if err != nil {
			return apperrors.Wrap("session.replaceTaskRowsTx", apperrors.CodeToolFailed, err, "marshal sqlite task dependencies")
		}
		if _, err := tx.Exec(
			`INSERT INTO tasks (session_id, ordinal, id, title, description, status, assignee, depends_on_json, result, created_at_ns, updated_at_ns) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			sessionID,
			index,
			record.ID,
			record.Title,
			record.Description,
			string(record.Status),
			string(record.Assignee),
			dependsOnJSON,
			record.Result,
			timeToUnixNano(record.CreatedAt),
			timeToUnixNano(record.UpdatedAt),
		); err != nil {
			return apperrors.Wrap("session.replaceTaskRowsTx", apperrors.CodeToolFailed, err, "insert sqlite task record")
		}
	}
	return nil
}

func replaceFileRowsTx(tx *sql.Tx, sessionID string, records []FileRecord) error {
	if _, err := tx.Exec(`DELETE FROM files WHERE session_id = ?`, sessionID); err != nil {
		return apperrors.Wrap("session.replaceFileRowsTx", apperrors.CodeToolFailed, err, "delete sqlite file records")
	}
	for index, record := range records {
		metadataJSON, err := marshalJSON(record.Metadata)
		if err != nil {
			return apperrors.Wrap("session.replaceFileRowsTx", apperrors.CodeToolFailed, err, "marshal sqlite file metadata")
		}
		if _, err := tx.Exec(
			`INSERT INTO files (session_id, ordinal, id, kind, path, tool, created_at_ns, updated_at_ns, metadata_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			sessionID,
			index,
			record.ID,
			string(record.Kind),
			record.Path,
			record.Tool,
			timeToUnixNano(record.CreatedAt),
			timeToUnixNano(record.UpdatedAt),
			metadataJSON,
		); err != nil {
			return apperrors.Wrap("session.replaceFileRowsTx", apperrors.CodeToolFailed, err, "insert sqlite file record")
		}
	}
	return nil
}

func replaceUsageRowTx(tx *sql.Tx, sessionID string, usage UsageTotals) error {
	if _, err := tx.Exec(
		`INSERT INTO usage_totals (session_id, input_tokens, output_tokens, response_count, priced_responses, unpriced_responses, total_cost, last_provider, last_model, updated_at_ns)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			response_count = excluded.response_count,
			priced_responses = excluded.priced_responses,
			unpriced_responses = excluded.unpriced_responses,
			total_cost = excluded.total_cost,
			last_provider = excluded.last_provider,
			last_model = excluded.last_model,
			updated_at_ns = excluded.updated_at_ns`,
		sessionID,
		usage.InputTokens,
		usage.OutputTokens,
		usage.ResponseCount,
		usage.PricedResponses,
		usage.UnpricedResponses,
		usage.TotalCost,
		usage.LastProvider,
		usage.LastModel,
		timeToUnixNano(usage.UpdatedAt),
	); err != nil {
		return apperrors.Wrap("session.replaceUsageRowTx", apperrors.CodeToolFailed, err, "upsert sqlite usage totals")
	}
	return nil
}

func marshalJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalStringSlice(data string) []string {
	if strings.TrimSpace(data) == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(data), &values); err != nil {
		return nil
	}
	return values
}

func unmarshalStringMap(data string) map[string]string {
	if strings.TrimSpace(data) == "" {
		return nil
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(data), &values); err != nil {
		return nil
	}
	return values
}

func timeToUnixNano(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UTC().UnixNano()
}

func timeFromUnixNano(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(0, value).UTC()
}
