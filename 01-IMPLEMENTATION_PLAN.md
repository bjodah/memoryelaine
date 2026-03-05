# IMPLEMENTATION_PLAN.md — `memoryelaine`

## 0. Document Purpose

This document is the single authoritative reference for implementing
`memoryelaine`. Every file, every public function signature, every data
structure, and the order in which they should be built is specified below.
The plan is divided into sequential phases; each phase produces a testable
artifact.

---

## 1. Repository Layout (Final State)

```
memoryelaine/
├── main.go                         # Entrypoint — cobra root command
├── cmd/
│   ├── root.go                     # Root cobra command, config loading
│   ├── serve.go                    # `serve` subcommand
│   ├── log.go                      # `log` subcommand (CLI query)
│   ├── tui.go                      # `tui` subcommand
│   └── prune.go                    # `prune` subcommand
├── internal/
│   ├── config/
│   │   └── config.go               # Typed config struct, loader via viper
│   ├── database/
│   │   ├── db.go                   # Open, migrate, PRAGMA setup
│   │   ├── writer.go               # Async queue consumer (INSERT worker)
│   │   ├── reader.go               # Query helpers (used by CLI/TUI/Web)
│   │   └── models.go               # LogEntry struct (central data model)
│   ├── proxy/
│   │   ├── proxy.go                # Build httputil.ReverseProxy, attach hooks
│   │   ├── capture.go              # CaptureTransport / teeReadCloser / countingWriter
│   │   ├── redact.go               # Header redaction logic
│   │   └── handler.go              # Top-level HTTP handler (path allowlist routing)
│   ├── management/
│   │   ├── server.go               # Management HTTP server setup (mux, auth)
│   │   ├── health.go               # /health handler
│   │   ├── metrics.go              # /metrics handler (Prometheus)
│   │   └── api.go                  # /api/logs JSON handler
│   ├── web/
│   │   ├── embed.go                # //go:embed directive
│   │   └── static/                 # HTML/JS/CSS assets
│   │       ├── index.html
│   │       ├── app.js
│   │       └── style.css
│   ├── tui/
│   │   ├── app.go                  # bubbletea.Program bootstrap
│   │   ├── model.go                # Top-level Model, Update, View
│   │   ├── table.go                # Table component
│   │   └── detail.go               # Detail pane component
│   └── version/
│       └── version.go              # Build-time version vars
├── config.yaml                     # Gitignored (user copy)
├── example-config.yaml             # Checked in
├── Dockerfile
├── docker-compose.yaml             # Optional dev convenience
├── go.mod
├── go.sum
├── README.md
├── 00-SPEC-DRAFT_memoryelaine_logging-inference-proxy.md
└── IMPLEMENTATION_PLAN.md          # This file
```

---

## 2. Central Data Structures

### 2.1. `internal/config/config.go` — `Config`

```go
package config

import "time"

type Config struct {
    Proxy      ProxyConfig      `mapstructure:"proxy"`
    Management ManagementConfig `mapstructure:"management"`
    Database   DatabaseConfig   `mapstructure:"database"`
    Logging    LoggingConfig    `mapstructure:"logging"`
}

type ProxyConfig struct {
    ListenAddr      string        `mapstructure:"listen_addr"`       // "0.0.0.0:8000"
    UpstreamBaseURL string        `mapstructure:"upstream_base_url"` // "https://api.openai.com"
    TimeoutMinutes  int           `mapstructure:"timeout_minutes"`   // 23
    LogPaths        []string      `mapstructure:"log_paths"`         // ["/v1/chat/completions", ...]
}

type ManagementConfig struct {
    ListenAddr string     `mapstructure:"listen_addr"` // "0.0.0.0:8080"
    Auth       AuthConfig `mapstructure:"auth"`
}

type AuthConfig struct {
    Username string `mapstructure:"username"`
    Password string `mapstructure:"password"`
}

type DatabaseConfig struct {
    Path string `mapstructure:"path"` // "./memoryelaine.db"
}

type LoggingConfig struct {
    MaxCaptureBytes int `mapstructure:"max_capture_bytes"` // 8388608
}
```

**Functions:**

```go
// Load reads configuration using viper.
// Lookup order: --config flag value → ./config.yaml → $HOME/.config/memoryelaine/config.yaml
// Returns a fully validated Config or an error.
func Load(cfgPath string) (*Config, error)

// validate is called internally by Load. It checks:
//   - UpstreamBaseURL is a valid URL with scheme
//   - ListenAddr ports don't collide
//   - MaxCaptureBytes > 0
//   - LogPaths is non-empty
//   - Auth credentials are set (warn via slog if defaults)
func (c *Config) validate() error
```

**Technique:** `viper.SetConfigFile` / `viper.ReadInConfig`, then `viper.Unmarshal` into the struct. Use `mapstructure` tags. Set defaults with `viper.SetDefault`.

---

### 2.2. `internal/database/models.go` — `LogEntry`

This is the **central DTO** used across every subsystem: the proxy handler populates it, the async writer INSERTs it, the reader returns slices of it, the TUI/Web/CLI render it.

```go
package database

type LogEntry struct {
    ID            int64   `json:"id" db:"id"`
    TsStart       int64   `json:"ts_start" db:"ts_start"`             // Unix ms
    TsEnd         *int64  `json:"ts_end" db:"ts_end"`                 // nullable
    DurationMs    *int64  `json:"duration_ms" db:"duration_ms"`       // nullable
    ClientIP      string  `json:"client_ip" db:"client_ip"`
    RequestMethod string  `json:"request_method" db:"request_method"`
    RequestPath   string  `json:"request_path" db:"request_path"`
    UpstreamURL   string  `json:"upstream_url" db:"upstream_url"`
    StatusCode    *int    `json:"status_code" db:"status_code"`       // nullable (upstream error)
    ReqHeadersJSON  string `json:"req_headers_json" db:"req_headers_json"`
    RespHeadersJSON *string `json:"resp_headers_json" db:"resp_headers_json"`
    ReqBody       string  `json:"req_body" db:"req_body"`
    ReqTruncated  bool    `json:"req_truncated" db:"req_truncated"`
    ReqBytes      int64   `json:"req_bytes" db:"req_bytes"`
    RespBody      *string `json:"resp_body" db:"resp_body"`
    RespTruncated bool    `json:"resp_truncated" db:"resp_truncated"`
    RespBytes     int64   `json:"resp_bytes" db:"resp_bytes"`
    Error         *string `json:"error" db:"error"`
}
```

### 2.3. `internal/database/reader.go` — `QueryFilter`

```go
type QueryFilter struct {
    Limit      int     // default 50, max 1000
    Offset     int
    StatusCode *int    // optional filter
    Path       *string // optional filter
    Since      *int64  // Unix ms, optional
    Until      *int64  // Unix ms, optional
    Search     *string // LIKE on req_body/resp_body, optional
    OrderDesc  bool    // default true (newest first)
}
```

---

## 3. Phase 1 — Skeleton, Config, Database Layer

**Goal:** A binary that loads config, opens SQLite with WAL, runs migrations,
and exits cleanly. All subsequent phases build on this.

### 3.1. Files to Create

| File | Purpose |
|---|---|
| `main.go` | `func main()` → calls `cmd.Execute()` |
| `cmd/root.go` | Cobra root command; `--config` persistent flag; calls `config.Load` |
| `cmd/serve.go` | Stub: prints "serve not yet implemented" |
| `cmd/log.go` | Stub |
| `cmd/tui.go` | Stub |
| `cmd/prune.go` | Stub |
| `internal/config/config.go` | As defined in §2.1 |
| `internal/database/db.go` | Open + migrate + PRAGMA |
| `internal/database/models.go` | As defined in §2.2 |
| `internal/database/writer.go` | Async queue + INSERT worker |
| `internal/database/reader.go` | Query helpers |
| `example-config.yaml` | Reference config |
| `go.mod` | Module init |

### 3.2. `internal/database/db.go`

**Driver:** `github.com/mattn/go-sqlite3` (CGO). We use `database/sql` with
this driver, not an ORM.

```go
package database

import "database/sql"

// Open creates (or opens) the SQLite database at the given path,
// sets WAL pragmas, and runs the schema migration.
// It returns a *sql.DB suitable for concurrent use.
func Open(dbPath string) (*sql.DB, error)

// migrate runs CREATE TABLE IF NOT EXISTS and CREATE INDEX IF NOT EXISTS.
// It is idempotent and safe to call on every startup.
func migrate(db *sql.DB) error
```

**Implementation notes for `Open`:**
1. `sql.Open("sqlite3", dsn)` where dsn = `dbPath + "?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000"`
   (Alternatively, execute PRAGMAs post-open. Using DSN params with `go-sqlite3` is cleaner.)
2. `db.SetMaxOpenConns(1)` for the **writer** connection (serializes writes).
   For readers (CLI/TUI/Web), `SetMaxOpenConns` can be higher since WAL allows concurrent reads.
3. Call `migrate(db)`.
4. Ping the database to verify.

**Design decision:** We will expose **two** constructor functions:

```go
// OpenWriter opens a DB handle optimized for the single async writer.
// MaxOpenConns = 1.
func OpenWriter(dbPath string) (*sql.DB, error)

// OpenReader opens a DB handle optimized for concurrent readers.
// MaxOpenConns = 4 (or runtime.NumCPU(), whichever is smaller).
func OpenReader(dbPath string) (*sql.DB, error)
```

Both call the same internal `openAndMigrate` function but differ in connection pool settings.

### 3.3. `internal/database/writer.go` — `LogWriter`

```go
package database

import (
    "context"
    "database/sql"
    "log/slog"
    "sync/atomic"
)

// LogWriter consumes LogEntry values from a channel and INSERTs them.
type LogWriter struct {
    db          *sql.DB
    queue       chan LogEntry
    dropped     atomic.Int64 // exposed for /health
    insertStmt  *sql.Stmt   // prepared once
}

// NewLogWriter creates a LogWriter with bounded channel of given capacity.
func NewLogWriter(db *sql.DB, queueSize int) (*LogWriter, error)

// Enqueue attempts to send a LogEntry to the worker. If the channel is full,
// it increments the drop counter and logs via slog.Error. Returns true if enqueued.
func (w *LogWriter) Enqueue(entry LogEntry) bool

// Run starts the background INSERT loop. Blocks until ctx is cancelled.
// On context cancellation, it drains the remaining channel.
func (w *LogWriter) Run(ctx context.Context)

// DroppedCount returns the number of dropped log entries.
func (w *LogWriter) DroppedCount() int64
```

**INSERT implementation:** A single prepared statement:
```sql
INSERT INTO openai_logs (
    ts_start, ts_end, duration_ms, client_ip,
    request_method, request_path, upstream_url, status_code,
    req_headers_json, resp_headers_json,
    req_body, req_truncated, req_bytes,
    resp_body, resp_truncated, resp_bytes,
    error
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
```

The `Run` loop:
```go
for {
    select {
    case entry := <-w.queue:
        w.insert(entry)
    case <-ctx.Done():
        w.drain()
        return
    }
}
```

`insert` wraps `stmt.Exec` in an error handler that logs via `slog.Error` but never panics or stops the loop (fail-open).

### 3.4. `internal/database/reader.go` — `LogReader`

```go
package database

import "database/sql"

type LogReader struct {
    db *sql.DB
}

func NewLogReader(db *sql.DB) *LogReader

// Query returns log entries matching the filter.
// Builds a dynamic WHERE clause from non-nil filter fields.
func (r *LogReader) Query(filter QueryFilter) ([]LogEntry, error)

// GetByID returns a single log entry or sql.ErrNoRows.
func (r *LogReader) GetByID(id int64) (*LogEntry, error)

// Count returns total rows matching the filter (for pagination).
func (r *LogReader) Count(filter QueryFilter) (int64, error)

// DeleteBefore deletes all rows with ts_start < cutoffMs.
// Returns the number of deleted rows.
func (r *LogReader) DeleteBefore(cutoffMs int64) (int64, error)
```

**Technique for dynamic WHERE:** Build a slice of conditions and a
corresponding `[]interface{}` of args. Append conditions only when the
filter field is non-nil. Join with `AND`. This avoids an ORM while
remaining safe against injection (parameterized queries only).

### 3.5. Tests for Phase 1

| Test file | What it tests |
|---|---|
| `internal/config/config_test.go` | Valid YAML parses; invalid YAML errors; defaults applied; validation catches bad URLs |
| `internal/database/db_test.go` | Open creates file, tables exist, idempotent migration |
| `internal/database/writer_test.go` | Enqueue/insert round-trip; channel-full drop counting |
| `internal/database/reader_test.go` | Query with all filter combinations; GetByID; Count; DeleteBefore |

Use `t.TempDir()` for SQLite test databases. No mocks needed — SQLite is fast enough for unit tests.

---

## 4. Phase 2 — Reverse Proxy with Capture

**Goal:** `memoryelaine serve` starts the proxy port, forwards requests upstream,
streams responses back with zero added latency, and enqueues log entries.

### 4.1. Files to Create / Modify

| File | Purpose |
|---|---|
| `internal/proxy/proxy.go` | Constructs `httputil.ReverseProxy` |
| `internal/proxy/capture.go` | `cappedBuffer`, `teeReadCloser`, `countingResponseWriter` |
| `internal/proxy/redact.go` | Header redaction |
| `internal/proxy/handler.go` | Top-level handler with path allowlist |
| `cmd/serve.go` | Wire everything together |

### 4.2. `internal/proxy/capture.go` — Capture Primitives

```go
package proxy

import "io"

// cappedBuffer wraps a bytes.Buffer with a maximum capture size.
// After the cap is reached, Write still counts bytes but discards content.
type cappedBuffer struct {
    buf       []byte
    cap       int
    written   int64 // total bytes seen (even beyond cap)
    truncated bool
}

func newCappedBuffer(maxBytes int) *cappedBuffer

// Write implements io.Writer. Always returns len(p), nil.
// If total written exceeds cap, sets truncated=true and stops appending.
func (c *cappedBuffer) Write(p []byte) (int, error)

// Bytes returns captured bytes (up to cap).
func (c *cappedBuffer) Bytes() []byte

// TotalBytes returns the real total byte count.
func (c *cappedBuffer) TotalBytes() int64

// Truncated returns whether the capture was truncated.
func (c *cappedBuffer) Truncated() bool
```

```go
// teeReadCloser wraps an io.ReadCloser. Every Read() is tee'd into a cappedBuffer.
// Used to wrap response Body in ModifyResponse.
type teeReadCloser struct {
    source io.ReadCloser
    tee    *cappedBuffer
}

func newTeeReadCloser(rc io.ReadCloser, maxBytes int) *teeReadCloser

// Read implements io.Reader — reads from source, writes to tee.
func (t *teeReadCloser) Read(p []byte) (int, error)

// Close closes the underlying source.
func (t *teeReadCloser) Close() error
```

```go
// statusCapturingWriter wraps http.ResponseWriter to capture the status code
// and implement http.Flusher for SSE streaming.
type statusCapturingWriter struct {
    http.ResponseWriter
    statusCode int
    written    bool // whether WriteHeader has been called
}

func newStatusCapturingWriter(w http.ResponseWriter) *statusCapturingWriter

func (s *statusCapturingWriter) WriteHeader(code int)
func (s *statusCapturingWriter) Write(b []byte) (int, error)
func (s *statusCapturingWriter) Flush()  // delegates to underlying Flusher
// Unwrap returns the underlying ResponseWriter (for http.ResponseController compatibility)
func (s *statusCapturingWriter) Unwrap() http.ResponseWriter
```

### 4.3. `internal/proxy/redact.go`

```go
package proxy

import "net/http"

// redactedHeaders is the set of headers to strip before logging.
var redactedHeaders = map[string]struct{}{
    "Authorization": {},
    "Cookie":        {},
    "Set-Cookie":    {},
}

// RedactHeaders returns a shallow copy of the header map with sensitive
// headers removed. The original is not modified.
// Applied to BOTH request and response headers before DB serialization.
func RedactHeaders(h http.Header) http.Header

// HeadersToJSON serializes an http.Header to a JSON string.
// Returns "{}" on marshal error.
func HeadersToJSON(h http.Header) string
```

### 4.4. `internal/proxy/proxy.go`

```go
package proxy

import (
    "net/http"
    "net/http/httputil"
    "net/url"
    "time"
)

// ProxyConfig holds the runtime dependencies for the proxy.
type ProxyConfig struct {
    UpstreamURL    *url.URL
    Timeout        time.Duration
    MaxCapture     int
    LogWriter      *database.LogWriter  // for enqueuing
}

// NewReverseProxy creates a configured httputil.ReverseProxy.
//
// Director: rewrites request URL to upstream.
// Transport: http.Transport with configured timeouts.
// ModifyResponse: wraps resp.Body with teeReadCloser for capture.
// ErrorHandler: logs upstream errors via slog, returns 502 to client,
//               still enqueues a LogEntry with the error field set.
//
// The actual per-request lifecycle is managed by handler.go which wraps
// the proxy call, captures timing, and enqueues.
func NewReverseProxy(cfg ProxyConfig) *httputil.ReverseProxy
```

**Director implementation detail:**
```go
func(req *http.Request) {
    req.URL.Scheme = target.Scheme
    req.URL.Host = target.Host
    req.URL.Path = singleJoiningSlash(target.Path, req.URL.Path)
    req.Host = target.Host // important for virtual-hosted upstreams
}
```

**Transport configuration:**
```go
&http.Transport{
    // Enable HTTP/2 upstream if TLS
    ForceAttemptHTTP2:     true,
    MaxIdleConns:          100,
    MaxIdleConnsPerHost:   100,
    IdleConnTimeout:       90 * time.Second,
    // timeout_minutes bounds only connection setup + first byte:
    ResponseHeaderTimeout: cfg.Timeout, // 23 min — time to first response byte
    TLSHandshakeTimeout:  30 * time.Second,
    // No per-stream read deadline — active streams run indefinitely
}
```

**FlushInterval (critical for SSE zero-latency streaming):**
```go
rp.FlushInterval = -1 // immediate flush on every Write
```

**ModifyResponse:** This is where we attach the response body tee:
```go
func(resp *http.Response) error {
    // Stash the teeReadCloser in the request context so handler.go
    // can retrieve capture results after ServeHTTP returns.
    tee := newTeeReadCloser(resp.Body, cfg.MaxCapture)
    resp.Body = tee
    // Store reference via request context (set in handler.go pre-call)
    if holder := getHolder(resp.Request); holder != nil {
        holder.respTee = tee
    }
    return nil
}
```

### 4.5. `internal/proxy/handler.go`

This is the core HTTP handler mounted on the proxy port.

```go
package proxy

import (
    "net/http"
    "time"
)

// captureHolder is stored in the request context to pass capture state
// between the handler, the Director, and ModifyResponse.
type captureHolder struct {
    reqCapture  *cappedBuffer
    respTee     *teeReadCloser
    startTime   time.Time
}

type contextKey string
const holderKey contextKey = "capture"

func setHolder(r *http.Request, h *captureHolder) *http.Request
func getHolder(r *http.Request) *captureHolder

// Handler returns the top-level HTTP handler for the proxy port.
//
// Two proxy instances are used to avoid capture overhead on non-logged paths:
//   rpPlain   — vanilla reverse proxy (no ModifyResponse hooks)
//   rpCapture — reverse proxy with ModifyResponse tee for body capture
//
// Behavior:
// 1. Check if r.URL.Path is in logPaths set (exact match).
//    - If NO: proxy via rpPlain (zero capture overhead).
//    - If YES: proceed with capture via rpCapture.
// 2. Record start time.
// 3. Wrap r.Body with teeReadCloser (stream-only; captured as upstream reads).
// 4. Attach captureHolder to request context.
// 5. Derive client_ip from r.RemoteAddr (host portion only; ignore XFF).
// 6. Call rpCapture.ServeHTTP(statusCapturingWriter, r).
// 7. After ServeHTTP returns, build LogEntry from holder + status + timing.
//    Redact both request and response headers before serializing to JSON.
// 8. Enqueue LogEntry to LogWriter (fire-and-forget).
func Handler(
    rpPlain      *httputil.ReverseProxy,
    rpCapture    *httputil.ReverseProxy,
    logPathSet   map[string]struct{},
    maxCapture   int,
    logWriter    *database.LogWriter,
) http.Handler
```

**Critical implementation detail for request body capture:**

```go
// In the handler, BEFORE calling rp.ServeHTTP:
reqBuf := newCappedBuffer(maxCapture)
// Copy body into buffer while counting
body, err := io.ReadAll(io.TeeReader(
    io.LimitReader(r.Body, int64(maxCapture)+1), // +1 to detect truncation
    reqBuf,
))
// But wait — this reads the ENTIRE body up to cap into memory, which is fine.
// For bodies > cap, we need to also consume & count the rest.
// Better approach: use our cappedBuffer as a Writer with io.Copy:
reqBuf := newCappedBuffer(maxCapture)
fullBody, err := io.ReadAll(io.TeeReader(r.Body, reqBuf))
// Problem: this reads the entire body into memory.

// CORRECT approach for large request bodies:
// 1. Tee r.Body into cappedBuffer.
// 2. Also buffer the full body for replay (up to cap).
// 3. For anything beyond cap, we still need to forward it upstream.
//
// Solution: Don't buffer the full body. Instead, create a teeReadCloser
// for the request body too, and let the Director/Transport read through it
// naturally. The cappedBuffer captures up to the limit.

reqTee := newTeeReadCloser(r.Body, maxCapture)
r.Body = reqTee
holder.reqCapture = reqTee.tee
```

This is the cleanest approach: the request body flows through the tee naturally as the upstream Transport reads it. After `ServeHTTP` returns, the tee has captured up to `maxCapture` bytes and counted the total.

### 4.6. Request Body Lifecycle (Important Clarification)

The `httputil.ReverseProxy` Director receives the *same* `*http.Request` we hand to `ServeHTTP`. The Transport reads `r.Body`. So if we replace `r.Body` with our `teeReadCloser` **before** calling `ServeHTTP`, the body naturally flows:

```
Client → teeReadCloser.Read() → cappedBuffer.Write() (capture)
                               → Transport sends to upstream
```

No double-buffering. No replay needed. The body is streamed through, captured on the fly. This is the correct design.

### 4.7. Tests for Phase 2

| Test file | What it tests |
|---|---|
| `internal/proxy/capture_test.go` | `cappedBuffer` at/below/above cap; `teeReadCloser` reads correctly; `statusCapturingWriter` captures code and flushes |
| `internal/proxy/redact_test.go` | Headers redacted; original untouched; JSON output valid |
| `internal/proxy/handler_test.go` | Integration: `httptest.NewServer` as upstream; verify pass-through; verify LogEntry enqueued; verify non-log-path bypasses capture; SSE streaming test with Flusher assertion |

**SSE streaming test technique:** Upstream test server writes chunks with `time.Sleep(10ms)` between them. Client reads chunks and asserts each chunk arrives within ~20ms (not buffered until end).

---

## 5. Phase 3 — Management Server (Web UI, API, Metrics, Health)

### 5.1. Files to Create

| File | Purpose |
|---|---|
| `internal/management/server.go` | Mux setup, Basic Auth middleware |
| `internal/management/health.go` | `/health` handler |
| `internal/management/metrics.go` | Prometheus metrics setup and `/metrics` handler |
| `internal/management/api.go` | `/api/logs` and `/api/logs/:id` handlers |
| `internal/web/embed.go` | `//go:embed static/*` |
| `internal/web/static/index.html` | Single-page log viewer |
| `internal/web/static/app.js` | Fetch `/api/logs`, render table, pagination, detail modal |
| `internal/web/static/style.css` | Minimal styling |

### 5.2. `internal/management/server.go`

```go
package management

import (
    "net/http"
)

type ServerDeps struct {
    Reader      *database.LogReader
    LogWriter   *database.LogWriter  // for dropped count
    Auth        config.AuthConfig
}

// NewMux builds the http.ServeMux for the management port.
//
// Routes:
//   GET /health         → healthHandler  (no auth)
//   GET /metrics        → promhttp       (basic auth)
//   GET /               → embedded SPA   (basic auth)
//   GET /api/logs       → apiLogsHandler  (basic auth)
//   GET /api/logs/{id}  → apiLogByID      (basic auth)
func NewMux(deps ServerDeps) http.Handler

// basicAuth is a middleware that wraps a handler with HTTP Basic Auth.
// Uses constant-time comparison via crypto/subtle.
func basicAuth(next http.Handler, username, password string) http.Handler
```

### 5.3. `internal/management/health.go`

```go
package management

// healthHandler returns JSON:
// {"status":"ok","db_connected":true,"dropped_logs":0,"uptime_seconds":123}
//
// db_connected: verified by db.PingContext with a 1s timeout.
// dropped_logs: read from LogWriter.DroppedCount().
func healthHandler(reader *database.LogReader, writer *database.LogWriter) http.HandlerFunc
```

### 5.4. `internal/management/metrics.go`

```go
package management

import "github.com/prometheus/client_golang/prometheus"

// Metrics holds all Prometheus metrics for the proxy.
type Metrics struct {
    RequestsTotal    *prometheus.CounterVec   // labels: method, path, status
    RequestDuration  *prometheus.HistogramVec // labels: method, path
    ActiveStreams     prometheus.Gauge
    DroppedLogs      prometheus.Counter
    DBWriteErrors    prometheus.Counter
}

// NewMetrics registers all metrics and returns the struct.
func NewMetrics(reg prometheus.Registerer) *Metrics

// Note: Metrics is created in cmd/serve.go and passed to both the proxy
// handler (to increment counters) and the management server (to expose /metrics).
```

### 5.5. `internal/management/api.go`

```go
package management

import "net/http"

// apiLogsHandler handles GET /api/logs
//
// Query params: limit, offset, status, path, since, until, q (search)
// Response: JSON { "data": [...LogEntry], "total": 1234 }
func apiLogsHandler(reader *database.LogReader) http.HandlerFunc

// apiLogByIDHandler handles GET /api/logs/{id}
// Response: JSON LogEntry or 404
func apiLogByIDHandler(reader *database.LogReader) http.HandlerFunc
```

**Technique:** Parse query params → build `QueryFilter` → call `reader.Query` + `reader.Count` → marshal JSON response.

### 5.6. Web UI (`internal/web/static/`)

**`index.html`**: Minimal HTML skeleton with a `<div id="app">` mount point. Includes `app.js` and `style.css`.

**`app.js`**: Vanilla JavaScript (~300 lines). Features:
- On load: `fetch('/api/logs?limit=50')` → render table rows.
- Pagination: "Next" / "Prev" buttons, track offset.
- Filters: dropdowns for status code (200/4xx/5xx/all), text input for path.
- Row click: fetch `/api/logs/{id}` → show detail overlay with full request/response bodies (pre-formatted, scrollable).
- Auto-refresh toggle (polls every 5 seconds).

**`style.css`**: System font stack, table with alternating rows, sticky header, responsive width. Dark mode via `prefers-color-scheme`. Minimal — under 150 lines.

### 5.7. `internal/web/embed.go`

```go
package web

import "embed"

//go:embed static/*
var StaticFS embed.FS
```

Used in `management/server.go`:
```go
http.FileServer(http.FS(sub))  // where sub, _ = fs.Sub(web.StaticFS, "static")
```

### 5.8. Tests for Phase 3

| Test file | What it tests |
|---|---|
| `internal/management/server_test.go` | Auth middleware: valid creds pass, invalid get 401; routes resolve |
| `internal/management/health_test.go` | Returns valid JSON; db_connected reflects actual state |
| `internal/management/api_test.go` | Query param parsing; pagination; filter combinations; 404 on bad ID |

---

## 6. Phase 4 — CLI (`log` subcommand)

### 6.1. `cmd/log.go`

```go
// logCmd implements `memoryelaine log`
//
// Flags:
//   -f, --format string    Output format: "json" (default), "table", "jsonl"
//   -n, --limit int        Number of records (default 20)
//   --offset int           Pagination offset
//   --status int           Filter by status code
//   --path string          Filter by request path
//   --since string         ISO 8601 or relative ("1h", "30m", "7d")
//   --until string         ISO 8601 or relative
//   -q, --query string     Search body content
//   --id int               Show single record by ID (overrides other filters)
//
// Behavior:
// 1. Load config (only needs database.path).
// 2. Open DB with OpenReader.
// 3. Build QueryFilter from flags.
// 4. Query and format output to stdout.
//
// Output formats:
//   json:  Pretty-printed JSON array (compatible with jq: `memoryelaine log | jq '.[].status_code'`)
//   jsonl: One JSON object per line (for streaming pipelines)
//   table: Human-readable ASCII table (truncated bodies)
```

**Technique:** Use `encoding/json.NewEncoder(os.Stdout)` with `SetIndent` for `json` format. For `table`, use `text/tabwriter`.

**Duration parsing for `--since`/`--until`:** Implement a small helper:
```go
// parseTimeArg parses "2024-01-15T10:00:00Z" or relative durations "1h", "30m", "7d"
// into a Unix millisecond timestamp.
func parseTimeArg(s string) (int64, error)
```

---

## 7. Phase 5 — TUI (`tui` subcommand)

### 7.1. Files

| File | Purpose |
|---|---|
| `internal/tui/app.go` | Entry point: creates bubbletea.Program |
| `internal/tui/model.go` | Root Model: manages focus, sub-models |
| `internal/tui/table.go` | Scrollable table of log entries |
| `internal/tui/detail.go` | Detail view: full headers + body |

### 7.2. `internal/tui/app.go`

```go
package tui

import (
    tea "github.com/charmbracelet/bubbletea"
    "github.com/charmbracelet/lipgloss"
)

// Run starts the TUI application. Blocks until the user quits.
func Run(reader *database.LogReader) error
```

### 7.3. `internal/tui/model.go`

```go
type mode int
const (
    modeTable mode = iota
    modeDetail
)

type Model struct {
    mode       mode
    table      TableModel
    detail     DetailModel
    reader     *database.LogReader
    filter     database.QueryFilter
    err        error
    width      int
    height     int
}

// Messages
type logsLoadedMsg struct { entries []database.LogEntry; total int64 }
type logDetailMsg  struct { entry *database.LogEntry }
type errMsg        struct { err error }

func initialModel(reader *database.LogReader) Model
func (m Model) Init() tea.Cmd
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m Model) View() string
```

**Key interactions:**
- `j/k` or `↑/↓`: navigate table rows
- `Enter`: open detail view for selected row
- `Esc` or `q` in detail: back to table
- `/`: focus search input
- `f`: cycle status filter
- `r`: refresh
- `q` in table: quit

### 7.4. `internal/tui/table.go`

```go
type TableModel struct {
    entries   []database.LogEntry
    cursor    int
    offset    int
    total     int64
    columns   []column  // {title, width, accessor func}
}

func (t TableModel) View(width, height int) string
```

**Technique:** Use `lipgloss` for styling. Render column headers + visible rows based on terminal height. Highlight cursor row.

### 7.5. `internal/tui/detail.go`

```go
type DetailModel struct {
    entry    *database.LogEntry
    scroll   int // vertical scroll offset
    tab      int // 0=overview, 1=req headers, 2=req body, 3=resp headers, 4=resp body
}

func (d DetailModel) View(width, height int) string
```

**Tabs** rendered at top; body content in a scrollable viewport (use `charmbracelet/bubbles/viewport`).

---

## 8. Phase 6 — Prune Command

### 8.1. `cmd/prune.go`

```go
// pruneCmd implements `memoryelaine prune`
//
// Flags:
//   --keep-days int      Required. Delete records older than this many days.
//   --vacuum             Optional. Run VACUUM after deletion. (Warns: may be slow on large DBs.)
//   --dry-run            Optional. Print count of records that would be deleted without deleting.
//
// Behavior:
// 1. Load config (database.path).
// 2. Open DB with OpenWriter (since we're doing a DELETE).
// 3. Compute cutoff: now() - keep-days in Unix ms.
// 4. If --dry-run: SELECT COUNT(*) WHERE ts_start < cutoff, print, exit.
// 5. Else: DELETE, print count, optionally VACUUM.
```

---

## 9. Phase 7 — `cmd/serve.go` Full Wiring

This is where all components are composed.

```go
// serveCmd implements `memoryelaine serve`
//
// Lifecycle:
// 1. Load config.
// 2. Set up slog (JSON handler to stdout).
// 3. Open DB writer (OpenWriter) and DB reader (OpenReader).
// 4. Create LogWriter, start its Run goroutine.
// 5. Create Prometheus Metrics.
// 6. Parse upstream URL.
// 7. Build httputil.ReverseProxy via proxy.NewReverseProxy.
// 8. Build proxy HTTP handler via proxy.Handler.
// 9. Build management mux via management.NewMux.
// 10. Start proxy http.Server on proxy port (goroutine).
// 11. Start management http.Server on management port (goroutine).
// 12. Block on signal (SIGINT, SIGTERM).
// 13. Graceful shutdown:
//     a. Stop accepting new connections (server.Shutdown with 30s timeout).
//     b. Cancel LogWriter context (drain queue).
//     c. Close DB handles.
// 14. Exit.
```

**Graceful shutdown detail:**
```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()

// ... start servers ...

<-ctx.Done()
slog.Info("shutting down")

shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

proxyServer.Shutdown(shutCtx)
mgmtServer.Shutdown(shutCtx)
writerCancel()  // cancel the LogWriter's context
wg.Wait()       // wait for LogWriter.Run to return (drains queue)
writerDB.Close()
readerDB.Close()
```

---

## 10. Phase 8 — Dockerfile & Deployment

### 10.1. `Dockerfile`

```dockerfile
# Build stage
FROM golang:1.22-bookworm AS builder
RUN apt-get update && apt-get install -y gcc libc6-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w \
    -X memoryelaine/internal/version.Version=$(git describe --tags --always) \
    -X memoryelaine/internal/version.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /memoryelaine .

# Runtime stage
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /memoryelaine /usr/local/bin/memoryelaine
RUN mkdir -p /data
VOLUME ["/data"]
EXPOSE 8000 8080
ENTRYPOINT ["memoryelaine"]
CMD ["serve", "--config", "/data/config.yaml"]
```

### 10.2. `docker-compose.yaml`

```yaml
version: "3.8"
services:
  memoryelaine:
    build: .
    ports:
      - "8000:8000"
      - "8080:8080"
    volumes:
      - ./data:/data
    environment:
      - TZ=UTC
```

---

## 11. Phase 9 — Integration & Acceptance Tests

These tests validate the acceptance criteria from the PRD.

| Test | Technique |
|---|---|
| **Zero-Latency Streaming** | Start proxy in-process. Upstream: `httptest.Server` that writes 10 SSE chunks with 50ms gaps. Client reads chunks and asserts each arrives within 100ms of being sent (not batched). After completion, verify DB row exists with full concatenated body. |
| **Truncation** | Upstream returns 15MB body. After proxy completion: verify `resp_body` length = 8MB, `resp_truncated = true`, `resp_bytes = 15728640`. Verify client received all 15MB (SHA-256 match). |
| **Concurrency** | Launch `serve` in-process. In parallel goroutines: 100 proxy requests + repeated `reader.Query` calls. Assert zero `database locked` errors. Use `testing.T.Parallel()`. |
| **Fail-Open** | Start proxy. Make DB read-only (`os.Chmod`). Send 5 requests. Assert all get 200. Assert `DroppedCount() >= 5` or slog output contains error. |
| **Redaction** | Send request with `Authorization: Bearer sk-test123`. Query DB row. Parse `req_headers_json`. Assert no `Authorization` key. Assert other headers (e.g., `Content-Type`) are present. |

**Test file:** `integration_test.go` in the repository root (build tag: `//go:build integration`).

---

## 12. Dependency Summary

| Dependency | Version | Purpose |
|---|---|---|
| `github.com/spf13/cobra` | latest | CLI framework |
| `github.com/spf13/viper` | latest | Config loading |
| `github.com/mattn/go-sqlite3` | latest | SQLite driver (CGO) |
| `github.com/prometheus/client_golang` | latest | Metrics |
| `github.com/charmbracelet/bubbletea` | latest | TUI framework |
| `github.com/charmbracelet/lipgloss` | latest | TUI styling |
| `github.com/charmbracelet/bubbles` | latest | TUI components (viewport) |

No ORM. No web framework. Standard library `net/http`, `encoding/json`, `log/slog`, `net/http/httputil`, `database/sql`.

---

## 13. Implementation Order Summary

| Phase | Deliverable | Est. effort |
|---|---|---|
| 1 | Config + DB layer + models + writer + reader | Foundation |
| 2 | Reverse proxy with capture + streaming | Core |
| 3 | Management server + Web UI + metrics + health | Operations |
| 4 | CLI `log` command | Query tool |
| 5 | TUI | Interactive tool |
| 6 | Prune command | Maintenance |
| 7 | `serve` full wiring + graceful shutdown | Integration |
| 8 | Dockerfile + compose | Deployment |
| 9 | Integration + acceptance tests | Validation |

Phases 4, 5, and 6 are independent of each other and can be parallelized after Phase 3. Phase 7 is the final integration that depends on all prior phases.
