package management

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"memoryelaine/internal/database"
	"memoryelaine/internal/streamview"
)

type logDetailResponse struct {
	Entry      *database.LogEntry `json:"entry"`
	StreamView streamViewResponse `json:"stream_view"`
}

type streamViewResponse struct {
	AssembledBody      string `json:"assembled_body,omitempty"`
	AssembledAvailable bool   `json:"assembled_available"`
	Reason             string `json:"reason"`
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

		entries, err := reader.Query(filter)
		if err != nil {
			http.Error(w, "query error: "+err.Error(), http.StatusInternalServerError)
			return
		}
		total, err := reader.Count(filter)
		if err != nil {
			http.Error(w, "count error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		resp := map[string]interface{}{
			"data":  entries,
			"total": total,
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
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}

		entry, err := reader.GetByID(id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		sv := streamview.Build(entry)
		resp := logDetailResponse{
			Entry: entry,
			StreamView: streamViewResponse{
				AssembledBody:      sv.AssembledBody,
				AssembledAvailable: sv.AssembledAvailable,
				Reason:             string(sv.Reason),
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("encoding log entry response", "id", id, "error", err)
		}
	}
}

func lastRequestHandler(reader *database.LogReader, writer *database.LogWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if body, ok := writer.LastRequest(); ok {
			writeTextBody(w, body)
			return
		}

		entry, err := reader.GetLatest()
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "no request captured yet", http.StatusNotFound)
				return
			}
			http.Error(w, "query error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		writeTextBody(w, entry.ReqBody)
	}
}

func lastResponseHandler(reader *database.LogReader, writer *database.LogWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if body, ok := writer.LastResponse(); ok {
			writeTextBody(w, body)
			return
		}

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
		writeTextBody(w, body)
	}
}

func writeTextBody(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := w.Write([]byte(body)); err != nil {
		slog.Error("writing plain-text response", "error", err)
	}
}
