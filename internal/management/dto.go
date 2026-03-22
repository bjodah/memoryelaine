package management

// LogSummary is the lightweight row returned by the list endpoint.
// It deliberately omits bodies, headers, and upstream_url.
type LogSummary struct {
	ID              int64   `json:"id"`
	TsStart         int64   `json:"ts_start"`
	TsEnd           *int64  `json:"ts_end"`
	DurationMs      *int64  `json:"duration_ms"`
	ClientIP        string  `json:"client_ip"`
	RequestMethod   string  `json:"request_method"`
	RequestPath     string  `json:"request_path"`
	StatusCode      *int    `json:"status_code"`
	ReqBytes        int64   `json:"req_bytes"`
	RespBytes       int64   `json:"resp_bytes"`
	ReqTruncated    bool    `json:"req_truncated"`
	RespTruncated   bool    `json:"resp_truncated"`
	HasRequestBody  bool    `json:"has_request_body"`
	HasResponseBody bool    `json:"has_response_body"`
	Error           *string `json:"error"`
}

// LogListResponse is the envelope for paginated summary results.
type LogListResponse struct {
	Data    []LogSummary `json:"data"`
	Total   int64        `json:"total"`
	Limit   int          `json:"limit"`
	Offset  int          `json:"offset"`
	HasMore bool         `json:"has_more"`
}

// LogDetailResponse is the envelope for a single log entry's metadata.
type LogDetailResponse struct {
	Entry      LogDetailEntry     `json:"entry"`
	StreamView StreamViewResponse `json:"stream_view"`
}

// LogDetailEntry contains full metadata for one log, including decoded headers.
// It does NOT include request/response bodies.
type LogDetailEntry struct {
	ID              int64               `json:"id"`
	TsStart         int64               `json:"ts_start"`
	TsEnd           *int64              `json:"ts_end"`
	DurationMs      *int64              `json:"duration_ms"`
	ClientIP        string              `json:"client_ip"`
	RequestMethod   string              `json:"request_method"`
	RequestPath     string              `json:"request_path"`
	UpstreamURL     string              `json:"upstream_url"`
	StatusCode      *int                `json:"status_code"`
	ReqBytes        int64               `json:"req_bytes"`
	RespBytes       int64               `json:"resp_bytes"`
	ReqTruncated    bool                `json:"req_truncated"`
	RespTruncated   bool                `json:"resp_truncated"`
	HasRequestBody  bool                `json:"has_request_body"`
	HasResponseBody bool                `json:"has_response_body"`
	Error           *string             `json:"error"`
	ReqHeaders      map[string][]string `json:"req_headers"`
	RespHeaders     map[string][]string `json:"resp_headers"`
}

// StreamViewResponse contains stream-view availability metadata.
type StreamViewResponse struct {
	AssembledAvailable bool   `json:"assembled_available"`
	Reason             string `json:"reason"`
}

// BodyResponse is the envelope for body retrieval results.
type BodyResponse struct {
	Part          string `json:"part"`
	Mode          string `json:"mode"`
	Full          bool   `json:"full"`
	Content       string `json:"content"`
	IncludedBytes int    `json:"included_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
	Truncated     bool   `json:"truncated"`
	Available     bool   `json:"available"`
	Reason        string `json:"reason,omitempty"`
}

// APIError is a structured error response.
type APIError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}
