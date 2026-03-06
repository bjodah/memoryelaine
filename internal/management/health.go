package management

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"memoryelaine/internal/database"
)

func healthHandler(reader *database.LogReader, writer *database.LogWriter) http.HandlerFunc {
	startTime := time.Now()
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
		defer cancel()

		dbConnected := true
		if err := reader.PingContext(ctx); err != nil {
			dbConnected = false
		}

		resp := map[string]interface{}{
			"status":         "ok",
			"db_connected":   dbConnected,
			"dropped_logs":   writer.DroppedCount(),
			"uptime_seconds": int(time.Since(startTime).Seconds()),
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("encoding health response", "error", err)
		}
	}
}
