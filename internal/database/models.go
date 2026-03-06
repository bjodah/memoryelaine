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
