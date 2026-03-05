package management

import (
	"crypto/subtle"
	"io/fs"
	"net/http"

	"memoryelaine/internal/config"
	"memoryelaine/internal/database"
	"memoryelaine/internal/web"
)

// ServerDeps holds runtime dependencies for the management server.
type ServerDeps struct {
	Reader    *database.LogReader
	LogWriter *database.LogWriter
	Auth      config.AuthConfig
}

// NewMux builds the http.ServeMux for the management port.
func NewMux(deps ServerDeps) http.Handler {
	mux := http.NewServeMux()

	// /health is public
	mux.Handle("/health", healthHandler(deps.Reader, deps.LogWriter))

	// Everything else requires basic auth
	mux.Handle("/metrics", basicAuth(metricsHandler(), deps.Auth.Username, deps.Auth.Password))
	mux.Handle("/api/logs", basicAuth(apiLogsHandler(deps.Reader), deps.Auth.Username, deps.Auth.Password))
	mux.Handle("/api/logs/", basicAuth(apiLogByIDHandler(deps.Reader), deps.Auth.Username, deps.Auth.Password))

	// Embedded web UI
	sub, err := fs.Sub(web.StaticFS, "static")
	if err != nil {
		panic("failed to create sub filesystem: " + err.Error())
	}
	mux.Handle("/", basicAuth(http.FileServer(http.FS(sub)), deps.Auth.Username, deps.Auth.Password))

	return mux
}

func basicAuth(next http.Handler, username, password string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok ||
			subtle.ConstantTimeCompare([]byte(u), []byte(username)) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="memoryelaine"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
