package database

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"sync/atomic"

	"memoryelaine/internal/chat"
)

// LogWriter consumes LogEntry values from a channel and INSERTs them.
type LogWriter struct {
	db         *sql.DB
	queue      chan LogEntry
	dropped    atomic.Int64
	insertStmt *sql.Stmt
	last       lastBodies
	// SSEExtractor, when set, extracts the assistant text from an SSE
	// response body. Injected to avoid an import cycle with streamview.
	SSEExtractor func(entry *LogEntry) string
}

type bodySnapshot struct {
	body  string
	valid bool
	stale bool
}

type lastBodies struct {
	mu       sync.RWMutex
	request  bodySnapshot
	response bodySnapshot
}

const insertSQL = `INSERT INTO openai_logs (
    ts_start, ts_end, duration_ms, client_ip,
    request_method, request_path, upstream_url, status_code,
    req_headers_json, resp_headers_json,
    req_body, req_truncated, req_bytes,
    resp_body, resp_truncated, resp_bytes,
    error,
    parent_id, chat_hash, parent_prefix_len, message_count,
    req_text, resp_text
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

// NewLogWriter creates a LogWriter with a bounded channel of given capacity.
func NewLogWriter(db *sql.DB, queueSize int) (*LogWriter, error) {
	stmt, err := db.Prepare(insertSQL)
	if err != nil {
		return nil, err
	}
	return &LogWriter{
		db:         db,
		queue:      make(chan LogEntry, queueSize),
		insertStmt: stmt,
	}, nil
}

// Enqueue attempts to send a LogEntry to the worker. Returns true if enqueued.
func (w *LogWriter) Enqueue(entry LogEntry) bool {
	select {
	case w.queue <- entry:
		return true
	default:
		w.dropped.Add(1)
		slog.Error("log queue full, dropping entry", "path", entry.RequestPath)
		return false
	}
}

// Run starts the background INSERT loop. Blocks until ctx is cancelled,
// then drains remaining items from the channel.
func (w *LogWriter) Run(ctx context.Context) {
	for {
		select {
		case entry := <-w.queue:
			w.enrichChat(&entry)
			w.insert(entry)
		case <-ctx.Done():
			w.drain()
			return
		}
	}
}

// DroppedCount returns the number of dropped log entries.
func (w *LogWriter) DroppedCount() int64 {
	return w.dropped.Load()
}

// SetLastRequest stores the most recently captured request body.
func (w *LogWriter) SetLastRequest(body string) {
	w.last.mu.Lock()
	defer w.last.mu.Unlock()
	w.last.request = bodySnapshot{body: body, valid: true}
}

// LastRequest returns the most recently captured request body.
func (w *LogWriter) LastRequest() (string, bool, bool) {
	w.last.mu.RLock()
	defer w.last.mu.RUnlock()
	return w.last.request.body, w.last.request.valid, w.last.request.stale
}

// SetLastResponse stores the most recently captured response body.
func (w *LogWriter) SetLastResponse(body string) {
	w.last.mu.Lock()
	defer w.last.mu.Unlock()
	w.last.response = bodySnapshot{body: body, valid: true}
}

// LastResponse returns the most recently captured response body.
func (w *LogWriter) LastResponse() (string, bool, bool) {
	w.last.mu.RLock()
	defer w.last.mu.RUnlock()
	return w.last.response.body, w.last.response.valid, w.last.response.stale
}

// MarkLastBodiesStale marks the cached last request/response bodies as stale.
func (w *LogWriter) MarkLastBodiesStale() {
	w.last.mu.Lock()
	defer w.last.mu.Unlock()
	w.last.request.stale = true
	w.last.response.stale = true
}

// Close closes the prepared statement.
func (w *LogWriter) Close() error {
	return w.insertStmt.Close()
}

func (w *LogWriter) insert(entry LogEntry) {
	_, err := w.insertStmt.Exec(
		entry.TsStart, entry.TsEnd, entry.DurationMs, entry.ClientIP,
		entry.RequestMethod, entry.RequestPath, entry.UpstreamURL, entry.StatusCode,
		entry.ReqHeadersJSON, entry.RespHeadersJSON,
		entry.ReqBody, entry.ReqTruncated, entry.ReqBytes,
		entry.RespBody, entry.RespTruncated, entry.RespBytes,
		entry.Error,
		entry.ParentID, entry.ChatHash, entry.ParentPrefixLen, entry.MessageCount,
		entry.ReqText, entry.RespText,
	)
	if err != nil {
		slog.Error("failed to insert log entry", "error", err)
	}
}

func (w *LogWriter) drain() {
	for {
		select {
		case entry := <-w.queue:
			w.enrichChat(&entry)
			w.insert(entry)
		default:
			return
		}
	}
}

const maxPrefixAttempts = 5

// enrichChat populates chat-specific fields (req_text, resp_text, chat_hash,
// parent_id, parent_prefix_len, message_count) for chat/completions requests.
func (w *LogWriter) enrichChat(entry *LogEntry) {
	if !chat.IsChatPath(entry.RequestPath) {
		return
	}
	if entry.ReqBody == "" || entry.ReqTruncated {
		return
	}

	msgs, err := chat.ParseMessages(entry.ReqBody)
	if err != nil {
		slog.Debug("chat enrichment: failed to parse request messages", "error", err)
		return
	}

	// Sidecar: req_text
	if reqText := chat.ExtractRequestText(msgs); reqText != "" {
		entry.ReqText = &reqText
	}

	// Sidecar: resp_text (try non-streaming JSON first, then SSE assembly)
	if entry.RespBody != nil && *entry.RespBody != "" && !entry.RespTruncated {
		if respText := chat.ExtractAssistantResponse(*entry.RespBody); respText != "" {
			entry.RespText = &respText
		} else if w.SSEExtractor != nil {
			if assembled := w.SSEExtractor(entry); assembled != "" {
				entry.RespText = &assembled
			}
		}
	}

	n := len(msgs)
	entry.MessageCount = &n

	if n == 0 {
		return
	}

	// Compute full conversation hash.
	fullHash, err := chat.HashMessages(msgs)
	if err != nil {
		slog.Debug("chat enrichment: failed to hash messages", "error", err)
		return
	}
	entry.ChatHash = &fullHash

	// Parent lookup: try prefix lengths N-2, N-1, N-3, ... down to 1,
	// capped at maxPrefixAttempts. Skip prefix_len <= 0.
	prefixOrder := buildPrefixOrder(n)
	for i, prefixLen := range prefixOrder {
		if i >= maxPrefixAttempts {
			break
		}
		prefixHash, err := chat.HashPrefix(msgs, prefixLen)
		if err != nil {
			continue
		}
		parentID, err := w.findParentByHash(prefixHash)
		if err != nil {
			continue
		}
		if parentID > 0 {
			entry.ParentID = &parentID
			entry.ParentPrefixLen = &prefixLen
			break
		}
	}
}

// buildPrefixOrder returns the prefix lengths to try: N-2 first (the normal
// case where the previous turn contributed 2 messages: user + assistant),
// then N-1, then remaining descending. Skips values <= 0.
func buildPrefixOrder(n int) []int {
	seen := make(map[int]bool)
	var order []int
	add := func(v int) {
		if v > 0 && !seen[v] {
			seen[v] = true
			order = append(order, v)
		}
	}
	add(n - 2) // Normal: prev turn appended user+assistant
	add(n - 1) // Single message appended (e.g. user only, or re-send)
	for i := n - 3; i >= 1; i-- {
		add(i)
	}
	return order
}

// findParentByHash looks up the most recent entry whose chat_hash matches.
func (w *LogWriter) findParentByHash(hash string) (int64, error) {
	var id int64
	err := w.db.QueryRow(
		"SELECT id FROM openai_logs WHERE chat_hash = ? ORDER BY id DESC LIMIT 1",
		hash,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}
