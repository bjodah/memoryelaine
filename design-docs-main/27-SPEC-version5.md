# Product Specification v5: `memoryelaine`

## 1. Overview

`memoryelaine` is a single-binary Go middleware proxy for OpenAI-compatible
inference APIs. It sits transparently between clients and one fixed upstream
provider. Its primary purpose is to proxy requests with no intentional buffering
of active streams while asynchronously logging selected request/response pairs,
timings, and HTTP metadata to a local SQLite database.

The inspection surfaces also share a display-oriented JSON ellipsis transform
for long request and response bodies, and the Emacs client adds a dedicated JSON
inspector for canonical full-body viewing.

The system exposes multiple ways to inspect stored logs:

- a CLI (`memoryelaine log`)
- a Terminal UI (`memoryelaine tui`)
- a Web UI and JSON API on the management port
- an Emacs client

For streamed responses, the system stores the raw captured response body as the
canonical record. Viewers may additionally offer a derived `Stream view mode`
with:

- `Raw`: exact stored response body
- `Assembled`: reconstructed text for supported streamed endpoints, including a
  partial-warning state when recovery is possible from an interrupted stream

For `/v1/chat/completions` traffic, the system provides a **conversation view**
that renders the request's message history as a readable thread. The writer
extracts sidecar text for FTS indexing and computes deterministic lineage
hashes to link multi-turn conversations. Viewers can display the
root-to-selected conversation chain with per-turn attribution.

## 2. Goals

- Proxy requests to a single configured upstream base URL.
- Preserve zero-added-latency intent for active response streams.
- Capture request and response bodies up to a configurable in-memory limit.
- Write logs asynchronously so database work does not block request handling.
- Provide local inspection tools for raw captured traffic.
- Provide a readable assembled view for supported streamed responses in the TUI
  and Web UI without changing database storage format.
- Improve FTS for `/v1/chat/completions` by indexing extracted text instead of
  raw SSE fragments.
- Provide a conversation-oriented view for `/v1/chat/completions` traffic
  across all frontends (TUI, Web UI, Emacs).
- Provide shared display-oriented JSON ellipsis for long body previews across
  the Web UI, TUI, and Emacs.

## 3. Non-Goals

- Dynamic routing, failover, or upstream load balancing.
- TLS termination inside `memoryelaine`.
- Upstream authentication management or API key injection.
- Modification of proxied request or response bytes.
- Broad first-version support for every possible OpenAI-compatible streaming
  dialect.
- Storage deduplication across log entries.
- Descendant traversal or branch exploration in conversation view.
- Backward-compatible migration for existing SQLite files.

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

### 5.5 SSE Extractor Injection

The writer uses an injected `SSEExtractor` function to extract assembled text
from streamed responses. This callback is set during `serve` initialization and
delegates to the `streamview.Build` function. The injection avoids an import
cycle between the `database` and `streamview` packages.

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

### 6.4 Chat Enrichment

Before inserting a log entry, the writer performs chat-specific enrichment for
`/v1/chat/completions` requests. This is described fully in §14.

### 6.5 Last Captured Bodies and Staleness

The service maintains in-memory "last captured request body" and "last captured
response body" values for the `/last-request` and `/last-response` endpoints.

If at least one request on a loggable path is proxied while `recording=false`,
those endpoints become stale until a newly captured request or response body
replaces the corresponding stored value.

When stale, the endpoint must clearly label the returned plain-text value as
stale.

### 6.6 Redaction

The following headers must be removed before storing request or response headers
in the database:

- `Authorization`
- `Cookie`
- `Set-Cookie`

### 6.7 Raw Storage Is Canonical

The database stores raw captured request and response bodies as the canonical
record. Derived sidecar text (§8.2) is stored alongside for FTS indexing but
does not replace the raw bodies.

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

The supported paths are:

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
- reject multi-choice streams as unsupported
- reject tool-call / function-call streams as unsupported
- handle empty `choices` arrays (e.g. usage-only chunks) by skipping the event
- handle `choices[0].delta.content` being JSON `null` or absent by skipping
  the event

For `/v1/completions`:

- assemble text from streamed `choices[0].text` fragments
- reject multi-choice streams as unsupported
- handle empty `choices` arrays by skipping the event
- handle `choices[0].text` being JSON `null` or absent by skipping the event

Assembled rendering is defined only for single-choice text streams.

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

It is not required in `memoryelaine log`.

## 8. Database

### 8.1 Storage Engine

SQLite is the sole supported database.

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
    error TEXT,
    parent_id INTEGER REFERENCES openai_logs(id),
    chat_hash TEXT,
    parent_prefix_len INTEGER,
    message_count INTEGER,
    req_text TEXT,
    resp_text TEXT
);

CREATE INDEX IF NOT EXISTS idx_ts_start ON openai_logs(ts_start);
CREATE INDEX IF NOT EXISTS idx_status_code_ts ON openai_logs(status_code, ts_start);
CREATE INDEX IF NOT EXISTS idx_path_ts ON openai_logs(request_path, ts_start);
CREATE INDEX IF NOT EXISTS idx_chat_hash ON openai_logs(chat_hash);
CREATE INDEX IF NOT EXISTS idx_parent_id ON openai_logs(parent_id);
```

Column interpretation:

- `req_bytes` and `resp_bytes` count body bytes only
- `status_code` may be null when an upstream error occurs before a response is
  available
- `resp_body` contains the raw captured response body, not an assembled view
- `parent_id` references the parent log entry in a conversation chain (§14.4)
- `chat_hash` is the SHA-256 hex of the canonicalized message array (§14.3)
- `parent_prefix_len` records how many messages matched the parent request
- `message_count` records total messages in this request
- `req_text` is extracted searchable text for chat endpoints; `NULL` otherwise
- `resp_text` is extracted assistant response text; `NULL` otherwise

### 8.3 Full-Text Search

FTS5 is used for full-text search across request and response content.

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS openai_logs_fts USING fts5(
    req_text,
    resp_text,
    content='openai_logs',
    content_rowid='id'
);
```

FTS triggers use `COALESCE` so that `NULL` sidecar columns fall back to the
raw body for indexing. This ensures all endpoints remain searchable without
duplicating raw bodies into the sidecar columns:

```sql
CREATE TRIGGER IF NOT EXISTS openai_logs_ai AFTER INSERT ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(rowid, req_text, resp_text)
    VALUES (new.id,
            COALESCE(new.req_text, new.req_body),
            COALESCE(new.resp_text, new.resp_body));
END;

CREATE TRIGGER IF NOT EXISTS openai_logs_ad AFTER DELETE ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(openai_logs_fts, rowid, req_text, resp_text)
    VALUES ('delete', old.id,
            COALESCE(old.req_text, old.req_body),
            COALESCE(old.resp_text, old.resp_body));
END;
```

No `AFTER UPDATE` trigger is required because rows are insert-only after
capture.

### 8.4 FTS Rebuild

The built-in FTS5 `'rebuild'` command bypasses triggers and reads columns
directly from the content table. With `NULL` sidecar columns, a rebuild would
silently drop non-chat entries from the FTS index.

Instead, the rebuild uses a manual equivalent that applies `COALESCE`:

```sql
DELETE FROM openai_logs_fts;
INSERT INTO openai_logs_fts(rowid, req_text, resp_text)
    SELECT id, COALESCE(req_text, req_body), COALESCE(resp_text, resp_body)
    FROM openai_logs;
```

This is executed within a transaction.

### 8.5 Rollout Constraint

`CREATE TABLE IF NOT EXISTS` does not upgrade an old database in place. Rollout
for v5 requires a fresh database file. This is acceptable and should be stated
in deployment notes.

## 9. Configuration

Configuration is file-based YAML loaded in this order:

1. `--config <path>` if provided
2. `./config.yaml`
3. `$HOME/.config/memoryelaine/config.yaml`
4. built-in defaults

Example:

```yaml
proxy:
  listen_addr: "0.0.0.0:8688"
  upstream_base_url: "https://api.openai.com"
  timeout_minutes: 23
  log_paths:
    - "/v1/chat/completions"
    - "/v1/completions"

management:
  listen_addr: "0.0.0.0:8677"
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
- `GET /api/logs/{id}/thread`
  Conversation thread for a `/v1/chat/completions` log entry. Returns the
  linear ancestor chain from root to the selected entry with per-message
  attribution. See §10.8.
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
- `q`: compatibility-only free-text search across `req_body` and `resp_body`

Legacy `q` is treated as sanitized literal text input for FTS-backed search.
Clients must not rely on raw SQLite FTS syntax through this parameter.

Response shape:

```json
{
  "data": [/* log summaries */],
  "total": 123,
  "limit": 50,
  "offset": 0,
  "has_more": true
}
```

If the `query` DSL is invalid, the endpoint returns `400 Bad Request` with a
structured error response:

```json
{
  "error": "query_parse_error",
  "message": "invalid status value \"abc\""
}
```

Implementations may include additional machine-readable parser fields such as
`token` and `position`.

#### 10.3.1 Query DSL

The `query` parameter accepts a search string combining free-text and
structured filters:

- Bare words: full-text search (FTS5) across request and response bodies
- `status:200` or `status:4xx` — filter by status code or wildcard range
- `method:POST` — filter by HTTP method
- `path:/v1/chat/completions` — filter by request path
- `since:1h` or `since:2024-01-01T00:00:00Z` — entries after time
- `until:24h` or `until:2024-01-01T00:00:00Z` — entries older than time
- `is:error`, `is:req-truncated`, `is:resp-truncated` — flag filters
- `has:req`, `has:resp` — body presence filters
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
- `ellipsis`: positive integer rune limit for display-oriented JSON string
  shortening (optional)

When `full=false`, the response is limited to `management.preview_bytes`
(default: 65536). When `full=true`, the complete stored body is returned.

When `ellipsis` is present and the source body is valid JSON, the server first
attempts to shorten long string values for display. If that transform changes
the payload, `ellipsized=true` is returned. Preview truncation still applies to
the transformed body when `full=false`, so `ellipsized=true` and
`truncated=true` may both be set.

When `mode=assembled`, the endpoint returns the derived assembled text for
supported streamed responses. If assembly is unavailable, the endpoint returns
`available: false` with a `reason` field explaining why (e.g., `not_sse`,
`truncated`).

Response shape:

```json
{
  "part": "resp",
  "mode": "assembled",
  "full": false,
  "content": "Hello world",
  "included_bytes": 11,
  "total_bytes": 42,
  "truncated": true,
  "ellipsized": false,
  "complete": false,
  "available": true,
  "reason": ""
}
```

Notes:

- `included_bytes` and `total_bytes` describe the bytes for the requested
  representation, not some other representation of the same body. For example,
  `mode=assembled` reports assembled-content byte counts, while `mode=raw`
  reports raw stored-body byte counts.
- `complete=true` means the returned content is the canonical full body with no
  display ellipsis or preview truncation applied.
- For unavailable bodies, `available` is `false`, `content` may be empty, and
  `reason` explains why the requested representation cannot be returned.
- `part=req` with `mode=assembled` is invalid and must return
  `400 Bad Request`.
- `404 Not Found` is returned when the log entry ID does not exist.
- Operational backend failures should return `500 Internal Server Error` rather
  than being reported as not-found.

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

### 10.8 `/api/logs/{id}/thread`

Returns the conversation thread for a `/v1/chat/completions` log entry.

**Guards:**

- If the log entry does not exist: `404 Not Found`
- If the log entry ID is not a valid integer: `400 Bad Request`
- If the log entry's `request_path` does not end with `/chat/completions`:
  `400 Bad Request` with message "thread view is only available for
  /v1/chat/completions requests"
- If `req_truncated == true`: `400 Bad Request` with message "thread view is
  not available for truncated requests"

**Response shape:**

```json
{
  "selected_log_id": 42,
  "selected_entry_index": 6,
  "total_entries": 7,
  "messages": [
    {
      "role": "system",
      "content": "You are a helpful assistant.",
      "log_id": 36,
      "is_complex": false,
      "complexity": ""
    },
    {
      "role": "user",
      "content": "Hello!",
      "log_id": 36,
      "is_complex": false,
      "complexity": ""
    },
    {
      "role": "assistant",
      "content": "Hi there! How can I help?",
      "log_id": 36,
      "is_complex": false,
      "complexity": ""
    }
  ]
}
```

**Definition of entry index:** `selected_entry_index` and `total_entries` count
log entries in the ancestor chain, not individual messages. A root entry
containing `[system, user]` plus its assistant response is entry 0 (one entry),
regardless of how many messages it contains. The API uses 0-based indexing;
frontends display 1-based values (e.g., "turn 7 of 7").

**Message-level fields:**

- `log_id` links each message back to its originating log entry for raw-log
  navigation
- `is_complex` indicates whether the message contains structured content
  (tool calls, function calls, or multimodal content)
- `complexity` provides a machine-readable reason when `is_complex` is true:
  `"tool_calls"`, `"function_call"`, or `"multimodal:{type}"`
- Complex messages receive a placeholder `content` such as
  `"[tool_calls — view raw Log #N]"` rather than blank content

**Orphan entries:** If a chat entry has no parent, the endpoint returns a valid
1-entry thread with `selected_entry_index = 0` and `total_entries = 1`.

The assembly algorithm is described in §14.5.

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
- request and response bodies use the shared JSON ellipsis transform for long
  string values before rendering
- for supported streamed responses, toggle `Stream view mode` between `Raw` and
  `Assembled`
- if the assembled result is partial, display a clear warning state

### 12.1 Conversation View

The TUI provides a conversation view for `/v1/chat/completions` log entries:

- press `c` in detail view to open conversation mode
- `c` is only available for chat completion entries that are not truncated
- the view renders messages with role-colored headers and `Log #N` attribution
- displays selected position in the title (1-based: "turn 7 of 7")
- press `esc` or `q` to return to detail view
- scroll with `j`/`k`

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

### 13.1 Conversation View

The detail overlay includes a `View Conversation` button for
`/v1/chat/completions` entries that are not truncated:

- clicking the button fetches `/api/logs/{id}/thread` and renders conversation
  blocks inline
- each message is styled by role (`user`, `assistant`, `system`, `developer`)
- `Log #N` links are clickable and navigate to the raw log detail for that
  entry
- a clear switch returns to the raw request/response panes
- body panes load preview content with the shared JSON ellipsis transform
- if a fetched body is not `complete`, the UI offers a `Load Full` action that
  refetches the canonical body
- the preview info line distinguishes byte truncation from display ellipsis

## 14. Chat Specialization

### 14.1 Scope

Chat specialization applies only to `/v1/chat/completions` requests. The
determination is made by checking whether the request path ends with
`/chat/completions`.

### 14.2 Sidecar Text

Two nullable sidecar columns hold derived searchable text:

- `req_text` — populated only for `/v1/chat/completions` where extracted text
  differs from `req_body`. `NULL` otherwise.
- `resp_text` — populated only when extracted text differs from `resp_body`
  (streamed or non-streamed chat responses with extractable text). `NULL`
  otherwise.

FTS indexes use `COALESCE(req_text, req_body)` and
`COALESCE(resp_text, resp_body)`, so all endpoints remain searchable without
duplicating raw bodies into the sidecar columns.

**Critical guardrail:** Sidecar columns use `*string` pointers in Go. When
sidecar text is not generated, these remain `nil`, never `""`. An empty string
would cause `COALESCE` to evaluate to `""`, silently erasing that entry from
the FTS index.

### 14.3 Canonical Message Hashing

For lineage tracking, the system hashes a canonicalized form of the request
messages using SHA-256.

Each message is canonicalized as:

```go
type CanonicalMessage struct {
    Role        string `json:"role"`
    Content     string `json:"content,omitempty"`
    ComplexHash string `json:"complex_hash,omitempty"`
}
```

**Canonicalization rules:**

1. Always include `role`.
2. If `content` is a plain JSON string, store the text directly.
3. If `content` is a text-only array, flatten the text parts.
4. If the message is structurally complex (has `tool_calls`, `function_call`,
   or multimodal content array), compute `ComplexHash` as described below.
5. Request-level fields (`model`, `temperature`, `tools`, etc.) are ignored.

**ComplexHash:** When a message contains structured content that cannot be
represented as plain text, collision resistance is preserved by computing a
SHA-256 hash of the raw bytes:

1. Collect the raw `json.RawMessage` fields that triggered the complex
   classification (`tool_calls`, `function_call`, or content array).
2. Apply `json.Compact` to normalize whitespace.
3. Concatenate the compacted bytes in deterministic order.
4. Compute SHA-256, store as lowercase hex in `ComplexHash`.

**Multimodal content detection:** Content fields that are JSON arrays (starting
with `[`) are treated as multimodal and receive ComplexHash treatment. This
covers `image_url`, `audio`, and any future content part types without
requiring per-type parsing.

**Hash functions:**

- `HashMessages(msgs)` — SHA-256 of the full canonicalized message array
- `HashPrefix(msgs, n)` — SHA-256 of the first `n` canonicalized messages

### 14.4 Deterministic Lineage

For `/v1/chat/completions`, lineage is tracked with four columns:

- `chat_hash` — SHA-256 hex of the canonicalized full message array
- `parent_id` — foreign key to the parent log entry
- `parent_prefix_len` — number of request messages that matched the parent
- `message_count` — total number of messages in this request

**Parent lookup:** On insertion, the writer attempts to find the parent entry
by hashing message prefixes, with a capped search of 5 attempts:

1. First try `N-2` messages (the common case: previous request without the new
   user message and echoed assistant response).
2. Then try remaining prefixes from `N-1` down to `1`, skipping `N-2`,
   stopping after 5 total attempts.
3. On first match, store `parent_id` and `parent_prefix_len`.
4. If no match after the capped attempts, leave both `NULL`.

The parent query uses:

```sql
SELECT id FROM openai_logs WHERE chat_hash = ? ORDER BY id DESC LIMIT 1
```

The "latest match wins" policy is a deliberate simplification for v5.

**Truncation guard:** If `req_truncated == true`, all chat enrichment is
skipped. The entry is treated as a standalone raw log with `NULL` values for
all chat-specific columns.

### 14.5 Thread Assembly — Top-Down Annotation with Backward Attribution

The thread endpoint (`GET /api/logs/{id}/thread`) assembles the conversation
view using a top-down annotation algorithm with backward attribution.

**Reader query:** A recursive CTE traverses the `parent_id` chain upward from
the selected entry to the root, then orders chronologically:

```sql
WITH RECURSIVE thread AS (
    SELECT * FROM openai_logs WHERE id = ?
    UNION ALL
    SELECT o.* FROM openai_logs o
    INNER JOIN thread t ON t.parent_id = o.id
)
SELECT * FROM thread ORDER BY id ASC;
```

This yields an ordered chain `[E0, E1, ..., En]` where `E0` is the root and
`En` is the selected entry.

**Algorithm:**

1. Parse the **selected (final) entry's** `req_body.messages`. This is the
   canonical conversation history. Let `M = len(messages)`.
2. Attribute messages to log entries using backward attribution.
3. Append the selected entry's extracted assistant response as the last
   message, attributed to `En`.

**Backward attribution:**

```
cursor = M

for k = n down to 0:
    if k == 0:
        lower = 0
    else:
        lower = Ek.parent_prefix_len
    Ek owns messages [lower, cursor)
    cursor = lower
```

Each message in a range is annotated with the owning entry's `log_id`.

**Boundary clamping:** If any `parent_prefix_len` exceeds `cursor` or is
negative, it is clamped to `cursor`. This ensures the algorithm never panics
and degrades gracefully.

**Fallback for broken chains:** If the chain has only one entry (orphan root),
all messages are attributed to that entry. If `parent_prefix_len` is `NULL`,
it is treated as `0`.

**Benefits:**

- Eliminates double-echo of assistant messages.
- Gracefully handles client history rewriting.
- The rendered conversation is faithful to what the LLM actually received.
- Lineage metadata is used only for attribution, not for content.

### 14.6 Complex Message Handling

Messages are classified as complex when they contain `tool_calls`,
`function_call`, or multimodal content (array-typed `content`). Complex
messages are detected by `GetMessageComplexity()` which returns a boolean flag
and a machine-readable reason string.

In conversation view:

- Complex messages are never hidden.
- They receive a placeholder `content` such as
  `"[tool_calls — view raw Log #N]"`.
- The `is_complex` and `complexity` fields on `ThreadMessage` allow frontends
  to render appropriate indicators.

For complex assistant responses that cannot be parsed as plain text, the thread
endpoint attempts to extract text via `resp_text` or direct JSON parsing,
falling back to a placeholder with the complexity reason.

### 14.7 Sidecar Text Generation

Before insert, the writer populates sidecar fields for chat endpoints:

**Request text (`req_text`):**

If parsing succeeds, extract searchable text from all messages. For text-only
content, include the text directly. For complex content, insert a marker token
such as `[complex-content]`. If parsing fails, leave `req_text` as `nil`
(falls back to raw body via FTS `COALESCE`).

**Response text (`resp_text`):**

For chat responses, use the SSE extractor (for streamed responses) or direct
JSON parsing (for non-streamed responses) to extract assistant text. If
extraction fails or the content is complex, leave `resp_text` as `nil`.

### 14.8 Known Limitations

**Identical prefix collision:** Two conversations starting with the exact same
messages share a `chat_hash`. The `ORDER BY id DESC LIMIT 1` parent lookup
links to the most recent match, which may cross conversation boundaries. The
rendered conversation text is always correct because it uses top-down
annotation; only the attributed `Log #N` links on early messages may point to
the wrong raw log.

**Async queue ordering:** The writer uses a bounded channel consumed by a
single goroutine. Entries enqueue in completion order, not request order. If
Turn 2 finishes before Turn 1, it writes first and will not find Turn 1 as its
parent. The conversation UI still renders correctly from the request body; only
the lineage linkage is incomplete.

**Client history rewriting:** If a client summarizes or modifies past messages
between turns, the prefix hash will not match. `parent_id` will be `NULL` and
the chain breaks. The conversation view renders the current request's messages
correctly as a standalone conversation.

**Client-echoed history:** The conversation view renders historical assistant
messages exactly as the client echoed them back in the final request. If the
client truncated or modified a previous assistant response, the conversation
view shows the modified version. Users can click `Log #N` links to view the
original proxy-captured response.

## 15. Emacs Client Behavior

The Emacs client provides two modes for interacting with logs:

### 15.1 Log Detail View

The show mode (`memoryelaine-show`) displays log entry details including
headers, bodies, stream-view support, and entry navigation.

- `g` refreshes the current entry
- `v` toggles raw and assembled response view when assembled data is available
- `t` fetches canonical full request/response bodies on demand
- `M-n` / `M-p` jump between section headings
- `C-M-n` / `C-M-p` jump to the next or previous search result entry
- `w h/b/H/B` copies request headers, request body, response headers, or
  response body; the body copy commands auto-fetch canonical full bodies before
  copying
- `j` and `J` open the request or response body in a dedicated JSON inspector

The JSON inspector requires Emacs 29.1+, JSON tree-sitter support, and
`treesit-fold`. It opens valid JSON in a foldable `json-ts-mode` buffer and
rejects invalid JSON with a user-facing error.

### 15.2 Conversation View

For `/v1/chat/completions` entries, press `c` in show mode to open a
conversation view (`memoryelaine-thread-mode`):

- displays the conversation header with turn count (1-based)
- renders messages with role-specific faces:
  - `user`: bold blue
  - `assistant`: bold green
  - `system` / `developer`: italic gray
- `Log #N` appears as a clickable button that opens the raw log detail
- press `q` to return to the previous view
- `c` is only available for non-truncated chat completion entries

## 16. Observability and Security

### 16.1 Application Logging

The service writes structured logs to stdout using Go's `log/slog`.

The configured log level controls application log verbosity.

Chat-specific debug logging covers:

- request/response parse failures
- parent lookup success/failure
- prefix search cap reached (all 5 attempts exhausted)
- truncation guard activation

### 16.2 Prometheus

The management port exposes a Prometheus-compatible `/metrics` endpoint.

The exact metric set may include standard Go/process collectors and
implementation-defined application metrics.

### 16.3 Security Posture

- request and response payloads are logged locally to SQLite
- management endpoints are protected by Basic Auth except `/health`
- sensitive headers are redacted from stored headers

## 17. Failure Semantics

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
- If chat enrichment fails (parse error, hash error, parent lookup error), the
  log entry is still inserted with `NULL` chat-specific columns. Capture
  reliability takes priority over thread perfection.
- If an individual message in a thread cannot be parsed, the endpoint prefers
  partial output over failing the entire request.

## 18. Acceptance Criteria

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
13. Searching for words from a streamed `/v1/chat/completions` assistant
    response succeeds via FTS.
14. Searching non-chat endpoints still works via `COALESCE` fallback with no
    storage duplication.
15. FTS remains correct after a full index rebuild (non-chat entries are not
    silently dropped).
16. Conversation view for a selected log shows the root-to-selected chain in
    order, with no double-echo of assistant messages.
17. `system` and `developer` messages are visible in conversation view.
18. Complex messages are not silently dropped; they are represented with
    placeholders and raw-log links.
19. Different tool-call messages produce different canonical hashes (no
    `IsComplex`-only collision).
20. Truncated entries are handled gracefully (no parse attempts, no conversation
    button, raw fallback for FTS).
21. Thread endpoint returns `400` for non-chat and truncated entries, and a
    valid 1-entry thread for orphan roots.
22. Sidecar columns are `NULL` or non-empty, never empty strings.
23. TUI, Web UI, and Emacs all show long JSON string values with the shared
    ellipsis transform while preserving canonical full bodies on demand.
24. Emacs can open request and response bodies in a dedicated foldable JSON
    inspector, and body copy commands fetch canonical full content before
    copying.
