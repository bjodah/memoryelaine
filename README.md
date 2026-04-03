# memoryelaine

memoryelaine is a single-binary Go middleware proxy for OpenAI-compatible inference APIs. It sits transparently between clients and one fixed upstream provider. Its primary purpose is to proxy requests with no intentional buffering of active streams while asynchronously logging selected request/response pairs, timings, and HTTP metadata to a local SQLite database.

## Quick Start

```bash
# Copy the example config
cp example-config.yaml config.yaml

# Build and test
CGO_ENABLED=1 GOOS=linux go build -tags sqlite_fts5 -o memoryelaine .

# Run the proxy
./memoryelaine serve --config ./config.yaml
```

Once the proxy is running:
- Send client traffic to `proxy.listen_addr`
- Browse the management UI on `management.listen_addr`
- Inspect logs with `memoryelaine log` or `memoryelaine tui`

## Core Commands

### `serve`
Start both HTTP servers:
- **Proxy listener**: forwards client traffic to the configured upstream. Requests matching `proxy.log_paths` are captured.
- **Management listener**: Web UI, JSON API, Prometheus metrics, health.

The management surface also exposes a runtime recording switch. When recording is paused, matching proxy paths are still forwarded normally, but no new request/response bodies are captured to SQLite.

### `log`
Query stored logs from the command line.

**Usage:**
```bash
  -f, --format json|jsonl|table   Output format (default: json)
  -n, --limit INT                 Number of records to return (default: 20)
      --offset INT                Pagination offset (default: 0)
      --status INT                Exact HTTP status filter, e.g., 200 or 500
      --path STRING               Exact request path filter
      --since VALUE               RFC3339 timestamp or relative duration (e.g., 30m, 2h, 7d)
      --until VALUE               RFC3339 timestamp or relative duration
  -q, --query STRING              Substring search across req_body and resp_body
      --id INT                    Return a single log record by primary key
```

**Examples:**
```bash
memoryelaine log -f table -n 10
memoryelaine log --status 500 --since 24h
memoryelaine log --path /v1/chat/completions -q tool_call
memoryelaine log --id 42
```

### `tui`
Open the interactive terminal UI for browsing logs.

**Keybindings:**
- `j`/`k` or arrows: Navigate the table or scroll the detail view
- `enter`: Open detail view for the selected row
- `esc` or `q`: Leave detail view
- `v`: Toggle stream view mode (Raw / Assembled)
- `r`: Refresh current page
- `n` / `p`: Next / previous page
- `f`: Cycle exact status filters (none → 200 → 400 → 500)
- `q` / `ctrl+c`: Quit

### `prune`
Delete records older than N days.

**Examples:**
```bash
memoryelaine prune --keep-days 7 --dry-run
memoryelaine prune --keep-days 30 --vacuum
```

## Configuration

Example config file (`config.yaml`):
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

Lookup order: `--config <path>` → `./config.yaml` → `$HOME/.config/memoryelaine/config.yaml` → Built-in defaults.

### Configuration Fields

#### `proxy.listen_addr`
Address for the proxy listener. Default: `0.0.0.0:8688`

#### `proxy.upstream_base_url`
Base URL for the single upstream provider. Must be a valid `http://` or `https://` URL. Default: `https://api.openai.com`

#### `proxy.timeout_minutes`
Connection setup / response-header timeout budget. This does not terminate an already-active response stream. Default: `23`

#### `proxy.log_paths`
Exact path allowlist for payload capture. Requests on other paths are still proxied, just not written to SQLite. Default:
- `/v1/chat/completions`
- `/v1/completions`

#### `management.listen_addr`
Address for the management server. Must differ from proxy.listen_addr. Default: `0.0.0.0:8677`

#### `management.preview_bytes`
Maximum bytes returned in body preview responses via `/api/logs/{id}/body`. Default: `65536` (64 KiB)

#### `management.auth.username`
Basic Auth username for `/`, `/api/logs`, `/api/logs/{id}`, `/api/logs/{id}/body`, and `/metrics`. Default: `admin`

#### `management.auth.password`
Basic Auth password for the management endpoints above. Default: `changeme`

#### `database.path`
Path to the SQLite database file. Default: `./memoryelaine.db`

#### `logging.max_capture_bytes`
Maximum number of request or response body bytes retained in memory and persisted in the database per direction. Bodies larger than this are truncated in the log entry while still being fully streamed to the client. Must be greater than zero. Default: `8388608` (8 MiB)

#### `logging.level`
Structured log verbosity for the service process. Accepted values: `debug`, `info`, `warn`, `error`. Default: `info`

## Management API

### Endpoints

- `GET /` - Embedded Web UI (Basic Auth protected)
- `GET /api/logs` - Log summaries (no bodies/headers). Supports `query`, `limit`, `offset` parameters. (Basic Auth protected)
- `GET /api/logs/{id}` - Log detail metadata with decoded headers and stream-view availability. No bodies. (Basic Auth protected)
- `GET /api/logs/{id}/body` - Request or response body content. Params: `part` (req|resp, default: resp), `mode` (raw|assembled, default: raw), `full` (true|false, default: false). (Basic Auth protected)
- `GET /api/recording` - Current runtime recording state (Basic Auth protected)
- `PUT /api/recording` - Change runtime recording state with `{"recording":true|false}` (Basic Auth protected)
- `GET /last-request` - Latest captured request body (Basic Auth protected)
- `GET /last-response` - Latest captured response body (Basic Auth protected)
- `GET /metrics` - Prometheus metrics (Basic Auth protected)
- `GET /health` - Public health JSON, no auth required. Includes the current `recording` state.

### Query Parameters

`GET /api/logs` accepts a `query` parameter with a DSL string (see below), plus `limit` (integer, max 1000) and `offset` (integer). When `query` is absent, legacy parameters are accepted as fallback: `status`, `path`, `q`, `since`, `until`.

### Query DSL

The `query` parameter accepts a search string combining free-text and structured filters:

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

**Example:** `status:2xx method:POST path:/chat hello world`

### Configuration

- `management.preview_bytes`: max bytes returned in body preview (default: 65536)

When one or more loggable requests have been proxied while recording is paused, `/last-request` and `/last-response` keep serving the last captured bodies but label them as stale until a newly captured body replaces them.

## Stream View Mode

When viewing a streamed response in the detail view, the TUI and Web UI offer a **Stream View** toggle:

- **Raw**: the exact stored response body, including SSE framing
- **Assembled**: reconstructed assistant text derived from the SSE stream

Assembled mode is currently supported for:
- `/v1/chat/completions`
- `/v1/completions`

Assembled mode is unavailable for truncated, non-streamed, or unsupported responses. When parsing only partially succeeds, the recovered text is shown with a warning indicator.

## Emacs client

See [./emacs-memoryelaine/README.md](./emacs-memoryelaine/README.md).

## Development

### Helper Scripts

```bash
./scripts/build-and-test.sh
./scripts/run-lint-checks.sh
```

### Trouble shooting

If you see an error reading: `migrating database: executing FTS schema: creating FTS table: no such module: fts5`
Try building with fts5 enabled:
```console
CGO_ENABLED=1 GOOS=linux go build -tags sqlite_fts5 ...
CGO_ENABLED=1 GOOS=linux go run -tags sqlite_fts5 . serve --config ./example-config.yaml
GOFLAGS="-tags=sqlite_fts5" go mod tidy
```


### Repository Layout

Specifications, implementation plans etc. are found under `design-docs-wip/`, `design-docs-main/`, and `design-docs-legacy/`. The `-wip` folder is "work in progress" (should typically be empty when we are on `main` branch), the `-main` folder should typically describe the state of the main-branch / "what's released", the `-legacy` folder is typically of little interest and is to be considered a historical legacy and typically contains documents that are out-of-date.
