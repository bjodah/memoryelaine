package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// LogReader provides query helpers for reading log entries.
type LogReader struct {
	db *sql.DB
}

// NewLogReader creates a new LogReader.
func NewLogReader(db *sql.DB) *LogReader {
	return &LogReader{db: db}
}

// Query returns log entries matching the filter.
func (r *LogReader) Query(filter QueryFilter) ([]LogEntry, error) {
	where, args := buildWhere(filter)
	order := "DESC"
	if !filter.OrderDesc {
		order = "ASC"
	}
	limit := filter.Limit
	if limit <= 0 || limit > 1000 {
		limit = 50
	}

	query := fmt.Sprintf(
		"SELECT id, ts_start, ts_end, duration_ms, client_ip, request_method, request_path, upstream_url, status_code, req_headers_json, resp_headers_json, req_body, req_truncated, req_bytes, resp_body, resp_truncated, resp_bytes, error FROM openai_logs %s ORDER BY ts_start %s LIMIT ? OFFSET ?",
		where, order,
	)
	args = append(args, limit, filter.Offset)

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying logs: %w", err)
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(
			&e.ID, &e.TsStart, &e.TsEnd, &e.DurationMs, &e.ClientIP,
			&e.RequestMethod, &e.RequestPath, &e.UpstreamURL, &e.StatusCode,
			&e.ReqHeadersJSON, &e.RespHeadersJSON,
			&e.ReqBody, &e.ReqTruncated, &e.ReqBytes,
			&e.RespBody, &e.RespTruncated, &e.RespBytes,
			&e.Error,
		); err != nil {
			return nil, fmt.Errorf("scanning log row: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// GetByID returns a single log entry or sql.ErrNoRows.
func (r *LogReader) GetByID(id int64) (*LogEntry, error) {
	query := "SELECT id, ts_start, ts_end, duration_ms, client_ip, request_method, request_path, upstream_url, status_code, req_headers_json, resp_headers_json, req_body, req_truncated, req_bytes, resp_body, resp_truncated, resp_bytes, error FROM openai_logs WHERE id = ?"
	var e LogEntry
	err := r.db.QueryRow(query, id).Scan(
		&e.ID, &e.TsStart, &e.TsEnd, &e.DurationMs, &e.ClientIP,
		&e.RequestMethod, &e.RequestPath, &e.UpstreamURL, &e.StatusCode,
		&e.ReqHeadersJSON, &e.RespHeadersJSON,
		&e.ReqBody, &e.ReqTruncated, &e.ReqBytes,
		&e.RespBody, &e.RespTruncated, &e.RespBytes,
		&e.Error,
	)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// Count returns total rows matching the filter (for pagination).
func (r *LogReader) Count(filter QueryFilter) (int64, error) {
	where, args := buildWhere(filter)
	query := fmt.Sprintf("SELECT COUNT(*) FROM openai_logs %s", where)
	var count int64
	err := r.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

// DeleteBefore deletes all rows with ts_start < cutoffMs.
func (r *LogReader) DeleteBefore(cutoffMs int64) (int64, error) {
	res, err := r.db.Exec("DELETE FROM openai_logs WHERE ts_start < ?", cutoffMs)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Ping checks database connectivity.
func (r *LogReader) Ping() error {
	return r.db.Ping()
}

// PingContext checks database connectivity with a context deadline.
func (r *LogReader) PingContext(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

func buildWhere(f QueryFilter) (string, []interface{}) {
	var conds []string
	var args []interface{}

	if f.StatusCode != nil {
		conds = append(conds, "status_code = ?")
		args = append(args, *f.StatusCode)
	}
	if f.Path != nil {
		conds = append(conds, "request_path = ?")
		args = append(args, *f.Path)
	}
	if f.Since != nil {
		conds = append(conds, "ts_start >= ?")
		args = append(args, *f.Since)
	}
	if f.Until != nil {
		conds = append(conds, "ts_start <= ?")
		args = append(args, *f.Until)
	}
	if f.Search != nil {
		conds = append(conds, "(req_body LIKE ? OR resp_body LIKE ?)")
		pattern := "%" + *f.Search + "%"
		args = append(args, pattern, pattern)
	}

	if len(conds) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}
