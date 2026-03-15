# Implementation Plan: Stream view mode for streamed responses

## Purpose

This document describes how to implement the `Stream view mode` feature
introduced in
`02-IDEATION_stream_view_mode_for_streamed_responses.md`.

The goal is to let users switch between:

- `Raw`: the exact stored response body
- `Assembled`: a derived, human-readable view for supported streamed responses

The implementation should preserve the current database schema and raw log
storage behavior.

## Guiding Decisions

### 1. Keep raw storage unchanged

The database remains the source of truth. No schema change is needed. Existing
fields such as `resp_body`, `resp_truncated`, `request_path`, and
`resp_headers_json` remain authoritative.

Why:

- avoids migration work
- preserves debugging fidelity
- keeps backward compatibility for CLI, API, and existing logs

### 2. Put stream assembly logic in Go, not in JavaScript

The assembly/parsing logic should be implemented once in a shared Go package and
reused by:

- the TUI
- the management API used by the web UI

Why:

- centralizes the hard logic
- makes parser behavior testable with normal Go unit tests
- avoids implementing and maintaining the same SSE parsing logic twice

### 3. Start with a narrow endpoint scope

The first version should support assembled rendering only for:

- `/v1/chat/completions`
- `/v1/completions`

Why:

- keeps parser behavior explicit
- reduces ambiguity around different event formats
- gives a smaller, testable first surface

### 4. Only expose assembled mode when confidence is high

`Assembled` should only be available when:

- `request_path` is supported
- `resp_truncated` is `false`
- `resp_body` is present
- the response looks like SSE
- parsing either succeeds fully or succeeds partially with recoverable text

Why:

- avoids showing misleading partial output
- makes the UI contract predictable

## High-Level Design

Introduce a new internal package responsible for deriving stream-view metadata
from an existing `database.LogEntry`.

That package should:

- inspect whether a log entry is eligible for stream assembly
- parse supported SSE response bodies
- extract the assembled text
- return view metadata explaining whether assembled mode is available

The TUI will use this package directly when rendering the detail view.

The management API will use the same package to augment the detail response for
the web UI.

The web UI will then render a simple `Raw` / `Assembled` toggle without needing
to understand SSE internals.

## File-Level Plan

### 1. Add new package: `internal/streamview/`

Create a new package to encapsulate derived stream-view behavior.

New files:

- `internal/streamview/view.go`
- `internal/streamview/openai_sse.go`
- `internal/streamview/view_test.go`
- `internal/streamview/openai_sse_test.go`

#### `internal/streamview/view.go`

Define the public types and top-level entry point.

Suggested types:

```go
package streamview

import "memoryelaine/internal/database"

type Mode string

const (
    ModeRaw       Mode = "raw"
    ModeAssembled Mode = "assembled"
)

type AvailabilityReason string

const (
    ReasonSupported          AvailabilityReason = "supported"
    ReasonPartialParse       AvailabilityReason = "partial_parse"
    ReasonUnsupportedPath    AvailabilityReason = "unsupported_path"
    ReasonUnsupportedMultiChoice AvailabilityReason = "unsupported_multi_choice"
    ReasonUnsupportedToolCallStream AvailabilityReason = "unsupported_tool_call_stream"
    ReasonMissingBody        AvailabilityReason = "missing_body"
    ReasonTruncated          AvailabilityReason = "truncated"
    ReasonNotSSE             AvailabilityReason = "not_sse"
    ReasonParseFailed        AvailabilityReason = "parse_failed"
)

type Result struct {
    RawBody            string
    AssembledBody      string
    AssembledAvailable bool
    Reason             AvailabilityReason
    Format             string
}

func Build(entry database.LogEntry) Result
```

Responsibilities:

- return raw body directly from `entry.RespBody`
- determine whether the entry is eligible for assembly
- dispatch to endpoint-specific assembly helpers
- report whether assembled mode is fully available, partially available, or
  unavailable

Why this structure:

- keeps TUI/web behavior consistent
- makes fallback behavior explicit
- makes tests precise

#### `internal/streamview/openai_sse.go`

Implement parsing for supported streamed OpenAI-compatible responses.

Recommended responsibilities:

- split SSE stream into events, handling both LF and CRLF framing
- ignore empty/comment lines where appropriate
- process `data:` payload lines
- stop cleanly on `[DONE]`
- parse JSON event payloads

For `/v1/chat/completions`:

- iterate over `choices[0].delta.content`
- append text chunks in order
- detect and reject multi-choice streams with
  `ReasonUnsupportedMultiChoice`
- detect tool-call / function-call deltas and reject with
  `ReasonUnsupportedToolCallStream`

For `/v1/completions`:

- iterate over `choices[0].text`
- append chunks in order
- detect and reject multi-choice streams with
  `ReasonUnsupportedMultiChoice`

Partial parse behavior:

- if earlier SSE events parse successfully but a later event is malformed,
  return the recovered text with `ReasonPartialParse`
- partial parse is only valid when at least one supported text fragment was
  recovered before failure
- if no useful text was recovered, fail closed with `ReasonParseFailed`

Multi-choice and tool-call behavior:

- do not silently assemble only choice `0`
- do not inject placeholder text into assembled output
- instead, expose explicit metadata via `ReasonUnsupportedMultiChoice` or
  `ReasonUnsupportedToolCallStream` and fall back to raw view

Why:

- partial recovery is better than discarding obviously useful text
- explicit unsupported reasons avoid misleading output
- avoiding placeholder injection keeps assembled text semantically clean

### 2. Update management API: `internal/management/api.go`

The web UI currently receives raw `database.LogEntry` JSON from
`GET /api/logs/{id}`. That is not enough for a viewer toggle unless the web app
also implements parser logic.

Change this handler to return a dedicated detail response object that includes
derived stream-view information.

Suggested response shape:

```go
type logDetailResponse struct {
    Entry      database.LogEntry   `json:"entry"`
    StreamView streamViewResponse  `json:"stream_view"`
}

type streamViewResponse struct {
    AssembledBody      string `json:"assembled_body,omitempty"`
    AssembledAvailable bool   `json:"assembled_available"`
    Reason             string `json:"reason"`
    Format             string `json:"format,omitempty"`
}
```

Handler changes:

- keep `/api/logs` list response unchanged for now
- change `/api/logs/{id}` to wrap the log entry instead of returning it bare
- compute `streamview.Build(*entry)` inside the handler

Why:

- keeps parsing logic out of JS
- limits API churn to the detail endpoint
- avoids affecting CLI/TUI or list/table rendering
- avoids duplicating `resp_body` in large detail responses

Test impact:

- existing tests assuming bare `LogEntry` JSON from `/api/logs/{id}` must be
  updated
- add explicit tests for `stream_view` on supported and unsupported entries

### 3. Update management router tests: `internal/management/server_test.go`

Add or update tests covering:

- `/api/logs/{id}` returns `entry` plus `stream_view`
- supported chat SSE returns `assembled_available=true`
- supported completions SSE returns expected `assembled_body`
- partially parsed streams return `assembled_available=true` with reason
  `partial_parse`
- truncated responses return `assembled_available=false` with reason
  `truncated`
- non-stream response returns `assembled_available=false`
- unsupported path returns `assembled_available=false`
- multi-choice stream returns `assembled_available=false` with reason
  `unsupported_multi_choice`
- tool-call stream returns `assembled_available=false` with reason
  `unsupported_tool_call_stream`

Also keep the existing auth and empty-state tests intact.

Why:

- the web UI depends entirely on this API contract
- this is the main integration boundary for assembled mode

### 4. Update TUI detail rendering: `internal/tui/model.go`

The current detail view renders only the raw response body.

Required changes:

- extend `Model` with a stream view state for detail mode
- default detail view mode to `Raw`
- when a detail entry loads, call `streamview.Build(*entry)` inside the
  background `tea.Cmd`, not inside `Update()`
- render a mode indicator in the detail panel
- add a keybinding to toggle modes only when assembled mode is available

Suggested model additions:

```go
type streamViewState struct {
    mode   streamview.Mode
    result streamview.Result
}
```

Suggested keybinding:

- `v`: toggle `Raw` / `Assembled`

Suggested detail rendering behavior:

- always show metadata near the response section:
  - `Stream View: Raw`
  - `Stream View: Assembled`
- if the assembled result is partial, show a warning such as:
  - `Assembled (partial parse)`
- if assembled is unavailable, show:
  - `Assembled unavailable: truncated`
  - or equivalent human-readable message
- response body section renders either `result.RawBody` or
  `result.AssembledBody`

Why:

- preserves the current TUI structure
- keeps new state local to detail mode
- adds minimal input complexity

Test impact:

- add TUI unit tests focused on rendered strings and key handling

### 5. Add TUI tests: new file `internal/tui/model_test.go`

There are currently no TUI tests. This feature is a good reason to add them.

Recommended tests:

- detail view defaults to `Raw`
- pressing `v` switches to `Assembled` when available
- pressing `v` does nothing when assembled is unavailable
- detail view displays a partial-parse warning when applicable
- detail view displays availability reason for truncated streams
- detail view renders expected assembled text for a supported sample entry

Keep these tests narrow:

- construct `Model` directly
- avoid full Bubble Tea runtime integration where unnecessary
- assert on `View()` output and `Update()` transitions

Why:

- protects UI behavior without requiring terminal snapshot machinery
- validates that the parser result is actually wired into the TUI

### 6. Update web UI markup: `internal/web/static/index.html`

The detail overlay needs controls for stream view mode.

Add within the detail panel:

- a toggle group or two buttons for `Raw` and `Assembled`
- a small status area for availability / fallback message
- a dedicated container for response body content

The table view does not need changes for v1.

Why:

- keep the feature localized to the detail experience
- avoid cluttering the main list/table

### 7. Update web UI behavior: `internal/web/static/app.js`

The current web UI renders raw detail content directly from the log entry.

Required changes:

- adapt detail fetch handling to the new `/api/logs/{id}` response shape
- store the currently loaded detail payload in JS state
- render `Raw` / `Assembled` buttons based on `stream_view`
- default to `Raw`
- disable or hide the `Assembled` button when unavailable
- re-render the response body when the selected mode changes

Recommended structure:

- keep `currentDetail` and `currentStreamViewMode` module-local
- split rendering into:
  - `renderDetail(entry, streamView)`
  - `renderResponseBody(streamView, mode)`

Why:

- keeps the JS change small
- makes the toggle behavior explicit and debuggable

Testability note:

There is no existing JS test harness in this repository. To keep the feature
testable without introducing frontend tooling just for this change, the web UI
should remain intentionally thin and rely on the Go API contract for the hard
logic.

Rendering note:

- assembled output must be treated as plain text
- pass it through the same escaping path as raw output
- render it inside a `<pre>` or equivalent plain-text container

### 8. Update web UI styling: `internal/web/static/style.css`

Add minimal styles for:

- stream view toggle buttons
- active/inactive button states
- unavailable-mode message

Why:

- avoids an ambiguous or visually broken detail view
- keeps the feature discoverable

### 9. Update docs: `README.md`

The README should document:

- that streamed responses can be viewed in `Raw` and `Assembled` modes
- that v1 assembled mode supports only:
  - `/v1/chat/completions`
  - `/v1/completions`
- that assembled mode is unavailable for truncated or unsupported responses
- the TUI keybinding, if added as `v`

Why:

- this is user-visible behavior
- the scope limits need to be stated explicitly

## Detailed Parsing Rules

These rules should be implemented and tested explicitly.

### Input eligibility

Assembly should only be attempted if:

- `entry.RespBody != nil`
- `entry.RespTruncated == false`
- `entry.RequestPath` is one of the supported paths
- the response body contains SSE-style `data:` lines

Do not rely only on stored response headers for eligibility, because:

- headers may be absent in some edge cases
- body inspection is enough for v1

Normalize SSE framing so both `\n\n` and `\r\n\r\n` event separators are
handled correctly.

### `/v1/chat/completions`

For each SSE `data:` payload:

- ignore `[DONE]`
- decode JSON
- inspect `choices`
- if more than one choice is present, fail with `ReasonUnsupportedMultiChoice`
- append `choices[0].delta.content` when present

Handle for v1:

- role-only deltas: ignore
- tool call deltas: fail with `ReasonUnsupportedToolCallStream`
- function-call fragments: fail with `ReasonUnsupportedToolCallStream`
- usage events: ignore

If the stream contains only ignored deltas and no text content, assembled mode
may still be considered available but yield an empty string. That should be
decided explicitly during implementation. My recommendation is:

- assembled mode is available
- assembled body may be empty

This is less surprising than treating a valid textless stream as a parse error.

If parsing later fails after some valid text has already been accumulated:

- return the accumulated text
- mark the result `ReasonPartialParse`
- keep `AssembledAvailable = true`

### `/v1/completions`

For each SSE `data:` payload:

- ignore `[DONE]`
- decode JSON
- if more than one choice is present, fail with `ReasonUnsupportedMultiChoice`
- append `choices[0].text` when present

### Failure behavior

Treat the following as `Assembled unavailable`:

- invalid JSON in an early `data:` event before any text is recovered
- malformed SSE framing that prevents event extraction
- unsupported path
- truncated response
- multi-choice response
- tool-call stream

The returned reason should be machine-stable so tests can assert it.

Treat the following as `Assembled available with warning`:

- malformed or incomplete later event after recoverable text has already been
  assembled

## Sequence of Work

Implement in this order:

1. Add `internal/streamview` package and unit tests.
2. Update management API response shape and management tests.
3. Update TUI detail mode and add TUI tests.
4. Update web UI markup/JS/CSS to consume the new API response.
5. Update README.

Why this order:

- parser correctness is the hard part and should be stabilized first
- API contract should be finalized before UI work
- TUI and web UI can then become mostly wiring/rendering changes

## Test Plan

### Unit tests: `internal/streamview`

Table-driven tests should cover:

- supported chat SSE with multiple text deltas
- supported completions SSE with multiple text chunks
- `[DONE]` handling
- CRLF-framed SSE input
- empty SSE payload lines
- non-SSE body
- invalid JSON event before any text
- invalid JSON event after valid text
- truncated response
- unsupported path
- missing response body
- multi-choice response
- tool-call stream

Also include realistic fixtures that resemble OpenAI-compatible stream payloads.

### Integration tests: `internal/management`

Add tests that insert synthetic log entries into SQLite and assert:

- `/api/logs/{id}` response shape
- `assembled_available`
- `assembled_body`
- `reason`

This verifies the parser is wired into the server response correctly.

### UI tests: `internal/tui`

Add tests for:

- mode toggle behavior
- rendered content in raw mode
- rendered content in assembled mode
- rendered content in partial-parse mode
- unavailability messaging

### Manual verification: web UI

Because there is no browser automation in the repo today, the web UI should be
verified manually after implementation:

1. start `memoryelaine serve`
2. generate at least one streamed `/v1/chat/completions` log
3. open the detail view in the browser
4. confirm `Raw` and `Assembled` both render
5. confirm a partial-parse sample shows assembled text with warning state
6. confirm a truncated sample shows only `Raw`

## Risks and Mitigations

### Risk: duplicated parsing behavior across viewers

Mitigation:

- do not implement SSE assembly in JS
- keep parsing only in `internal/streamview`

### Risk: ambiguous behavior for non-text stream events

Mitigation:

- define narrow v1 parsing rules
- ignore unsupported delta fields
- reject unsupported tool-call streams explicitly
- document the limitation in README

### Risk: accidental API break for the web UI

Mitigation:

- only change `/api/logs/{id}`
- add server tests before wiring the web UI
- keep `/api/logs` list response unchanged

### Risk: user confusion when assembled mode is unavailable

Mitigation:

- expose explicit reason metadata
- show a clear fallback message in both TUI and web UI

## Out of Scope for This Plan

- changing database schema
- changing capture behavior in the proxy
- assembled output in the CLI `memoryelaine log`
- support for tool call assembly
- support for multi-choice response visualization
- support for every OpenAI-compatible streaming dialect

## Definition of Done

The feature is complete when:

1. raw response storage remains unchanged
2. supported streamed log entries can be viewed as `Raw` or `Assembled` in the
   TUI
3. supported streamed log entries can be viewed as `Raw` or `Assembled` in the
   web UI
4. unsupported, truncated, or unparsable streams clearly fall back to `Raw`
5. partially parseable streams can show recovered text with a warning state
6. parser logic is covered by Go unit tests
7. management API contract is covered by integration tests
8. TUI toggle behavior is covered by tests
