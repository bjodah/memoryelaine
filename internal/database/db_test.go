package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenWriter_CreatesFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenWriter(dbPath)
	if err != nil {
		t.Fatalf("OpenWriter failed: %v", err)
	}
	defer mustClose(t, db)

	// Verify table exists
	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='openai_logs'").Scan(&name)
	if err != nil {
		t.Fatalf("table not created: %v", err)
	}
}

func TestOpenReader_CreatesFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenReader(dbPath)
	if err != nil {
		t.Fatalf("OpenReader failed: %v", err)
	}
	defer mustClose(t, db)

	if err := db.Ping(); err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenWriter(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	mustClose(t, db)

	db, err = OpenWriter(dbPath)
	if err != nil {
		t.Fatalf("second open (idempotent migrate): %v", err)
	}
	mustClose(t, db)
}

func TestWALMode(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenWriter(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, db)

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("scan journal mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("expected WAL mode, got %s", mode)
	}
}

func TestMigrate_LegacySchemaUpgradesColumnsAndFTS(t *testing.T) {
	const legacySchema = `
CREATE TABLE openai_logs (
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
    error TEXT
);

CREATE VIRTUAL TABLE openai_logs_fts USING fts5(
    req_body,
    resp_body,
    content='openai_logs',
    content_rowid='id'
);

CREATE TRIGGER openai_logs_ai AFTER INSERT ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(rowid, req_body, resp_body)
    VALUES (new.id, new.req_body, new.resp_body);
END;

CREATE TRIGGER openai_logs_ad AFTER DELETE ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(openai_logs_fts, rowid, req_body, resp_body)
    VALUES ('delete', old.id, old.req_body, old.resp_body);
END;
`

	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	legacyDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacyDB.Exec(legacySchema); err != nil {
		t.Fatalf("creating legacy schema: %v", err)
	}
	if _, err := legacyDB.Exec(
		`INSERT INTO openai_logs (
			ts_start, client_ip, request_method, request_path, upstream_url,
			req_headers_json, req_body, req_bytes,
			resp_body, resp_bytes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1, "127.0.0.1", "POST", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions",
		"{}", `{"messages":[{"role":"user","content":"legacy unicorn"}]}`, 54,
		`{"choices":[{"message":{"content":"ok"}}]}`, 38,
	); err != nil {
		t.Fatalf("inserting legacy row: %v", err)
	}
	mustClose(t, legacyDB)

	db, err := OpenWriter(dbPath)
	if err != nil {
		t.Fatalf("opening upgraded db: %v", err)
	}
	defer mustClose(t, db)

	cols, err := tableColumns(db, "openai_logs")
	if err != nil {
		t.Fatalf("reading migrated columns: %v", err)
	}
	for _, name := range []string{"parent_id", "chat_hash", "parent_prefix_len", "message_count", "req_text", "resp_text"} {
		if !cols[name] {
			t.Fatalf("expected migrated column %q to exist", name)
		}
	}

	ftsCols, err := tableColumns(db, "openai_logs_fts")
	if err != nil {
		t.Fatalf("reading FTS columns: %v", err)
	}
	if !ftsCols["req_text"] || !ftsCols["resp_text"] {
		t.Fatalf("expected migrated FTS columns req_text/resp_text, got %#v", ftsCols)
	}

	reader := NewLogReader(db)
	entry, err := reader.GetLatest()
	if err != nil {
		t.Fatalf("reading migrated row: %v", err)
	}
	if entry.ID != 1 {
		t.Fatalf("expected migrated row ID 1, got %d", entry.ID)
	}

	search := "unicorn"
	results, err := reader.QuerySummaries(QueryFilter{Search: &search, Limit: 10})
	if err != nil {
		t.Fatalf("FTS query after migration failed: %v", err)
	}
	if len(results) != 1 || results[0].ID != 1 {
		t.Fatalf("expected migrated FTS row [1], got %+v", results)
	}
}
