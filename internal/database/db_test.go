package database

import (
	"path/filepath"
	"testing"
)

func TestOpenWriter_CreatesFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenWriter(dbPath)
	if err != nil {
		t.Fatalf("OpenWriter failed: %v", err)
	}
	defer db.Close()

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
	defer db.Close()

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
	db.Close()

	db, err = OpenWriter(dbPath)
	if err != nil {
		t.Fatalf("second open (idempotent migrate): %v", err)
	}
	db.Close()
}

func TestWALMode(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenWriter(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var mode string
	db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if mode != "wal" {
		t.Errorf("expected WAL mode, got %s", mode)
	}
}
