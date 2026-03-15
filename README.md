# memoryelaine

memoryelaine is a single-binary Go proxy for OpenAI-compatible inference APIs.
It forwards traffic to one fixed upstream, captures selected request/response pairs,
and writes those logs asynchronously into SQLite so the proxy stays responsive.

## Core commands

  memoryelaine serve
      Start both HTTP servers:
        - proxy listener: forwards client traffic to the configured upstream
        - management listener: Web UI, JSON API, Prometheus metrics, health

  memoryelaine log [flags]
      Query stored logs from the command line.

  memoryelaine tui
      Open the interactive terminal UI for browsing logs.

  memoryelaine prune --keep-days N [--dry-run] [--vacuum]
      Delete records older than N days.

## Global flags
```
  --config PATH
      Path to a YAML config file.
      Lookup order when omitted:
        1. ./config.yaml
        2. $HOME/.config/memoryelaine/config.yaml
        3. built-in defaults
```

## command: `serve`

The serve command has no command-specific flags today; it is driven by config.
When running, it starts:

```
  Proxy port
    - Handles upstream proxying only.
    - Requests whose path exactly matches proxy.log_paths are captured and logged.
    - All other paths are proxied without database logging.

  Management port
    - GET /           : embedded Web UI (Basic Auth protected)
    - GET /api/logs   : JSON list API (Basic Auth protected)
    - GET /api/logs/{id}
                      : JSON detail API (Basic Auth protected)
    - GET /last-request
                      : latest captured request body (Basic Auth protected)
    - GET /last-response
                      : latest captured response body (Basic Auth protected)
    - GET /metrics    : Prometheus metrics (Basic Auth protected)
    - GET /health     : public health JSON, no auth required
```

Implementation notes worth knowing:
- request/response capture is capped by logging.max_capture_bytes
- over-limit bodies keep streaming to the client but logs are marked truncated
- Authorization, Cookie, and Set-Cookie are redacted before DB storage
- SQLite runs in WAL mode for concurrent serve/log/tui/prune access
- active streams flush immediately; the proxy does not buffer SSE responses
- `/last-request` and `/last-response` can be briefly out of sync during an
  in-flight exchange; that is expected

## command: `log`

```
  -f, --format json|jsonl|table   Output format (default: json)
  -n, --limit INT                 Number of records to return (default: 20)
      --offset INT                Pagination offset (default: 0)
      --status INT                Exact HTTP status filter, for example 200 or 500
      --path STRING               Exact request path filter
      --since VALUE               RFC3339 timestamp or relative duration
      --until VALUE               RFC3339 timestamp or relative duration
  -q, --query STRING              Substring search across req_body and resp_body
      --id INT                    Return a single log record by primary key
```

Relative time examples accepted by `--since`/`--until`: ` 30m   2h   7d   2.5h`

Examples:
```console
memoryelaine log -f table -n 10
memoryelaine log --status 500 --since 24h
memoryelaine log --path /v1/chat/completions -q tool_call
memoryelaine log --id 42
```

Table output columns: `ID, TIME, METHOD, PATH, STATUS, DURATION, REQ SIZE, RESP SIZE`

## command: `prune`
```
  --keep-days INT   Required. Delete rows older than this many days.
  --dry-run         Print how many rows would be deleted, but do not delete.
  --vacuum          Run SQLite VACUUM after deletion. Can be slow on large DBs.
```
Examples:
```console
memoryelaine prune --keep-days 7 --dry-run
memoryelaine prune --keep-days 30 --vacuum
```

## tui

The terminal UI opens against the configured SQLite database and supports:

```
  - j/k or arrow keys: move through the table
  - enter            : open detail view for the selected row
  - esc or q         : leave detail view
  - r                : refresh current page
  - n / p            : next / previous page
  - f                : cycle exact status filters: none -> 200 -> 400 -> 500 -> none
  - q / ctrl+c       : quit from the table view
```

## Config file schema

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

Accepted fields and what they do:

```
  proxy.listen_addr
      Address for the proxy listener.
      Default: 0.0.0.0:8000

  proxy.upstream_base_url
      Base URL for the single upstream provider.
      Must be a valid http:// or https:// URL.
      Default: https://api.openai.com

  proxy.timeout_minutes
      Connection setup / response-header timeout budget.
      This does not terminate an already-active response stream.
      Default: 23

  proxy.log_paths
      Exact path allowlist for payload capture.
      Requests on other paths are still proxied, just not written to SQLite.
      Must not be empty.
      Default:
        - /v1/chat/completions
        - /v1/completions

  management.listen_addr
      Address for the management server.
      Must differ from proxy.listen_addr.
      Default: 0.0.0.0:8080

  management.auth.username
      Basic Auth username for /, /api/logs, /api/logs/{id}, and /metrics.
      Default: admin

  management.auth.password
      Basic Auth password for the management endpoints above.
      Default: changeme

  database.path
      Path to the SQLite database file.
      Default: ./memoryelaine.db

  logging.max_capture_bytes
      Maximum number of request or response body bytes retained in memory and
      persisted in the database per direction.
      Bodies larger than this are truncated in the log entry while still being
      fully streamed to the client.
      Must be greater than zero.
      Default: 8388608 (8 MiB)

  logging.level
      Structured log verbosity for the service process.
      Accepted values: debug, info, warn, error.
      Default: info
```

## Management API query parameters

```
  GET /api/logs accepts:
    limit   integer, max 1000
    offset  integer
    status  exact status code
    path    exact request path
    since   unix timestamp in milliseconds
    until   unix timestamp in milliseconds
    q       substring search across request/response bodies

  GET /last-request
    returns the latest captured request body as plain text

  GET /last-response
    returns the latest captured response body as plain text
```

## Helper scripts

```
  ./scripts/build-and-test.sh
      Runs go test ./... and go build ./...

  ./scripts/run-lint-checks.sh
      Verifies formatting with gofmt and runs golangci-lint using the installed
      binary at $(go env GOPATH)/bin/golangci-lint
```

## Typical local workflow

```
  cp example-config.yaml config.yaml
  ./scripts/run-lint-checks.sh
  ./scripts/build-and-test.sh
  go run . serve --config ./config.yaml
```

Once the proxy is running:
- send client traffic to proxy.listen_addr
- browse the management UI on management.listen_addr
- inspect logs with `memoryelaine log` or `memoryelaine tui`


## Repository layout

Specifications, implementation plans etc. are found under `design-docs-wip/`, `design-docs-main/`,
and `design-docs-legacy/`. The `-wip` folder is "work in progress" (should typically be empty when
we are on `main` branch), the `-main` folder should typically describe the state of the main-branch
/ "what's released", the `-legacy` folder is typically of little interest and is to be considered a
historical legacy and typically contains documents that are out-of-date.
