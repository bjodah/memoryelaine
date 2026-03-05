package database

import (
	"context"
	"database/sql"
	"log/slog"
	"sync/atomic"
)

// LogWriter consumes LogEntry values from a channel and INSERTs them.
type LogWriter struct {
	db         *sql.DB
	queue      chan LogEntry
	dropped    atomic.Int64
	insertStmt *sql.Stmt
}

const insertSQL = `INSERT INTO openai_logs (
    ts_start, ts_end, duration_ms, client_ip,
    request_method, request_path, upstream_url, status_code,
    req_headers_json, resp_headers_json,
    req_body, req_truncated, req_bytes,
    resp_body, resp_truncated, resp_bytes,
    error
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

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
	)
	if err != nil {
		slog.Error("failed to insert log entry", "error", err)
	}
}

func (w *LogWriter) drain() {
	for {
		select {
		case entry := <-w.queue:
			w.insert(entry)
		default:
			return
		}
	}
}
