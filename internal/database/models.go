package database

// LogEntry is the central DTO used across every subsystem.
type LogEntry struct {
	ID              int64   `json:"id" db:"id"`
	TsStart         int64   `json:"ts_start" db:"ts_start"`
	TsEnd           *int64  `json:"ts_end" db:"ts_end"`
	DurationMs      *int64  `json:"duration_ms" db:"duration_ms"`
	ClientIP        string  `json:"client_ip" db:"client_ip"`
	RequestMethod   string  `json:"request_method" db:"request_method"`
	RequestPath     string  `json:"request_path" db:"request_path"`
	UpstreamURL     string  `json:"upstream_url" db:"upstream_url"`
	StatusCode      *int    `json:"status_code" db:"status_code"`
	ReqHeadersJSON  string  `json:"req_headers_json" db:"req_headers_json"`
	RespHeadersJSON *string `json:"resp_headers_json" db:"resp_headers_json"`
	ReqBody         string  `json:"req_body" db:"req_body"`
	ReqTruncated    bool    `json:"req_truncated" db:"req_truncated"`
	ReqBytes        int64   `json:"req_bytes" db:"req_bytes"`
	RespBody        *string `json:"resp_body" db:"resp_body"`
	RespTruncated   bool    `json:"resp_truncated" db:"resp_truncated"`
	RespBytes       int64   `json:"resp_bytes" db:"resp_bytes"`
	Error           *string `json:"error" db:"error"`
	ParentID        *int64  `json:"parent_id" db:"parent_id"`
	ChatHash        *string `json:"chat_hash" db:"chat_hash"`
	ParentPrefixLen *int    `json:"parent_prefix_len" db:"parent_prefix_len"`
	MessageCount    *int    `json:"message_count" db:"message_count"`
	ReqText         *string `json:"req_text" db:"req_text"`
	RespText        *string `json:"resp_text" db:"resp_text"`
}

func (e LogEntry) GetID() int64             { return e.ID }
func (e LogEntry) GetRequestPath() string   { return e.RequestPath }
func (e LogEntry) GetReqBody() string       { return e.ReqBody }
func (e LogEntry) IsReqTruncated() bool     { return e.ReqTruncated }
func (e LogEntry) GetRespBody() *string     { return e.RespBody }
func (e LogEntry) GetRespText() *string     { return e.RespText }
func (e LogEntry) GetParentPrefixLen() *int { return e.ParentPrefixLen }

// LogSummary contains only the columns needed for list/table views.
type LogSummary struct {
	ID            int64   `json:"id" db:"id"`
	TsStart       int64   `json:"ts_start" db:"ts_start"`
	TsEnd         *int64  `json:"ts_end" db:"ts_end"`
	DurationMs    *int64  `json:"duration_ms" db:"duration_ms"`
	ClientIP      string  `json:"client_ip" db:"client_ip"`
	RequestMethod string  `json:"request_method" db:"request_method"`
	RequestPath   string  `json:"request_path" db:"request_path"`
	StatusCode    *int    `json:"status_code" db:"status_code"`
	ReqBytes      int64   `json:"req_bytes" db:"req_bytes"`
	RespBytes     int64   `json:"resp_bytes" db:"resp_bytes"`
	ReqTruncated  bool    `json:"req_truncated" db:"req_truncated"`
	RespTruncated bool    `json:"resp_truncated" db:"resp_truncated"`
	ReqBodyLen    int64   `json:"-" db:"req_body_len"`
	RespBodyLen   int64   `json:"-" db:"resp_body_len"`
	Error         *string `json:"error" db:"error"`
}

// QueryFilter defines parameters for querying log entries.
type QueryFilter struct {
	Limit      int
	Offset     int
	StatusCode *int
	Path       *string
	Since      *int64
	Until      *int64
	Search     *string
	OrderDesc  bool
}

// DefaultQueryFilter returns a filter with sensible defaults.
func DefaultQueryFilter() QueryFilter {
	return QueryFilter{
		Limit:     50,
		OrderDesc: true,
	}
}
