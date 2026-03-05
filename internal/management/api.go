package management

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"memoryelaine/internal/database"
)

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
		json.NewEncoder(w).Encode(resp)
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entry)
	}
}
