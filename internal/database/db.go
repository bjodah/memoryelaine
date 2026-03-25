package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"runtime"

	_ "github.com/mattn/go-sqlite3"
)

const schema = `
CREATE TABLE IF NOT EXISTS openai_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts_start INTEGER NOT NULL,
    ts_end INTEGER,
    duration_ms INTEGER,
    client_ip TEXT,
    request_method TEXT NOT NULL,
    request_path TEXT NOT NULL,
    upstream_url TEXT NOT NULL,
    status_code INTEGER,
    req_headers_json TEXT,
    resp_headers_json TEXT,
    req_body TEXT,
    req_truncated BOOLEAN DEFAULT 0,
    req_bytes INTEGER,
    resp_body TEXT,
    resp_truncated BOOLEAN DEFAULT 0,
    resp_bytes INTEGER,
    error TEXT,
    parent_id INTEGER REFERENCES openai_logs(id),
    chat_hash TEXT,
    parent_prefix_len INTEGER,
    message_count INTEGER,
    req_text TEXT,
    resp_text TEXT
);

CREATE INDEX IF NOT EXISTS idx_ts_start ON openai_logs(ts_start);
CREATE INDEX IF NOT EXISTS idx_status_code_ts ON openai_logs(status_code, ts_start);
CREATE INDEX IF NOT EXISTS idx_path_ts ON openai_logs(request_path, ts_start);
CREATE INDEX IF NOT EXISTS idx_chat_hash ON openai_logs(chat_hash);
CREATE INDEX IF NOT EXISTS idx_parent_id ON openai_logs(parent_id);
`

const ftsSchema = `
CREATE VIRTUAL TABLE IF NOT EXISTS openai_logs_fts USING fts5(
    req_text,
    resp_text,
    content='openai_logs',
    content_rowid='id'
);

-- Triggers use COALESCE so NULL sidecar columns fall back to raw bodies.
CREATE TRIGGER IF NOT EXISTS openai_logs_ai AFTER INSERT ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(rowid, req_text, resp_text)
    VALUES (new.id, COALESCE(new.req_text, new.req_body), COALESCE(new.resp_text, new.resp_body));
END;

CREATE TRIGGER IF NOT EXISTS openai_logs_ad AFTER DELETE ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(openai_logs_fts, rowid, req_text, resp_text)
    VALUES ('delete', old.id, COALESCE(old.req_text, old.req_body), COALESCE(old.resp_text, old.resp_body));
END;
`

// OpenWriter opens a DB handle optimized for the single async writer.
func OpenWriter(dbPath string) (*sql.DB, error) {
	db, err := openAndMigrate(dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// OpenReader opens a DB handle optimized for concurrent readers.
func OpenReader(dbPath string) (*sql.DB, error) {
	db, err := openAndMigrate(dbPath)
	if err != nil {
		return nil, err
	}
	maxConns := runtime.NumCPU()
	if maxConns > 4 {
		maxConns = 4
	}
	db.SetMaxOpenConns(maxConns)
	return db, nil
}

func openAndMigrate(dbPath string) (*sql.DB, error) {
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := migrate(db); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("migrating database: %w (close error: %v)", err, closeErr)
		}
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	if err := db.Ping(); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("pinging database: %w (close error: %v)", err, closeErr)
		}
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("executing schema: %w", err)
	}
	if err := migrateFTS(db); err != nil {
		return fmt.Errorf("executing FTS schema: %w", err)
	}
	return nil
}

func migrateFTS(db *sql.DB) error {
	_, err := db.Exec(ftsSchema)
	if err != nil {
		return fmt.Errorf("creating FTS table: %w", err)
	}

	// Check if FTS table needs to be populated from existing data.
	// If the main table has rows but FTS is empty, rebuild.
	var mainCount, ftsCount int64
	if err := db.QueryRow("SELECT COUNT(*) FROM openai_logs").Scan(&mainCount); err != nil {
		return fmt.Errorf("counting main table: %w", err)
	}
	if mainCount == 0 {
		return nil
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM openai_logs_fts").Scan(&ftsCount); err != nil {
		return fmt.Errorf("counting FTS table: %w", err)
	}
	if ftsCount > 0 {
		return nil
	}

	// Rebuild FTS index from existing data using COALESCE so that NULL
	// sidecar columns fall back to the raw body columns.
	// We cannot use the built-in 'rebuild' command because it reads columns
	// directly from the content table, bypassing COALESCE.
	slog.Info("rebuilding FTS index from existing data", "rows", mainCount)
	if err := rebuildFTS(db); err != nil {
		return fmt.Errorf("rebuilding FTS index: %w", err)
	}
	slog.Info("FTS index rebuild complete")
	return nil
}

func rebuildFTS(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("DELETE FROM openai_logs_fts"); err != nil {
		return fmt.Errorf("clearing FTS table: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO openai_logs_fts(rowid, req_text, resp_text)
		SELECT id, COALESCE(req_text, req_body), COALESCE(resp_text, resp_body)
		FROM openai_logs`); err != nil {
		return fmt.Errorf("populating FTS table: %w", err)
	}
	return tx.Commit()
}
