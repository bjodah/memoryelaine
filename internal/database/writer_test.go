package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestWriterEnqueueAndInsert(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenWriter(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, db)

	w, err := NewLogWriter(db, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, w)

	ctx, cancel := context.WithCancel(context.Background())
	go w.Run(ctx)

	entry := LogEntry{
		TsStart:        time.Now().UnixMilli(),
		ClientIP:       "127.0.0.1",
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		UpstreamURL:    "https://api.openai.com/v1/chat/completions",
		ReqHeadersJSON: "{}",
		ReqBody:        `{"model":"gpt-4"}`,
		ReqBytes:       18,
	}

	if !w.Enqueue(entry) {
		t.Fatal("enqueue failed")
	}

	// Give the worker time to process
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Verify it was inserted
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM openai_logs").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestWriterDropsWhenFull(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenWriter(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, db)

	w, err := NewLogWriter(db, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, w)

	// Don't start the worker — channel will fill up
	entry := LogEntry{
		TsStart:        time.Now().UnixMilli(),
		RequestMethod:  "POST",
		RequestPath:    "/test",
		UpstreamURL:    "http://localhost",
		ReqHeadersJSON: "{}",
	}

	w.Enqueue(entry) // fills the channel (capacity 1)
	w.Enqueue(entry) // should drop

	if w.DroppedCount() != 1 {
		t.Errorf("expected 1 drop, got %d", w.DroppedCount())
	}
}

func TestWriterDrainsOnShutdown(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := OpenWriter(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, db)

	w, err := NewLogWriter(db, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer mustClose(t, w)

	// Enqueue entries before starting the worker
	for i := 0; i < 5; i++ {
		w.Enqueue(LogEntry{
			TsStart:        time.Now().UnixMilli(),
			RequestMethod:  "GET",
			RequestPath:    "/test",
			UpstreamURL:    "http://localhost",
			ReqHeadersJSON: "{}",
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately — Run should drain

	w.Run(ctx)

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM openai_logs").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Errorf("expected 5 drained rows, got %d", count)
	}
}
