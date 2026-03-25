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

func TestWriterChatEnrichment(t *testing.T) {
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

	respBody := `{"choices":[{"message":{"content":"Hello!"}}]}`
	entry := LogEntry{
		TsStart:        time.Now().UnixMilli(),
		ClientIP:       "127.0.0.1",
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		UpstreamURL:    "https://api.openai.com/v1/chat/completions",
		ReqHeadersJSON: "{}",
		ReqBody:        `{"model":"gpt-4","messages":[{"role":"system","content":"Be helpful"},{"role":"user","content":"Hi"}]}`,
		ReqBytes:       80,
		RespBody:       &respBody,
		RespBytes:      40,
	}

	if !w.Enqueue(entry) {
		t.Fatal("enqueue failed")
	}

	time.Sleep(200 * time.Millisecond)
	cancel()

	r := NewLogReader(db)
	e, err := r.GetByID(1)
	if err != nil {
		t.Fatal(err)
	}

	// chat_hash should be populated
	if e.ChatHash == nil || *e.ChatHash == "" {
		t.Error("expected chat_hash to be populated")
	}

	// message_count should be 2
	if e.MessageCount == nil || *e.MessageCount != 2 {
		t.Errorf("expected message_count 2, got %v", e.MessageCount)
	}

	// req_text should contain "system:" and "user:"
	if e.ReqText == nil {
		t.Error("expected req_text to be populated")
	} else {
		if !contains(*e.ReqText, "system:") || !contains(*e.ReqText, "user:") {
			t.Errorf("unexpected req_text: %q", *e.ReqText)
		}
	}

	// resp_text should be "Hello!"
	if e.RespText == nil {
		t.Error("expected resp_text to be populated")
	} else if *e.RespText != "Hello!" {
		t.Errorf("expected resp_text 'Hello!', got %q", *e.RespText)
	}

	// parent_id should be nil (first entry)
	if e.ParentID != nil {
		t.Errorf("expected no parent for first entry, got %v", *e.ParentID)
	}
}

func TestWriterChatLineage(t *testing.T) {
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

	// Turn 1: system + user
	resp1 := `{"choices":[{"message":{"content":"Hi there!"}}]}`
	e1 := LogEntry{
		TsStart:        time.Now().UnixMilli(),
		ClientIP:       "127.0.0.1",
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		UpstreamURL:    "https://api.openai.com/v1/chat/completions",
		ReqHeadersJSON: "{}",
		ReqBody:        `{"model":"gpt-4","messages":[{"role":"system","content":"Be helpful"},{"role":"user","content":"Hello"}]}`,
		ReqBytes:       80,
		RespBody:       &resp1,
		RespBytes:      40,
	}

	if !w.Enqueue(e1) {
		t.Fatal("enqueue e1 failed")
	}
	time.Sleep(200 * time.Millisecond)

	// Turn 2: system + user + assistant + user (continues the conversation)
	resp2 := `{"choices":[{"message":{"content":"2+2 is 4"}}]}`
	e2 := LogEntry{
		TsStart:        time.Now().UnixMilli(),
		ClientIP:       "127.0.0.1",
		RequestMethod:  "POST",
		RequestPath:    "/v1/chat/completions",
		UpstreamURL:    "https://api.openai.com/v1/chat/completions",
		ReqHeadersJSON: "{}",
		ReqBody:        `{"model":"gpt-4","messages":[{"role":"system","content":"Be helpful"},{"role":"user","content":"Hello"},{"role":"assistant","content":"Hi there!"},{"role":"user","content":"What is 2+2?"}]}`,
		ReqBytes:       150,
		RespBody:       &resp2,
		RespBytes:      40,
	}

	if !w.Enqueue(e2) {
		t.Fatal("enqueue e2 failed")
	}
	time.Sleep(200 * time.Millisecond)
	cancel()

	r := NewLogReader(db)

	// Verify turn 2 has parent_id pointing to turn 1
	entry2, err := r.GetByID(2)
	if err != nil {
		t.Fatal(err)
	}
	if entry2.ParentID == nil {
		t.Fatal("expected parent_id to be set on turn 2")
	}
	if *entry2.ParentID != 1 {
		t.Errorf("expected parent_id 1, got %d", *entry2.ParentID)
	}
	if entry2.ParentPrefixLen == nil || *entry2.ParentPrefixLen != 2 {
		t.Errorf("expected parent_prefix_len 2, got %v", entry2.ParentPrefixLen)
	}
	if entry2.MessageCount == nil || *entry2.MessageCount != 4 {
		t.Errorf("expected message_count 4, got %v", entry2.MessageCount)
	}
}

func TestWriterNonChatNotEnriched(t *testing.T) {
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
		RequestPath:    "/v1/embeddings",
		UpstreamURL:    "https://api.openai.com/v1/embeddings",
		ReqHeadersJSON: "{}",
		ReqBody:        `{"input":"some text"}`,
		ReqBytes:       20,
	}

	if !w.Enqueue(entry) {
		t.Fatal("enqueue failed")
	}

	time.Sleep(200 * time.Millisecond)
	cancel()

	r := NewLogReader(db)
	e, err := r.GetByID(1)
	if err != nil {
		t.Fatal(err)
	}
	if e.ChatHash != nil {
		t.Error("expected no chat_hash for non-chat entry")
	}
	if e.ReqText != nil {
		t.Error("expected no req_text for non-chat entry")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
