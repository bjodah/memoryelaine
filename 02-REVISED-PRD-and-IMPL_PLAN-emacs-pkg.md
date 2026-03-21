# Revised PRD and Implementation Plans

## Product: `memoryelaine.el` + supporting changes in `memoryelaine`

## 1. Background and rationale

The original plan treated `memoryelaine.el` as a thin Emacs client over the existing `memoryelaine` management API. That approach surfaced several problems:

* the existing list API returns full log entries, including large request and response bodies, which makes list refreshes too expensive for an Emacs search buffer
* the original `t` design in the show buffer only deferred rendering, not transfer cost
* the original query language lived in Emacs, which would have duplicated search semantics and made other clients harder to keep consistent
* the original document borrowed heavily from Elfeed's interaction model, but Elfeed's live filter assumes local data and local redraws, while `memoryelaine.el` will query a remote API

This revised plan widens the scope deliberately. The management API of `memoryelaine` is now a first-class design surface for enabling a clean Emacs client.

The guiding idea is:

* keep storage canonical and simple on the Go side
* move query parsing and heavy lifting into `memoryelaine`
* keep `memoryelaine.el` lightweight, keyboard-driven, and predictable
* take inspiration from Elfeed's interaction model and buffer layout, but do not imitate its local-data assumptions

## 2. Product vision

`memoryelaine.el` is a lightweight, keyboard-driven Emacs browser for `memoryelaine` logs.

It should feel familiar to users of Elfeed:

* one search/list buffer
* one reusable detail buffer
* strong keyboard navigation
* efficient browsing of many entries
* plain-text inspection of request and response payloads

It is primarily intended for localhost and personal use.

## 3. Design principles

1. **Server-authoritative query semantics**
   The query language must be implemented by `memoryelaine`, not reimplemented in Elisp.

2. **Summary/detail split**
   The list endpoint must return summaries only. Large bodies must never be transferred just to render a table.

3. **True explicit opt-in for large payloads**
   Pressing `t` in Emacs must trigger additional body fetching, not merely re-render already downloaded content.

4. **Reusable detail buffer**
   The Emacs client must reuse a single `*memoryelaine-entry*` buffer to avoid clutter.

5. **Raw storage remains canonical**
   SQLite continues storing raw captured request and response bodies only. Stream-assembled views remain derived.

6. **Plain text in v1**
   No markdown or HTML rendering in v1. The show buffer renders plain text only.

7. **Optional live search**
   Live minibuffer-driven search is optional, off by default, and must be safe under latency and out-of-order responses.

8. **Predictable failure behavior**
   Large payloads, auth failures, parse failures, and stale async responses must degrade clearly and safely.

## 4. Goals

### 4.1 `memoryelaine`

* provide an API shape that is cheap enough for interactive clients
* provide a server-side query language designed for easy implementation in Go
* support preview vs full body fetching
* make stream-view metadata useful to external clients, including Emacs
* preserve current storage model and logging behavior where possible

### 4.2 `memoryelaine.el`

* provide an Elfeed-inspired search/detail workflow
* browse logs without freezing Emacs on large bodies
* use asynchronous `curl` for network requests
* make auth and connection setup easy for localhost usage
* present request/response bodies and assembled stream view cleanly in plain text

## 5. Non-goals

* markdown rendering in v1
* support for `/last-request` and `/last-response` in `memoryelaine.el`
* full analytics dashboards in Emacs
* offline mirroring of the `memoryelaine` database into Emacs
* complex boolean query grammar with parentheses and arbitrary precedence in v1
* multi-user collaboration features
* replacing the existing CLI and TUI with the Emacs client

## 6. Revised management API requirements

### 6.1 API split

The API must be split into:

* **summary list API** for table browsing
* **detail metadata API** for one log entry
* **body retrieval API** for fetching request or response bodies in preview or full form

This may be a breaking change.

### 6.2 Summary list endpoint

`GET /api/logs`

Returns paginated summaries only.

Each summary row must include:

* `id`
* `ts_start`
* `ts_end`
* `duration_ms`
* `client_ip`
* `request_method`
* `request_path`
* `status_code`
* `req_bytes`
* `resp_bytes`
* `req_truncated`
* `resp_truncated`
* `error`
* optionally lightweight booleans such as `has_request_body`, `has_response_body`

It must not include:

* `req_body`
* `resp_body`
* `req_headers_json`
* `resp_headers_json`
* assembled stream text

The response should include:

* `data`
* `total`
* `limit`
* `offset`
* `has_more`
* optionally `query_echo` and/or a normalized parsed-query representation for debugging

### 6.3 Detail metadata endpoint

`GET /api/logs/{id}`

Returns metadata for one entry without full bodies by default.

It should include:

* the summary fields
* `upstream_url`
* decoded request headers
* decoded response headers
* stream-view capability metadata
* preview availability flags
* preview truncation flags

The detail endpoint should not force clients to parse JSON strings embedded inside JSON. Headers should be returned as decoded JSON objects or arrays.

### 6.4 Body retrieval endpoint

Recommended shape:

* `GET /api/logs/{id}/body?part=req&mode=raw&full=false`
* `GET /api/logs/{id}/body?part=resp&mode=raw&full=false`
* `GET /api/logs/{id}/body?part=resp&mode=assembled&full=false`
* same with `full=true`

Response shape:

* `part`
* `mode`
* `full`
* `content`
* `included_bytes`
* `total_bytes`
* `truncated`
* `available`
* `reason`

For request bodies, `assembled` is invalid.

For response bodies, `assembled` follows the current stream-view rules.

### 6.5 Preview policy

The server must support preview-sized retrieval without loading entire large bodies into normal list responses.

Recommended config:

* `management.preview_bytes` with a sensible default such as 65536

Preview bodies should allow the show buffer to open quickly while preserving a meaningful `t` command for fetching the full body.

### 6.6 Query language

The management API must accept a single query-language string, parsed in Go.

Recommended request shape:

`GET /api/logs?query=<dsl>&limit=<n>&offset=<n>`

#### v1 query language requirements

Space-separated terms joined by implicit AND.

Support quoted phrases.

Supported terms:

* bare word or quoted phrase → substring search in request and response bodies
* `text:<value>` → explicit substring search
* `status:200`
* `status:4xx`
* `status:5xx`
* `method:POST`
* `path:/v1/chat/completions`
* `since:24h`
* `since:2026-03-01T10:00:00Z`
* `until:24h`
* `until:2026-03-01T10:00:00Z`
* `is:req-truncated`
* `is:resp-truncated`
* `is:error`
* `has:req`
* `has:resp`

Optional but recommended in v1:

* unary negation with a leading `-`, for example `-status:200` or `-"health check"`

Not required in v1:

* OR
* parentheses
* regex
* nested field syntax

### 6.7 Query parsing behavior

* invalid query terms must return `400 Bad Request` with a useful machine-readable error response
* relative times should reuse the same duration parsing semantics as the CLI where possible
* the server should normalize parsed queries internally so future clients can behave consistently

### 6.8 Stream-view metadata

The current stream-view rules stay intact, but the API must expose them in a client-friendly shape.

For response bodies, the detail API and body API should make available:

* whether assembled mode is available
* why it is unavailable when not available
* whether assembled mode is partial
* preview vs full assembled content when requested

### 6.9 Backward compatibility

Breaking changes are acceptable.

If compatibility is desired during transition, add a short-lived compatibility mode or versioned endpoints. However, the preferred outcome is one clean API rather than permanent dual maintenance.

## 7. Revised `memoryelaine.el` product requirements

### 7.1 Entry point

Interactive command:

* `M-x memoryelaine`

Opens the search buffer and loads the first page.

### 7.2 Search buffer

Buffer name:

* `*memoryelaine-search*`

Purpose:

* browse summary rows
* apply server-side query language
* paginate results
* inspect recording state
* open a selected entry in the reusable detail buffer

Columns:

* ID
* Time
* Method
* Path
* Status
* Duration
* Req Size
* Resp Size
* optional flags/error column if width permits

Header line must show:

* current query string
* page position or offset/total
* recording state
* loading/error state when relevant

Keybindings:

* `RET` open entry at point
* `g` refresh current page
* `s` edit query in minibuffer and submit on `RET`
* `S` live-edit query when optional live mode is enabled
* `n`/`p` move rows
* `N`/`P` next/previous page
* `R` toggle recording state
* `q` quit window or bury buffer

### 7.3 Detail buffer

Buffer name:

* `*memoryelaine-entry*`

The buffer must be reused for every opened entry.

Purpose:

* inspect metadata
* inspect headers
* inspect request and response body previews
* toggle raw vs assembled response mode
* fetch full bodies on demand

Display:

* metadata section
* request headers
* response headers
* request body section
* response body section

Response body modes:

* `raw`
* `assembled` when available

Keybindings:

* `q` return to search buffer/window
* `g` refresh current entry metadata and preview bodies
* `v` toggle raw/assembled response mode
* `t` fetch full content for the currently relevant body section or both bodies depending on implementation choice
* `TAB` or section navigation keys are optional, not required in v1

### 7.4 Authentication and connection setup

Default behavior:

* prefer credentials from `auth-source`
* allow explicit override via defcustoms for localhost or ad hoc usage

The package must not require users to keep passwords in plain Elisp variables, but it may allow that as an override.

### 7.5 Networking model

* use asynchronous `curl`
* guard against stale/out-of-order responses with generation IDs or equivalent request tokens
* cancel or ignore superseded requests when a newer query is issued
* surface HTTP and parse failures in a dedicated log/error buffer and in concise minibuffer messages

### 7.6 Rendering rules

* plain text only in v1
* no markdown rendering
* no syntax highlighting requirement in v1, though optional JSON prettification is welcome
* body previews must clearly say when they are preview-only and how to fetch the full body
* header rendering should be readable even when the server returns many headers

### 7.7 Live query editing

Live query editing is optional and off by default.

Requirements if enabled:

* debounced
* only one active logical query at a time
* stale response suppression is mandatory
* visual indication that the buffer is updating

### 7.8 Logging and recovery

The package should provide a `*memoryelaine-log*` or equivalent error buffer for:

* curl failures
* HTTP auth failures
* JSON parse failures
* query parse failures returned by the server
* unexpected API shapes

## 8. Acceptance criteria

### 8.1 Server acceptance criteria

1. List responses never include full bodies or header blobs.
2. The detail metadata endpoint does not require clients to decode JSON strings inside JSON.
3. Full body transfer only occurs when explicitly requested.
4. Query parsing is server-side and consistent across clients.
5. Invalid query strings return structured `400` errors.
6. Preview body retrieval and full body retrieval are both supported.
7. Assembled response mode is available through the API under the same conditions already used by the TUI/Web UI.
8. Existing raw-storage and truncation semantics remain intact.

### 8.2 Emacs client acceptance criteria

1. Opening the search buffer does not fetch any request or response bodies.
2. Opening the detail buffer fetches previews but not full bodies by default.
3. Pressing `t` causes an additional fetch and then shows the full body.
4. Out-of-order network responses never repaint newer search results with stale data.
5. The detail buffer is reused rather than spawning one buffer per entry.
6. Users can browse, paginate, inspect, and toggle recording state without blocking Emacs.
7. Raw and assembled response views are both plain text in v1.
8. Error conditions are visible and recoverable.

## 9. Risks and mitigations

### Risk: server-side query DSL grows too fast

Mitigation: keep v1 grammar deliberately small and AND-only.

### Risk: detail API still becomes too heavy

Mitigation: split metadata from body retrieval, and add preview/full controls.

### Risk: Emacs async race conditions

Mitigation: generation counters, request cancellation where possible, stale-response ignore logic.

### Risk: users assume Elfeed feature parity

Mitigation: document clearly that the package is Elfeed-inspired, not Elfeed-compatible.

### Risk: credentials handling becomes sloppy

Mitigation: default to `auth-source`, support override variables only as a convenience.

## 10. Implementation plan A: changes in `memoryelaine`

### 10.1 Objectives

* reshape the management API for interactive clients
* add a Go-native query parser
* add preview/full body retrieval
* preserve existing storage semantics and stream-view logic

### 10.2 Workstreams

#### Workstream A: API redesign

1. Replace the current `/api/logs` response type with a summary DTO.
2. Add a detail metadata DTO for `/api/logs/{id}`.
3. Add a body retrieval endpoint for request/response preview/full retrieval.
4. Return decoded headers in API responses.
5. Standardize JSON error responses for query parse and request validation failures.

#### Workstream B: query language implementation

1. Define a tokenizer supporting quotes and escaped quotes.
2. Define a simple parser producing an internal query AST or normalized filter struct.
3. Map the normalized query to SQL `WHERE` fragments.
4. Reuse CLI-compatible time parsing for `since:` and `until:`.
5. Add tests for successful parsing and failure cases.

#### Workstream C: database/query layer changes

1. Add summary query methods that do not select body and header columns.
2. Add preview query methods using SQL substring operations or equivalent.
3. Keep full-detail retrieval paths for explicit body fetches only.
4. Consider adding indexes if query usage expands materially.

#### Workstream D: stream-view API exposure

1. Keep `streamview.Build` as the canonical derived-view implementation.
2. Expose assembled availability and reason in the detail metadata response.
3. Support preview/full retrieval of assembled response text from the new body endpoint.
4. Preserve the existing unsupported/truncated/partial semantics.

#### Workstream E: tests and migration

1. Update server tests for new endpoint shapes.
2. Add query parser tests.
3. Add body preview/full tests.
4. Add compatibility or migration notes in README/spec docs.
5. Update Web UI and TUI to consume the new endpoints where worthwhile.

### 10.3 Recommended package/file changes

* `internal/management/api.go`

  * replace current list response with summary response
  * add body endpoint
  * add structured error responses

* `internal/database/models.go`

  * add summary DTOs and query plan structs

* `internal/database/reader.go`

  * add summary query method
  * add preview retrieval helpers
  * keep full entry retrieval for explicit detail fetches

* `internal/streamview/*`

  * expose helpers for preview/full assembled body generation if needed

* `cmd/log.go`

  * optionally migrate the CLI to use the same query parser over time

* `design-docs-main/*`

  * update product spec and management API spec

### 10.4 Suggested order of execution

1. Define new DTOs and API contracts.
2. Implement summary query path.
3. Implement query parser and SQL translation.
4. Implement detail metadata response.
5. Implement body preview/full endpoint.
6. Adapt Web UI/TUI if needed.
7. Update docs and tests.

### 10.5 Deliverables

* revised management API
* server-side query language
* preview/full body retrieval support
* updated documentation and tests

## 11. Implementation plan B: green-field project `memoryelaine.el`

### 11.1 Objectives

* build an Elfeed-inspired Emacs interface over the revised API
* keep the package lightweight, async, and safe around large payloads
* reuse one detail buffer

### 11.2 Proposed file layout

* `memoryelaine.el`

  * entry point, defgroup, user-facing commands

* `memoryelaine-auth.el`

  * `auth-source` lookup and override resolution

* `memoryelaine-http.el`

  * async `curl` wrapper
  * request tokens / generation IDs
  * structured error handling

* `memoryelaine-query.el`

  * local query helpers only where necessary
  * no semantic transpiler; the server owns the query language

* `memoryelaine-state.el`

  * search state, detail state, in-flight request bookkeeping

* `memoryelaine-search.el`

  * search-mode, table rendering, pagination, query prompt, recording toggle

* `memoryelaine-show.el`

  * show-mode, entry rendering, raw/assembled toggle, on-demand full fetch

* `memoryelaine-log.el`

  * internal error/debug log buffer

### 11.3 State model

#### Search state

* current query string
* limit
* offset
* total
* current page summaries
* loading flag
* recording state
* current request generation/token

#### Detail state

* current entry id
* detail metadata
* current response view mode (`raw`/`assembled`)
* preview/full state for request body
* preview/full state for response body
* loading flags for metadata and body fetches
* current request generation/token

### 11.4 Networking behavior

1. All network I/O goes through one async `curl` abstraction.
2. Each logical request gets a generation/token.
3. Callbacks verify that their token is still current before repainting buffers.
4. Superseded search requests are ignored or terminated.
5. HTTP status, stderr, and invalid JSON are surfaced through `memoryelaine-log`.

### 11.5 User experience flow

#### Initial launch

1. `M-x memoryelaine`
2. open `*memoryelaine-search*`
3. fetch `/api/logs?query=<default>&limit=<n>&offset=0`
4. render summary table and header line

#### Open entry

1. user presses `RET`
2. fetch `/api/logs/{id}` for metadata
3. fetch preview bodies as needed
4. render reusable `*memoryelaine-entry*`

#### Toggle response view

1. user presses `v`
2. if assembled preview already loaded, re-render immediately
3. otherwise fetch assembled preview or full assembled body depending on current full/preview state

#### Load full payload

1. user presses `t`
2. fetch full request and/or response content
3. replace preview markers with full content
4. keep the buffer in place; do not spawn a new buffer

### 11.6 Configuration

Recommended defcustoms:

* `memoryelaine-base-url`
* `memoryelaine-default-query`
* `memoryelaine-page-size`
* `memoryelaine-live-search-enabled`
* `memoryelaine-live-search-debounce`
* `memoryelaine-curl-program`
* `memoryelaine-auth-source-host`
* `memoryelaine-username`
* `memoryelaine-password`
* `memoryelaine-show-entry-function` if you want future flexibility similar to Elfeed

Preferred auth behavior:

* first try `auth-source`
* then explicit variables if provided
* otherwise prompt interactively and optionally cache for session use

### 11.7 Key implementation details

#### Search buffer

* implement with `tabulated-list-mode` or a custom derived mode
* keep rendering logic separate from network callbacks
* header line should reflect loading state and query text
* preserve point where reasonable across refreshes

#### Detail buffer

* derive a dedicated major mode
* keep body insertion helpers separate from metadata rendering
* render preview/full markers clearly
* render plain text only

#### Logging

* provide `memoryelaine-log` command
* append timestamped internal errors and debug messages
* avoid burying operational failures in transient minibuffer messages only

### 11.8 Testing and validation

#### Emacs package tests

* query submission updates list state correctly
* stale responses do not overwrite newer data
* detail buffer reuse works
* `t` triggers network fetch only when full bodies are not yet loaded
* `v` respects assembled availability
* auth-source lookup works or fails clearly

#### Manual QA

* localhost usage with default Basic Auth
* large response previews
* full body retrieval for multi-megabyte payloads
* invalid query error from server
* server paused recording state toggle
* slow network or artificial request delay to verify stale-response suppression

### 11.9 Suggested order of execution

1. implement auth and HTTP foundation
2. implement search state and search buffer with explicit submit
3. implement detail metadata rendering in reusable buffer
4. implement preview body retrieval
5. implement full body retrieval via `t`
6. implement raw/assembled toggle
7. add optional live search
8. add logging polish and tests

### 11.10 Deliverables

* installable `memoryelaine.el` package
* search buffer
* reusable show buffer
* auth-source integration
* async curl networking with stale-response protection
* documentation and screenshots/examples

## 12. Final recommendation

Treat this as one coordinated product effort, not an Emacs package bolted onto an unsuitable API.

The highest-value change is the server-side summary/detail/body split. If that is done first, the Emacs client becomes straightforward and honest. If it is not done first, the client will spend the rest of its life compensating for an API that is too heavy for interactive editor use.

## 13. Suggestions to take under consideration & tips

13.1.1. Regarding The Body Retrieval API (/api/logs/{id}/body)
The proposed schema (?part=req&mode=raw&full=false) is highly ergonomic.

    Suggestion: Ensure the response includes an HTTP header or JSON field like Total-Bytes-Available so the Emacs client can display exactly how much data is missing from the preview (e.g., [Preview: 64KB / 8.2MB shown. Press 't' to load full body]).

13.1.2. Server-Side Query DSL
Implementing the DSL (e.g., status:5xx since:24h) in Go is the correct choice. It standardizes the interface for the Web UI, TUI, and Emacs.

    Suggestion: When parsing the query into a SQL WHERE clause, ensure you sanitize inputs to prevent SQL injection, especially since the query string is user-generated. Using parameterized queries (?) for the parsed AST leaves will mitigate this.

13.2.1 Generation IDs / Request Tokens
Your identification of "stale out-of-order network responses" as a primary risk is spot on. Emacs' single-threaded nature combined with async processes makes this a common pitfall.
*   *Implementation Tip:* Use a simple counter variable (e.g., `memoryelaine--request-generation`). Increment it on every new search. Pass the *current* generation into the lexical closure of the curl callback. Inside the callback, check `(if (= captured-generation memoryelaine--request-generation) (render...))`.

13.2.2. The Reusable Detail Buffer (`*memoryelaine-entry*`)
Reusing a single buffer prevents the classic Emacs problem of accumulating dozens of dead buffers. 
*   *Implementation Tip:* When the user presses `q` in the detail buffer, use `(quit-window)` rather than `(kill-buffer)`. This elegantly hides the buffer, restores the previous window configuration (bringing them back to the search buffer exactly where they left off), but keeps the buffer alive for the next entry to overwrite.

13.2.3. Loading Full Payloads (`t` command)
*   *UX Suggestion:* When the user presses `t` and the async curl request returns the full payload, **do not redraw the entire buffer**. Re-rendering the headers and metadata resets the user's scroll position. Instead, use Emacs text properties or markers to locate the exact bounds of the preview text, delete it, insert the full text, and restore `(point)`. 

13.2.4. Authentication via `auth-source`
This is the most idiomatic Emacs approach. 
*   *Implementation Tip:* Map the `memoryelaine-base-url` (e.g., `localhost:8080`) to the `:machine` key in `auth-source`, and the literal string `"memoryelaine"` to the `:port` or `:user` key, so users can easily add it to their `~/.authinfo.gpg` file like this:
    `machine localhost:8080 port memoryelaine login admin password changeme`

13.2.5. Plain Text Rendering
Keeping it plain text for v1 is a wise scope reduction. However, JSON logs are notoriously hard to read without line breaks.
*   *Suggestion:* While you shouldn't apply heavy `json-mode` font-locking, it is highly recommended to run the raw JSON through a lightweight formatter *if* the content-type is `application/json`. Emacs 27+ has a built-in, fast C-level JSON formatter (`json-serialize` / `json-parse-string`). Formatting a 64KB preview payload takes milliseconds and vastly improves the UX.

13.3.1.  **Curl Sentinel:** Ensure your curl sentinel checks for exit codes. If curl exits with code `7` (Failed to connect to host), intercept it and print a friendly error to the echo area and `*memoryelaine-log*` rather than trying to parse empty JSON.
13.3.2.  **Debounce Implementation:** Use `run-with-idle-timer`. Whenever the user types in the minibuffer, use `cancel-timer` on the existing timer, then spawn a new one. This ensures the API is only hit when the user stops typing for `N` milliseconds.
13.3.3.  **Buffer Local Variables:** Store the "current entry ID" and "current stream mode (raw/assembled)" as buffer-local variables (`defvar-local`) inside the `*memoryelaine-entry*` buffer. This prevents global state pollution.
