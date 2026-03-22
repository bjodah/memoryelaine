# Product Specification v2: `memoryelaine`

## 1. Overview

`memoryelaine` is a single-binary Go middleware proxy for OpenAI-compatible
inference APIs. It sits transparently between clients and one fixed upstream
provider. Its primary purpose is to proxy requests with no intentional buffering
of active streams while asynchronously logging selected request/response pairs,
timings, and HTTP metadata to a local SQLite database.

The system exposes multiple ways to inspect stored logs:

- a CLI (`memoryelaine log`)
- a Terminal UI (`memoryelaine tui`)
- a Web UI and JSON API on the management port

For streamed responses, the system stores the raw captured response body as the
canonical record. Viewers may additionally offer a derived `Stream view mode`
with:

- `Raw`: exact stored response body
- `Assembled`: reconstructed text for supported streamed endpoints, including a
  partial-warning state when recovery is possible from an interrupted stream

## 2. Goals

- Proxy requests to a single configured upstream base URL.
- Preserve zero-added-latency intent for active response streams.
- Capture request and response bodies up to a configurable in-memory limit.
- Write logs asynchronously so database work does not block request handling.
- Provide local inspection tools for raw captured traffic.
- Provide a readable assembled view for supported streamed responses in the TUI
  and Web UI without changing database storage format.

## 3. Non-Goals

- Dynamic routing, failover, or upstream load balancing.
- TLS termination inside `memoryelaine`.
- Upstream authentication management or API key injection.
- Modification of proxied request or response bytes.
- Persisting assembled stream output as a separate database representation.
- Broad first-version support for every possible OpenAI-compatible streaming
  dialect.

## 4. Core Commands

The binary exposes four subcommands:

1. `memoryelaine serve`
   Starts the proxy listener and the management listener.
2. `memoryelaine log`
   Queries stored logs from the command line.
3. `memoryelaine tui`
   Opens the interactive terminal UI for browsing logs.
4. `memoryelaine prune --keep-days <N> [--dry-run] [--vacuum]`
   Deletes old records from the SQLite database.

## 5. Runtime Architecture

### 5.1 Dual-Port Design

`memoryelaine serve` binds to two distinct listeners:

- Proxy port, for upstream proxying only
- Management port, for health, metrics, Web UI, and JSON API

The listen addresses must not be identical.

### 5.2 Path Allowlist for Logging

Only requests whose `request_path` exactly matches an entry in
`proxy.log_paths` are captured and written to SQLite.

All other requests are still proxied, but bypass database logging entirely.

Path matching is exact string matching. No glob, regex, or prefix matching is
performed.

### 5.3 Runtime Recording State

`memoryelaine serve` maintains an in-memory runtime recording state:

- `recording=true`: requests on `proxy.log_paths` are captured and logged
- `recording=false`: requests on `proxy.log_paths` are still proxied, but
  request/response capture to SQLite is bypassed entirely

The recording decision is taken at request start.

If a request begins while `recording=true`, it remains fully loggable for its
lifetime even if recording is later disabled while that request is still
in-flight.

### 5.4 Zero-Latency Streaming Intent

For captured paths, response streaming must remain pass-through in behavior:

- active streamed responses are forwarded immediately
- SSE responses are not intentionally buffered before being written to the
  client
- request and response capture is performed by teeing bytes as they flow

If logging fails, the proxy must fail open and continue serving the client.

## 6. Data Capture and Logging

### 6.1 Capture Strategy

For loggable paths:

- the request body is captured via a streaming tee as the upstream transport
  reads it
- the response body is captured via a streaming tee attached to the upstream
  response body

This design prioritizes streaming behavior over complete capture in failure
scenarios. For example, if the upstream fails before fully consuming the request
body, the stored request body may be partial.

### 6.2 Capture Limit and Truncation

The proxy captures request and response bodies in memory up to
`logging.max_capture_bytes` per direction.

If a body exceeds that limit:

- streaming to the client continues uninterrupted
- the stored body contains only the first `max_capture_bytes`
- `req_truncated` or `resp_truncated` is set to `true`
- `req_bytes` or `resp_bytes` records the total body bytes observed

### 6.3 Async Database Writes

Completed log entries are sent to a bounded in-process queue and inserted by a
background worker.

If the queue is full:

- the log entry is dropped
- the proxy continues serving the client
- the dropped-log counter increases
- an application error is logged to structured stdout

### 6.4 Last Captured Bodies and Staleness

The service maintains in-memory "last captured request body" and "last captured
response body" values for the `/last-request` and `/last-response` endpoints.

If at least one request on a loggable path is proxied while `recording=false`,
those endpoints become stale until a newly captured request or response body
replaces the corresponding stored value.

When stale, the endpoint must clearly label the returned plain-text value as
stale.

### 6.5 Redaction

The following headers must be removed before storing request or response headers
in the database:

- `Authorization`
- `Cookie`
- `Set-Cookie`

### 6.6 Raw Storage Is Canonical

The database stores raw captured request and response bodies only. No assembled
stream representation is stored in SQLite.

Derived assembled views are computed at read/render time by supported viewers.

## 7. Stream View Mode

### 7.1 Purpose

Raw SSE logs are valuable for debugging but often hard to read. Stream view mode
solves a presentation problem, not a storage problem.

### 7.2 Modes

Supported viewers may offer:

- `Raw`: exact stored response body, including SSE framing and event boundaries
- `Assembled`: derived text reconstructed from supported streamed response
  formats, or a partial assembled result with explicit warning metadata when
  recovery is possible

### 7.3 Scope of Assembled Mode

The first supported paths are:

- `/v1/chat/completions`
- `/v1/completions`

Assembled mode is only available when:

- the request path is supported
- a response body is present
- the stored response body is not truncated
- the body appears to be a parseable SSE stream (contains `data:` lines; when
  stored response headers are available, a `content-type` of
  `text/event-stream` serves as additional confirmation; when headers are
  absent, body inspection alone is sufficient)
- parsing succeeds with at least some recovered text content, or parsing
  partially succeeds with recoverable text

If any of these conditions fail, viewers must fall back to `Raw`.

### 7.4 Parsing Rules

For `/v1/chat/completions`:

- assemble text from streamed `choices[0].delta.content` fragments
- reject multi-choice streams as unsupported in v2
- reject tool-call / function-call streams as unsupported in v2
- handle empty `choices` arrays (e.g. usage-only chunks) by skipping the event
- handle `choices[0].delta.content` being JSON `null` or absent by skipping
  the event

For `/v1/completions`:

- assemble text from streamed `choices[0].text` fragments
- reject multi-choice streams as unsupported in v2
- handle empty `choices` arrays by skipping the event
- handle `choices[0].text` being JSON `null` or absent by skipping the event

In v2, assembled rendering is defined only for single-choice text streams.

If the stream parses completely but yields no text content (e.g. role-only or
usage-only deltas), assembled mode is unavailable and the viewer falls back to
`Raw`.

If parsing fails only after some valid text has already been recovered, viewers
may present the recovered text with a partial-warning state rather than
discarding it entirely.

### 7.5 Viewer Scope

Stream view mode is required in:

- the Terminal UI
- the Web UI

It is not required in `memoryelaine log` in v2.

## 8. Database

### 8.1 Storage Engine

SQLite is the sole supported database in v2.

Every process connecting to the database must use WAL-compatible settings to
allow concurrent reads while the proxy is writing.

### 8.2 Schema

```sql
CREATE TABLE IF NOT EXISTS openai_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts_start INTEGER NOT NULL,
    ts_end INTEGER,
    duration_ms INTEGER,
    client_ip TEXT,
    request_method TEXT NOT NULL,
    request_path TEXT NOT NULL,
    upstream_url TEXT NOT NULL,
    status_code INTEGER,
    req_headers_json TEXT,
    resp_headers_json TEXT,
    req_body TEXT,
    req_truncated BOOLEAN DEFAULT 0,
    req_bytes INTEGER,
    resp_body TEXT,
    resp_truncated BOOLEAN DEFAULT 0,
    resp_bytes INTEGER,
    error TEXT
);

CREATE INDEX IF NOT EXISTS idx_ts_start ON openai_logs(ts_start);
CREATE INDEX IF NOT EXISTS idx_status_code_ts ON openai_logs(status_code, ts_start);
CREATE INDEX IF NOT EXISTS idx_path_ts ON openai_logs(request_path, ts_start);
```

Interpretation:

- `req_bytes` and `resp_bytes` count body bytes only
- `status_code` may be null when an upstream error occurs before a response is
  available
- `resp_body` contains the raw captured response body, not an assembled view

## 9. Configuration

Configuration is file-based YAML loaded in this order:

1. `--config <path>` if provided
2. `./config.yaml`
3. `$HOME/.config/memoryelaine/config.yaml`
4. built-in defaults

Example:

```yaml
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
  max_capture_bytes: 8388608
  level: "info"
```

### 9.1 `proxy`

- `listen_addr`: proxy listener address
- `upstream_base_url`: fixed upstream base URL, must be valid `http` or `https`
- `timeout_minutes`: connection setup / time-to-first-byte timeout budget; this
  does not terminate an already-active response stream
- `log_paths`: exact allowlist of paths to capture and log

### 9.2 `management`

- `listen_addr`: management listener address
- `auth.username`: Basic Auth username
- `auth.password`: Basic Auth password
- `preview_bytes`: maximum bytes returned in body preview responses via
  `/api/logs/{id}/body` (default: 65536)

### 9.3 `database`

- `path`: SQLite database path

### 9.4 `logging`

- `max_capture_bytes`: maximum captured request or response body bytes persisted
  per direction
- `level`: structured application log level; accepted values are `debug`,
  `info`, `warn`, `error`

## 10. Management Port

### 10.1 Authentication

Basic Auth is required for all management endpoints except `/health`.

### 10.2 Endpoints

- `GET /`
  Embedded Web UI.
- `GET /api/logs`
  Log summaries (no bodies or headers). Returns paginated metadata only.
- `GET /api/logs/{id}`
  Log detail metadata with decoded headers and stream-view availability. No
  bodies are included in this response.
- `GET /api/logs/{id}/body`
  Request or response body content. Accepts `part` (req|resp, default: resp),
  `mode` (raw|assembled, default: raw), and `full` (true|false, default:
  false) query parameters. Body previews are limited to
  `management.preview_bytes` (default: 65536) unless `full=true`.
- `GET /api/recording`
  Authenticated JSON endpoint returning the current runtime recording state.
- `PUT /api/recording`
  Authenticated JSON endpoint for changing the runtime recording state.
- `GET /last-request`
  Latest captured request body as plain text.
- `GET /last-response`
  Latest captured response body as plain text.
- `GET /metrics`
  Prometheus scrape endpoint.
- `GET /health`
  Public JSON health endpoint.

### 10.3 `/api/logs` Query Parameters

`GET /api/logs` accepts a `query` parameter containing a DSL string (see
§10.3.1), plus `limit` (integer, max 1000) and `offset` (integer).

When `query` is absent, legacy parameters are accepted as fallback:

- `status`: exact status code
- `path`: exact request path
- `since`: unix timestamp in milliseconds
- `until`: unix timestamp in milliseconds
- `q`: substring search across `req_body` and `resp_body`

Response shape:

```json
{
  "data": [/* log summaries */],
  "total": 123
}
```

#### 10.3.1 Query DSL

The `query` parameter accepts a search string combining free-text and
structured filters:

- Bare words: full-text search (FTS5) across request and response bodies
- `status:200` or `status:4xx` — filter by status code or wildcard range
- `method:POST` — filter by HTTP method
- `path:/v1/chat/completions` — filter by request path
- `since:1h` or `since:2024-01-01T00:00:00Z` — entries after time
- `until:24h` or `until:2024-01-01T00:00:00Z` — entries older than time
- `is:error`, `is:truncated` — flag filters
- `has:req-body`, `has:resp-body` — body presence filters
- `-status:500` — negate any filter
- `"exact phrase"` — quoted phrase search

Example: `status:2xx method:POST path:/chat hello world`

### 10.4 `/api/logs/{id}` Detail Response

The detail endpoint returns log metadata plus decoded request and response
headers and stream-view availability. Bodies are not included; use
`/api/logs/{id}/body` to retrieve body content.

Response shape:

```json
{
  "entry": { /* log metadata with decoded headers */ },
  "stream_view": {
    "assembled_available": true,
    "reason": "supported"
  }
}
```

Notes:

- `reason` is machine-stable and indicates whether assembled mode is fully
  available, partially available, or unavailable
- `reason` values may include `supported`, `partial_parse`, `truncated`,
  `unsupported_path`, `unsupported_multi_choice`,
  `unsupported_tool_call_stream`, `no_text_content`, `not_sse`,
  `missing_body`, and `parse_failed`

### 10.4.1 `/api/logs/{id}/body`

Retrieves request or response body content for a single log entry.

Query parameters:

- `part`: `req` or `resp` (default: `resp`)
- `mode`: `raw` or `assembled` (default: `raw`)
- `full`: `true` or `false` (default: `false`)

When `full=false`, the response is limited to `management.preview_bytes`
(default: 65536). When `full=true`, the complete stored body is returned.

When `mode=assembled`, the endpoint returns the derived assembled text for
supported streamed responses. If assembly is unavailable, the endpoint falls
back to the raw body.

### 10.5 `/last-request` and `/last-response`

These endpoints return the most recently captured request or response body as
plain text.

Because request and response capture happen independently during an active
exchange, they may briefly be out of sync during in-flight traffic.

If one or more loggable requests have been proxied while recording is disabled,
these endpoints must label their values as stale until a newly captured body
replaces the corresponding stored value.

### 10.6 `/api/recording`

`GET /api/recording` returns:

```json
{
  "recording": true
}
```

`PUT /api/recording` accepts:

```json
{
  "recording": false
}
```

and returns the new state in the same response shape.

### 10.7 `/health`

`GET /health` returns JSON similar to:

```json
{
  "status": "ok",
  "db_connected": true,
  "dropped_logs": 0,
  "recording": true,
  "uptime_seconds": 123
}
```

## 11. CLI Behavior

### 11.1 `memoryelaine log`

Supported flags:

- `-f, --format`: `json`, `jsonl`, or `table`
- `-n, --limit`: number of records to return
- `--offset`: pagination offset
- `--status`: exact status code filter
- `--path`: exact request path filter
- `--since`: RFC3339 timestamp or relative duration such as `30m`, `2h`, `7d`
- `--until`: RFC3339 timestamp or relative duration
- `-q, --query`: substring search across request and response bodies
- `--id`: return one record by primary key

`table` output includes:

- ID
- time
- method
- path
- status
- duration
- request size
- response size

### 11.2 `memoryelaine prune`

Supported flags:

- `--keep-days` required
- `--dry-run`
- `--vacuum`

Behavior:

- deletes rows whose `ts_start` is older than the requested retention window
- optionally runs `VACUUM`

## 12. TUI Behavior

The TUI is a lightweight browser over stored logs, not a full-screen analytics
console.

Required table behavior:

- navigate rows with `j`/`k` or arrow keys
- open detail view with `enter`
- refresh with `r`
- paginate with `n` and `p`
- cycle quick exact-status filters with `f`
- quit with `q` or `ctrl+c`

Required detail behavior:

- leave detail view with `esc` or `q`
- scroll with `j`/`k`
- display request and response headers and bodies
- for supported streamed responses, toggle `Stream view mode` between `Raw` and
  `Assembled`
- if the assembled result is partial, display a clear warning state

The v2 TUI does not require arbitrary text search, path filters, or time-range
editing from inside the terminal UI.

## 13. Web UI Behavior

The Web UI is a lightweight embedded interface for browsing logs.

Required capabilities:

- table of recent logs
- pagination
- exact status filter
- exact path filter via text input
- body substring search
- manual refresh and optional auto-refresh
- visible runtime recording state
- authenticated toggle of runtime recording state
- detail overlay for a selected log entry
- in the detail overlay, `Stream view mode` with `Raw` and `Assembled` where
  assembled mode is available
- when assembled mode is partial, show a warning state without rendering the
  content as HTML

## 14. Observability and Security

### 14.1 Application Logging

The service writes structured logs to stdout using Go's `log/slog`.

The configured log level controls application log verbosity.

### 14.2 Prometheus

The management port exposes a Prometheus-compatible `/metrics` endpoint.

The exact metric set may include standard Go/process collectors and
implementation-defined application metrics.

### 14.3 Security Posture

- request and response payloads are logged locally to SQLite
- management endpoints are protected by Basic Auth except `/health`
- sensitive headers are redacted from stored headers

## 15. Failure Semantics

- If database writes fail, proxying continues.
- If the logging queue is full, log entries may be dropped.
- If the upstream is unreachable, the proxy returns a 502-style failure to the
  client and still attempts to record an error-bearing log entry.
- If recording is disabled, loggable requests continue to proxy successfully but
  produce no SQLite log entry.
- If recording is disabled during an in-flight request that began while
  recording was enabled, that already-started request may still be fully logged.
- If stream assembly fails in a viewer, the raw stored response remains
  available.

## 16. Acceptance Criteria

1. Active streamed responses are forwarded to clients without intentional
   buffering delays.
2. Captured request and response bodies respect `logging.max_capture_bytes`
   while continuing to stream full responses to the client.
3. `memoryelaine serve`, `memoryelaine log`, `memoryelaine tui`, and
   `memoryelaine prune` can operate safely against the same SQLite database.
4. Sensitive headers are absent from stored header JSON.
5. The Web UI detail endpoint returns both stored log data and derived
   stream-view metadata.
6. The TUI can display supported streamed responses in `Raw` and `Assembled`
   modes.
7. The Web UI can display supported streamed responses in `Raw` and
   `Assembled` modes.
8. For partially recoverable streamed responses, viewers can display recovered
   assembled text with a warning state.
9. For truncated, unsupported, or fully unparsable streamed responses, viewers
   fall back to `Raw`.
10. Recording can be toggled at runtime through the management API and Web UI.
11. `/health` exposes the current recording state.
12. `/last-request` and `/last-response` clearly indicate when their values are
    stale due to paused recording and subsequent loggable traffic.
