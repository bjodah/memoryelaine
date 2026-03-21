package management

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"memoryelaine/internal/database"
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

		summaries, err := reader.QuerySummaries(filter)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "query_error", err.Error())
			return
		}
		total, err := reader.Count(filter)
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

func apiLogByIDHandler(reader *database.LogReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract ID from path: /api/logs/{id}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/logs/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			writeAPIError(w, http.StatusBadRequest, "missing_id", "log entry ID is required")
			return
		}
		id, err := strconv.ParseInt(parts[0], 10, 64)
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
