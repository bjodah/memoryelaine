# Product Requirements Document: `memoryelaine`

## 1. Overview
**memoryelaine** is a high-performance, single-binary Go middleware proxy for LLM inference APIs (primarily OpenAI-compatible endpoints). It sits transparently between clients and a single fixed upstream provider. Its primary purpose is to pass through requests with **zero added latency** while asynchronously logging request/response payloads, timings, and HTTP metadata to a local SQLite database for local auditing and debugging.

The system features concurrent access tools—a CLI, a Terminal UI (TUI), and a Web UI—that can query the database safely while the proxy is actively serving traffic.

## 2. Explicit Non-Goals
*   **Fail-Closed Proxying:** Zero-latency streaming is the priority. If database logging fails (e.g., disk full), the proxy will **fail-open**: it will log an application error to `stdout` but will *continue* to stream the LLM response to the client.
*   **Dynamic Routing/Load Balancing:** The proxy routes to a single, statically configured upstream base URL. (If dynamic routing / load balancing is required, it should be handled either before or after this proxy, this is in the interest of keeping the scope of this proxy small and focused).
*   **Upstream Authentication Management:** The proxy does not inject or manage API keys. It passes through what the client sends (but redacts sensitive headers from the *database logs*).
*   **TLS Termination:** Designed for intranet environments; TLS termination (if needed) is handled by an external reverse proxy (e.g., Nginx, Traefik).
*   **Payload Modification:** Aside from redaction in the logs, the proxy strictly does not alter the bytes of the request or response streams.

## 3. Core Interfaces (Subcommands)
The single compiled binary (`memoryelaine`) exposes four distinct subcommands:

1.  **`memoryelaine serve`**: Starts the main proxy, the management Web UI, and the background logging workers.
2.  **`memoryelaine log`**: A headless CLI for querying logs (e.g., `-f json`, `-r 10`, `--status 500`). Output formats must remain compatible with existing `jq` pipelines.
3.  **`memoryelaine tui`**: An interactive terminal UI (built via `charmbracelet/bubbletea`) for browsing and filtering logs.
4.  **`memoryelaine prune --keep-days <N>`**: A manual utility command to delete database records older than `<N>` days. Replaces complex automated background retention jobs.

## 4. Architecture & Networking

### 4.1. Dual-Port Design
To avoid path collisions, `memoryelaine serve` binds to two distinct ports:
*   **Proxy Port (e.g., `:8000`)**: Exclusively handles upstream proxying. No UI or internal endpoints exist here.
*   **Management Port (e.g., `:8080`)**: Exposes the Web UI (`/`), Prometheus metrics (`/metrics`), and application health (`/health`).

### 4.2. Path Allowlisting
Not all traffic passing through the proxy requires payload logging (e.g., health checks or model list lookups).
*   The config will define a `log_paths` array (e.g., `["/v1/chat/completions", "/v1/completions"]`).
*   Requests matching these paths are logged to the database. All other paths are proxied transparently but bypass the database entirely.

### 4.3. Zero-Latency Streaming
`httputil.ReverseProxy` will be utilized with a custom `http.ResponseWriter` wrapper. The wrapper must explicitly implement `http.Flusher`. As chunks arrive from the upstream (e.g., SSE streams), they are immediately written to the client and flushed.

## 5. Data Capture & Logging Policy

### 5.1. The configurable Hard Limit & Truncation
Buffering unbounded SSE streams in memory will crash the proxy.
*   The proxy will tee the request and response streams into an in-memory `bytes.Buffer`.
*   A hard limit (default is **8 Megabytes**) is enforced for memory capture.
*   If a stream exceeds the hard limit, the proxy **stops capturing** bytes to memory, but **continues streaming** the network response to the client uninterrupted.
*   The resulting database record will store the first contents up to the hard limit, set `req_truncated: true` or `resp_truncated: true`, and store the actual final byte count in `req_bytes` or `resp_bytes`.
*   The option (integer) is `max_capture_bytes` and defaults to 8388608 (i.e. 8*1024*1024).

### 5.2. Async Queueing
Database writes must not block the HTTP handler. Once a request finishes, the captured data object is sent to a bounded Go channel (e.g., capacity 1000). A background worker reads from this channel and executes a single `INSERT` into SQLite. If the channel is full, the proxy will drop the log and emit an `slog.Error`.

### 5.3. Redaction
The `Authorization`, `Cookie`, and `Set-Cookie` headers **MUST** be stripped before the headers are serialized to JSON and saved to the database.

## 6. Database Schema & Concurrency

### 6.1. SQLite Concurrency (WAL)
Every process (`serve`, `log`, `tui`, `prune`) connecting to the database must execute the following PRAGMAs to prevent `database locked` errors:
```sql
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA busy_timeout=5000;
```

### 6.2. Upgraded Schema
```sql
CREATE TABLE IF NOT EXISTS openai_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts_start INTEGER NOT NULL,          -- Unix timestamp (milliseconds)
    ts_end INTEGER,                     -- Unix timestamp (milliseconds)
    duration_ms INTEGER,                -- Total request duration
    client_ip TEXT,                     
    request_method TEXT NOT NULL,
    request_path TEXT NOT NULL,         -- Just the path, e.g., /v1/chat/completions
    upstream_url TEXT NOT NULL,         -- Full resolved upstream URL
    status_code INTEGER,
    req_headers_json TEXT,              -- Redacted JSON
    resp_headers_json TEXT,             -- JSON
    req_body TEXT,                      -- Up to hard limit (default 8 MiB)
    req_truncated BOOLEAN DEFAULT 0,
    req_bytes INTEGER,                  -- Actual total bytes
    resp_body TEXT,                     -- Up to hard limit (default 8 MiB)
    resp_truncated BOOLEAN DEFAULT 0,
    resp_bytes INTEGER,                 -- Actual total bytes
    error TEXT                          -- e.g., "upstream timeout", empty if none
);

CREATE INDEX idx_ts_start ON openai_logs(ts_start);
CREATE INDEX idx_status_code_ts ON openai_logs(status_code, ts_start);
CREATE INDEX idx_path_ts ON openai_logs(request_path, ts_start);
```

## 7. Configuration Specification
Configuration is exclusively driven by a `config.yaml` file (parsed via `viper`). An `example-config.yaml` must be included in the repository.

```yaml
# example-config.yaml
proxy:
  listen_addr: "0.0.0.0:8000"
  upstream_base_url: "https://api.openai.com"
  timeout_minutes: 23
  log_paths:
    - "/v1/chat/completions"
    - "/v1/completions"

management:
  listen_addr: "0.0.0.0:8080"
  auth:
    username: "admin"
    password: "changeme"

database:
  path: "./memoryelaine.db"

logging:
  max_capture_bytes: 8388608 # 8MB
```

## 8. Observability & Security

### 8.1. Application Logging (`slog`)
The proxy will use Go's standard library `log/slog` to output structured JSON logs to `stdout`. This tracks the *health of the proxy itself* (e.g., startup events, DB connection errors, dropped logs due to full queues, panic recoveries).

### 8.2. Management Endpoints (Port 8080)
*   **`/`**: Web UI (HTML/JS table of logs, embedded via `//go:embed`). Protected by Basic Auth.
*   **`/api/logs`**: JSON endpoint backing the Web UI. Protected by Basic Auth.
*   **`/metrics`**: Standard Prometheus metrics (RPS, status codes, latency, active streams). Protected by Basic Auth.
*   **`/health`**: Public JSON endpoint indicating app health (e.g., `{"status": "ok", "db_connected": true, "dropped_logs": 0}`). *Not* protected by Basic Auth for load-balancer compatibility.

## 9. Deployment
*   **Containerization**: Provided via a multi-stage Dockerfile.
    *   *Build:* `golang:1.22-bookworm` (CGO requires GCC).
    *   *Runtime:* `debian:bookworm-slim`.
*   Data must be stored in a standard volume mount location (e.g., `/data/memoryelaine.db`).

## 10. Acceptance Criteria (For Implementation)
1.  **Zero-Latency Streaming Test:** A client connecting to the proxy and requesting an SSE stream receives chunks exactly as they are emitted by the upstream, with no buffering delays, while the DB successfully captures the data in the background.
2.  **Truncation Test:** A request returning 15MB of data correctly logs the first 8MB to SQLite, marks `resp_truncated: true`, logs `resp_bytes: 15728640`, and successfully delivers all 15MB to the client.
3.  **Concurrency Test:** Running `memoryelaine serve`, `memoryelaine log -f json`, and `memoryelaine tui` simultaneously against the same SQLite file under load yields no `database locked` errors.
4.  **Fail-Open Test:** If the SQLite database file permissions are changed to read-only while the proxy is running, the proxy logs `slog` errors to stdout but continues to serve HTTP 200s to clients.
5.  **Redaction Test:** A request containing `Authorization: Bearer sk-...` is processed. The database record's `req_headers_json` explicitly lacks the `Authorization` key.

## 11. Implementation plan

Based on this specification, and an addendum specified below, a plan for implementing the software has been authored in the sibling file `01-IMPLEMENTATION_PLAN.md`. That plan is ready for implementation. Every file, every function signature, every data structure, and the technique for each non-trivial piece is specified. The phases are ordered so each produces a testable artifact, and the critical streaming capture design (using `teeReadCloser` on both request and response bodies, flowing naturally through the reverse proxy) is explicitly called out with its rationale.

<addendum incorporated_in="01-IMPLEMENTATION_PLAN.md">
## Addendum: Ambiguities & Open Questions

### 1. Streaming Body Capture Mechanics (Critical)

The PRD says we tee request/response streams into a `bytes.Buffer` with an 8 MiB cap. But the **request body** and **response body** have very different lifecities:

- **Request body**: Available as `r.Body` before proxying. We can read it fully (up to cap), buffer it, then replay it via `io.NopCloser` to the upstream. This is straightforward.
- **Response body**: With `httputil.ReverseProxy`, we don't directly control the response body stream in the same way. The standard approach is a custom `http.ResponseWriter` that wraps `Write()` to tee bytes into a buffer. But we also need to track `resp_bytes` (the **total** bytes, even beyond the cap). So the `Write()` wrapper must always count but conditionally stop copying.

**Decision I'll encode in the plan:** Use `ModifyResponse` to wrap the `Body` with a `teeReadCloser` that captures up to the limit and counts all bytes. This is cleaner than wrapping the ResponseWriter for body capture (we still wrap ResponseWriter for status code capture and Flusher delegation).

### 2. `req_bytes` / `resp_bytes` — Does This Include Only Body, or Headers Too?

The schema has `req_body` and `req_bytes` as separate columns. The natural reading is that `req_bytes` is the **body byte count** (not including headers). I'll go with that interpretation.

### 3. `ts_end` and `duration_ms` on Errored Requests

If the upstream is unreachable (dial error), we still create a log entry. `ts_end` = time of error, `status_code` = `NULL` (or 502?), `error` = the error string. The PRD doesn't specify what status code the proxy itself returns to the client on upstream failure. I'll assume standard reverse proxy behavior: 502 Bad Gateway.

### 4. Web UI Scope

The PRD says "HTML/JS table of logs, embedded via `go:embed`". I'll treat this as a single-page app with a simple paginated table, filter by status/path, and a detail view. No frameworks — vanilla HTML/JS/CSS. This keeps scope contained.

### 5. TUI Scope

The PRD mentions `charmbracelet/bubbletea` but doesn't detail the interaction model. I'll define it as: a filterable, scrollable table with a detail pane (split view). Filters: status code, path, time range, full-text search on bodies.

### 6. `timeout_minutes: 23` — This Is the Upstream Timeout?

This is presumably the `http.Transport` / dialer timeout for the upstream connection. 23 minutes is long but reasonable for long-running LLM inference. I'll apply it as the full round-trip timeout on the outbound HTTP client (not a per-chunk idle timeout). The PRD doesn't mention an idle/read timeout — I'll add a note that this could be refined.

### 7. Config File Discovery

The PRD says "exclusively driven by `config.yaml`" but doesn't specify lookup order. I'll use: `--config` flag → `./config.yaml` → `$HOME/.config/memoryelaine/config.yaml`.

### 8. Channel-Full Drop: Should We Count Drops?

The `/health` endpoint shows `dropped_logs`. This means we need an atomic counter incremented on channel-full drops. Simple, but needs to be in the plan.

### 9. Prune Command — Soft or Hard Delete?

The PRD says "delete database records older than N days." I'll use hard `DELETE` + `VACUUM` (optionally, since VACUUM rewrites the entire DB and can be slow; I'll make it a flag `--vacuum`).
</addendum>
