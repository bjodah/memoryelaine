package management

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"memoryelaine/internal/chat"
	"memoryelaine/internal/database"
	"memoryelaine/internal/jsonellipsis"
	"memoryelaine/internal/query"
	"memoryelaine/internal/recording"
	"memoryelaine/internal/streamview"
)

type recordingStateResponse struct {
	Recording bool `json:"recording"`
}

func apiLogsHandler(reader *database.LogReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter := database.DefaultQueryFilter()

		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
				filter.Limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				filter.Offset = n
			}
		}

		var extraWhere string
		var extraArgs []interface{}

		if q := r.URL.Query().Get("query"); q != "" {
			// Use the query DSL parser
			terms, err := query.Parse(q)
			if err != nil {
				if pe, ok := err.(*query.ParseError); ok {
					writeAPIError(w, http.StatusBadRequest, "query_parse_error", pe.Message)
					return
				}
				writeAPIError(w, http.StatusBadRequest, "query_parse_error", err.Error())
				return
			}
			extraWhere, extraArgs = query.ToSQL(terms)
		} else {
			// Backward compat: support old individual filter params
			if v := r.URL.Query().Get("status"); v != "" {
				if n, err := strconv.Atoi(v); err == nil {
					filter.StatusCode = &n
				}
			}
			if v := r.URL.Query().Get("path"); v != "" {
				filter.Path = &v
			}
			if v := r.URL.Query().Get("since"); v != "" {
				if n, err := strconv.ParseInt(v, 10, 64); err == nil {
					filter.Since = &n
				}
			}
			if v := r.URL.Query().Get("until"); v != "" {
				if n, err := strconv.ParseInt(v, 10, 64); err == nil {
					filter.Until = &n
				}
			}
			if v := r.URL.Query().Get("q"); v != "" {
				filter.Search = &v
			}
		}

		summaries, err := reader.QuerySummariesRaw(filter, extraWhere, extraArgs)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "query_error", err.Error())
			return
		}
		total, err := reader.CountRaw(filter, extraWhere, extraArgs)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "count_error", err.Error())
			return
		}

		data := make([]LogSummary, len(summaries))
		for i, s := range summaries {
			data[i] = LogSummary{
				ID:              s.ID,
				TsStart:         s.TsStart,
				TsEnd:           s.TsEnd,
				DurationMs:      s.DurationMs,
				ClientIP:        s.ClientIP,
				RequestMethod:   s.RequestMethod,
				RequestPath:     s.RequestPath,
				StatusCode:      s.StatusCode,
				ReqBytes:        s.ReqBytes,
				RespBytes:       s.RespBytes,
				ReqTruncated:    s.ReqTruncated,
				RespTruncated:   s.RespTruncated,
				HasRequestBody:  s.ReqBodyLen > 0,
				HasResponseBody: s.RespBodyLen > 0,
				Error:           s.Error,
			}
		}

		resp := LogListResponse{
			Data:    data,
			Total:   total,
			Limit:   filter.Limit,
			Offset:  filter.Offset,
			HasMore: int64(filter.Offset+filter.Limit) < total,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("encoding logs response", "error", err)
		}
	}
}

func apiLogSubHandler(reader *database.LogReader, previewBytes int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/logs/")
		parts := strings.Split(path, "/")
		if len(parts) >= 2 && parts[1] == "body" {
			handleBody(w, r, reader, previewBytes, parts[0])
			return
		}
		if len(parts) >= 2 && parts[1] == "thread" {
			handleThread(w, r, reader, parts[0])
			return
		}
		handleDetail(w, r, reader, parts[0])
	}
}

func handleDetail(w http.ResponseWriter, r *http.Request, reader *database.LogReader, idStr string) {
	if idStr == "" {
		writeAPIError(w, http.StatusBadRequest, "missing_id", "log entry ID is required")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", "log entry ID must be an integer")
		return
	}

	entry, err := reader.GetByID(id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "log entry not found")
		return
	}

	reqHeaders := decodeHeaders(entry.ReqHeadersJSON)
	respHeaders := decodeHeaders(derefStr(entry.RespHeadersJSON))

	sv := streamview.Build(entry)

	detail := LogDetailEntry{
		ID:              entry.ID,
		TsStart:         entry.TsStart,
		TsEnd:           entry.TsEnd,
		DurationMs:      entry.DurationMs,
		ClientIP:        entry.ClientIP,
		RequestMethod:   entry.RequestMethod,
		RequestPath:     entry.RequestPath,
		UpstreamURL:     entry.UpstreamURL,
		StatusCode:      entry.StatusCode,
		ReqBytes:        entry.ReqBytes,
		RespBytes:       entry.RespBytes,
		ReqTruncated:    entry.ReqTruncated,
		RespTruncated:   entry.RespTruncated,
		HasRequestBody:  len(entry.ReqBody) > 0,
		HasResponseBody: entry.RespBody != nil && len(*entry.RespBody) > 0,
		Error:           entry.Error,
		ReqHeaders:      reqHeaders,
		RespHeaders:     respHeaders,
	}

	resp := LogDetailResponse{
		Entry: detail,
		StreamView: StreamViewResponse{
			AssembledAvailable: sv.AssembledAvailable,
			Reason:             string(sv.Reason),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("encoding log detail response", "id", id, "error", err)
	}
}

func handleBody(w http.ResponseWriter, r *http.Request, reader *database.LogReader, previewBytes int, idStr string) {
	if idStr == "" {
		writeAPIError(w, http.StatusBadRequest, "missing_id", "log entry ID is required")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", "log entry ID must be an integer")
		return
	}

	part := r.URL.Query().Get("part")
	if part == "" {
		part = "resp"
	}
	if part != "req" && part != "resp" {
		writeAPIError(w, http.StatusBadRequest, "invalid_part", "part must be 'req' or 'resp'")
		return
	}

	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "raw"
	}
	if mode != "raw" && mode != "assembled" {
		writeAPIError(w, http.StatusBadRequest, "invalid_mode", "mode must be 'raw' or 'assembled'")
		return
	}

	if part == "req" && mode == "assembled" {
		writeAPIError(w, http.StatusBadRequest, "invalid_combination", "assembled mode is not available for request bodies")
		return
	}

	fullParam := r.URL.Query().Get("full")
	full := fullParam == "true" || fullParam == "1"

	ellipsisLimit := 0
	if v := r.URL.Query().Get("ellipsis"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ellipsisLimit = n
		}
	}

	entry, err := reader.GetByID(id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "log entry not found")
		return
	}

	var resp BodyResponse
	resp.Part = part
	resp.Mode = mode
	resp.Full = full

	// Resolve the canonical source body.
	var sourceBody string
	var available bool
	switch {
	case part == "req":
		resp.TotalBytes = entry.ReqBytes
		if entry.ReqBody == "" {
			resp.Reason = "no request body"
		} else {
			sourceBody = entry.ReqBody
			available = true
		}

	case part == "resp" && mode == "raw":
		resp.TotalBytes = entry.RespBytes
		if entry.RespBody == nil || *entry.RespBody == "" {
			resp.Reason = "no response body"
		} else {
			sourceBody = *entry.RespBody
			available = true
		}

	case part == "resp" && mode == "assembled":
		resp.TotalBytes = entry.RespBytes
		sv := streamview.Build(entry)
		if !sv.AssembledAvailable {
			resp.Reason = string(sv.Reason)
		} else {
			sourceBody = sv.AssembledBody
			available = true
			resp.TotalBytes = int64(len(sv.AssembledBody))
		}
	}

	resp.Available = available

	if available {
		content := sourceBody
		resp.Complete = true

		// Try display-ellipsis transform on the full source body.
		if ellipsisLimit > 0 {
			if transformed, changed, tErr := jsonellipsis.Transform(
				[]byte(sourceBody), ellipsisLimit, jsonellipsis.DefaultKeys, jsonellipsis.DefaultMinDepth,
			); tErr == nil && changed {
				content = string(transformed)
				resp.Ellipsized = true
				resp.Complete = false
			}
		}

		// Enforce preview size limit (applies to both ellipsized and plain content).
		if !full && len(content) > previewBytes {
			content = content[:previewBytes]
			resp.Truncated = true
			resp.Complete = false
		}

		resp.Content = content
		resp.IncludedBytes = len(content)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("encoding body response", "id", id, "error", err)
	}
}

func apiRecordingHandler(state *recording.State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeRecordingState(w, state.Enabled())
		case http.MethodPut:
			var req recordingStateResponse
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid json body", http.StatusBadRequest)
				return
			}
			state.SetEnabled(req.Recording)
			writeRecordingState(w, state.Enabled())
		default:
			w.Header().Set("Allow", "GET, PUT")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func lastRequestHandler(reader *database.LogReader, writer *database.LogWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if body, ok, stale := writer.LastRequest(); ok {
			writeTextBody(w, body, stale)
			return
		}

		_, _, stale := writer.LastRequest()
		entry, err := reader.GetLatest()
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "no request captured yet", http.StatusNotFound)
				return
			}
			http.Error(w, "query error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		writeTextBody(w, entry.ReqBody, stale)
	}
}

func lastResponseHandler(reader *database.LogReader, writer *database.LogWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if body, ok, stale := writer.LastResponse(); ok {
			writeTextBody(w, body, stale)
			return
		}

		_, _, stale := writer.LastResponse()
		entry, err := reader.GetLatest()
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "no response captured yet", http.StatusNotFound)
				return
			}
			http.Error(w, "query error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		body := ""
		if entry.RespBody != nil {
			body = *entry.RespBody
		}
		writeTextBody(w, body, stale)
	}
}

func writeRecordingState(w http.ResponseWriter, recording bool) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(recordingStateResponse{Recording: recording}); err != nil {
		slog.Error("encoding recording state response", "error", err)
	}
}

func writeTextBody(w http.ResponseWriter, body string, stale bool) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if stale {
		body = "[STALE]\n\n" + body
	}
	if _, err := w.Write([]byte(body)); err != nil {
		slog.Error("writing plain-text response", "error", err)
	}
}

func writeAPIError(w http.ResponseWriter, status int, errCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(APIError{Error: errCode, Message: message}); err != nil {
		slog.Error("encoding error response", "error", err)
	}
}

func decodeHeaders(raw string) map[string][]string {
	if raw == "" {
		return nil
	}
	var h map[string][]string
	if err := json.Unmarshal([]byte(raw), &h); err != nil {
		return nil
	}
	return h
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// handleThread returns the reconstructed conversation thread leading up to (and
// including) the selected log entry. It uses Top-Down Annotation with backward
// attribution: the selected entry's req_body.messages defines the canonical
// conversation, and the CTE ancestor chain is walked backward to attribute
// each message range to a specific log entry.
func handleThread(w http.ResponseWriter, r *http.Request, reader *database.LogReader, idStr string) {
	if idStr == "" {
		writeAPIError(w, http.StatusBadRequest, "missing_id", "log entry ID is required")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_id", "log entry ID must be an integer")
		return
	}

	// Fetch the ancestor chain (root first, selected last).
	chain, err := reader.GetThreadToSelected(id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "query_error", "failed to query thread")
		return
	}
	if len(chain) == 0 {
		writeAPIError(w, http.StatusNotFound, "not_found", "log entry not found")
		return
	}

	// The selected entry is the last element in the chain.
	selected := chain[len(chain)-1]

	// Guards: Thread view is only available for chat/completions and non-truncated requests.
	if !chat.IsChatPath(selected.RequestPath) {
		writeAPIError(w, http.StatusBadRequest, "invalid_type",
			"thread view is only available for /v1/chat/completions requests")
		return
	}
	if selected.ReqTruncated {
		writeAPIError(w, http.StatusBadRequest, "truncated",
			"thread view is not available for truncated requests")
		return
	}

	// Parse the selected entry's request messages — this is the canonical
	// conversation up to this turn.
	msgs, err := chat.ParseMessages(selected.ReqBody)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "parse_error",
			"failed to parse selected entry's request messages")
		return
	}

	// Build ThreadMessages using backward attribution.
	threadMsgs := chat.BuildThreadMessages(msgs, toThreadEntries(chain), func(id int64) string {
		// Find the entry in the chain to avoid re-fetching from DB.
		var entry *database.LogEntry
		for i := range chain {
			if chain[i].ID == id {
				entry = &chain[i]
				break
			}
		}
		if entry == nil {
			return ""
		}
		sv := streamview.Build(entry)
		if sv.AssembledAvailable {
			return sv.AssembledBody
		}
		return ""
	})

	resp := ThreadResponse{
		SelectedLogID:      id,
		SelectedEntryIndex: len(chain) - 1,
		TotalEntries:       len(chain),
		Messages:           threadMsgs,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode thread response", "error", err)
	}
}

func toThreadEntries(chain []database.LogEntry) []chat.ThreadEntry {
	res := make([]chat.ThreadEntry, len(chain))
	for i := range chain {
		res[i] = chain[i]
	}
	return res
}
