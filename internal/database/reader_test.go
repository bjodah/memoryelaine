package database

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenWriter(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func insertTestEntries(t *testing.T, db *sql.DB, n int) {
	t.Helper()
	stmt, err := db.Prepare(insertSQL)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, stmt)
	now := time.Now().UnixMilli()
	for i := 0; i < n; i++ {
		code := 200
		if i%3 == 0 {
			code = 500
		}
		tsEnd := now + int64(i*100)
		dur := int64(i * 100)
		path := "/v1/chat/completions"
		if i%2 == 0 {
			path = "/v1/completions"
		}
		if _, err := stmt.Exec(
			now+int64(i), &tsEnd, &dur, "127.0.0.1",
			"POST", path, "https://api.openai.com"+path, &code,
			"{}", "{}",
			`{"prompt":"test"}`, false, 17,
			`{"text":"hello"}`, false, 16,
			nil,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReaderQuery_NoFilter(t *testing.T) {
	db := setupTestDB(t)
	defer mustClose(t, db)
	insertTestEntries(t, db, 10)

	r := NewLogReader(db)
	entries, err := r.Query(DefaultQueryFilter())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 10 {
		t.Errorf("expected 10, got %d", len(entries))
	}
	// Verify order (newest first)
	if len(entries) >= 2 && entries[0].TsStart < entries[1].TsStart {
		t.Error("expected descending order")
	}
}

func TestReaderQuery_StatusFilter(t *testing.T) {
	db := setupTestDB(t)
	defer mustClose(t, db)
	insertTestEntries(t, db, 9)

	r := NewLogReader(db)
	status := 500
	f := DefaultQueryFilter()
	f.StatusCode = &status
	entries, err := r.Query(f)
	if err != nil {
		t.Fatal(err)
	}
	// indices 0,3,6 have status 500
	if len(entries) != 3 {
		t.Errorf("expected 3 entries with status 500, got %d", len(entries))
	}
}

func TestReaderQuery_PathFilter(t *testing.T) {
	db := setupTestDB(t)
	defer mustClose(t, db)
	insertTestEntries(t, db, 10)

	r := NewLogReader(db)
	path := "/v1/completions"
	f := DefaultQueryFilter()
	f.Path = &path
	entries, err := r.Query(f)
	if err != nil {
		t.Fatal(err)
	}
	// even indices: 0,2,4,6,8
	if len(entries) != 5 {
		t.Errorf("expected 5, got %d", len(entries))
	}
}

func TestReaderGetByID(t *testing.T) {
	db := setupTestDB(t)
	defer mustClose(t, db)
	insertTestEntries(t, db, 1)

	r := NewLogReader(db)
	e, err := r.GetByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if e.RequestMethod != "POST" {
		t.Errorf("expected POST, got %s", e.RequestMethod)
	}
}

func TestReaderGetByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer mustClose(t, db)

	r := NewLogReader(db)
	_, err := r.GetByID(999)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows, got %v", err)
	}
}

func TestReaderCount(t *testing.T) {
	db := setupTestDB(t)
	defer mustClose(t, db)
	insertTestEntries(t, db, 7)

	r := NewLogReader(db)
	count, err := r.Count(DefaultQueryFilter())
	if err != nil {
		t.Fatal(err)
	}
	if count != 7 {
		t.Errorf("expected 7, got %d", count)
	}
}

func TestReaderDeleteBefore(t *testing.T) {
	db := setupTestDB(t)
	defer mustClose(t, db)
	insertTestEntries(t, db, 10)

	r := NewLogReader(db)
	// Delete everything before a point in the future
	deleted, err := r.DeleteBefore(time.Now().UnixMilli() + 100000)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 10 {
		t.Errorf("expected 10 deleted, got %d", deleted)
	}

	count, err := r.Count(DefaultQueryFilter())
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 remaining, got %d", count)
	}
}

func TestReaderQuery_SearchFilter(t *testing.T) {
	db := setupTestDB(t)
	defer mustClose(t, db)
	insertTestEntries(t, db, 5)

	r := NewLogReader(db)
	search := "hello"
	f := DefaultQueryFilter()
	f.Search = &search
	entries, err := r.Query(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 (all match resp_body), got %d", len(entries))
	}
}

func TestReaderQuery_FTSSearch(t *testing.T) {
	db := setupTestDB(t)
	defer mustClose(t, db)

	stmt, err := db.Prepare(insertSQL)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, stmt)

	now := time.Now().UnixMilli()
	code := 200

	// Entry 1: req_body contains "unicorn"
	if _, err := stmt.Exec(
		now, nil, nil, "127.0.0.1",
		"POST", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions", &code,
		"{}", "{}",
		`{"prompt":"tell me about unicorn"}`, false, 30,
		`{"text":"response one"}`, false, 20,
		nil,
	); err != nil {
		t.Fatal(err)
	}

	// Entry 2: resp_body contains "dragon"
	if _, err := stmt.Exec(
		now+1, nil, nil, "127.0.0.1",
		"POST", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions", &code,
		"{}", "{}",
		`{"prompt":"something else"}`, false, 25,
		`{"text":"here be dragon"}`, false, 22,
		nil,
	); err != nil {
		t.Fatal(err)
	}

	// Entry 3: neither contains the search terms
	if _, err := stmt.Exec(
		now+2, nil, nil, "127.0.0.1",
		"POST", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions", &code,
		"{}", "{}",
		`{"prompt":"boring"}`, false, 18,
		`{"text":"nothing special"}`, false, 24,
		nil,
	); err != nil {
		t.Fatal(err)
	}

	r := NewLogReader(db)

	// Search for "unicorn" — should match only entry 1
	search := "unicorn"
	f := DefaultQueryFilter()
	f.Search = &search
	entries, err := r.Query(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("search 'unicorn': expected 1 result, got %d", len(entries))
	}

	// Search for "dragon" — should match only entry 2
	search = "dragon"
	f.Search = &search
	entries, err = r.Query(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("search 'dragon': expected 1 result, got %d", len(entries))
	}

	// Search for a term not present — should return 0 results
	search = "nonexistent"
	f.Search = &search
	entries, err = r.Query(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("search 'nonexistent': expected 0 results, got %d", len(entries))
	}
}

func TestReaderQuery_FTSSpecialChars(t *testing.T) {
	db := setupTestDB(t)
	reader := NewLogReader(db)

	// Insert a test entry with all required non-null fields
	_, err := db.Exec(`INSERT INTO openai_logs (ts_start,client_ip,request_method,request_path,upstream_url,req_headers_json,req_body,req_bytes,resp_bytes)
		VALUES (?,?,?,?,?,?,?,?,?)`, 1000, "127.0.0.1", "POST", "/v1/chat/completions", "http://api.openai.com/v1/chat/completions", "{}", `{"prompt":"test"}`, 18, 0)
	if err != nil {
		t.Fatal(err)
	}

	// These inputs would cause FTS5 syntax errors without sanitization
	problematic := []string{
		`he said "hello`,
		`test*wildcard`,
		`(parentheses)`,
		`a^b`,
		`OR`,
		`"unterminated`,
	}
	for _, q := range problematic {
		filter := DefaultQueryFilter()
		filter.Search = &q
		// Should not panic or error — may return 0 results, that's fine
		_, err := reader.Query(filter)
		if err != nil {
			t.Errorf("FTS query with %q should not error, got: %v", q, err)
		}
	}
}
