# Implementation Plan for Temporarily Disabling Logging

Add a runtime recording state so request/response capture can be paused without
restarting `memoryelaine serve`.

The revised scope is:

- update the main spec in `design-docs-main/`
- expose recording state publicly via `/health`
- add an authenticated management endpoint to read and change recording state
- make recording state visible and toggleable in the Web UI
- keep the TUI a pure log-viewing utility with no runtime-control features

## 0. Semantics to Document

The feature needs explicit specification, not just code changes.

- `recording=true` means loggable paths are captured and written as usual.
- `recording=false` means loggable paths are still proxied, but request/response
  capture to SQLite is bypassed entirely.
- The recording decision is taken at request start.
- If a request begins while recording is enabled, it is fully logged even if
  recording is disabled while that request is still in flight.
- `/health` will expose `"recording": true|false` publicly.
- `/last-request` and `/last-response` continue returning the last captured
  bodies, but must be labeled stale once at least one loggable request has been
  proxied while recording is disabled.

## 1. Spec Update
**Files:** `design-docs-main/04-SPEC-version2.md`, `README.md`

*   **What to change:**
    *   Extend the logging/runtime semantics to define the recording state and
        its request-start boundary.
    *   Add `GET/PUT /api/recording` to the management API.
    *   Extend `/health` response shape with `recording`.
    *   Clarify stale behavior for `/last-request` and `/last-response`.
    *   Update Web UI behavior to include recording-state visibility and toggle.
    *   Keep TUI behavior unchanged apart from remaining a read-only viewer.
*   **Why:** The main spec currently defines the management surface and viewer
    behavior tightly; this feature is a real spec change.

## 2. Core State & Command Wiring
**File:** `cmd/serve.go`

*   **What to change:**
    *   Create a shared runtime recording controller/state object initialized to
        `true`.
    *   Pass it into `proxy.Handler`.
    *   Pass it into `management.NewMux` via `management.ServerDeps`.
*   **Why:** This establishes a thread-safe source of truth in memory for the
    serve process.

## 3. Proxy Bypass Logic
**File:** `internal/proxy/proxy.go`

*   **What to change:**
    *   Update `Handler` to accept the shared runtime recording controller.
    *   For loggable paths, check the recording state before capture wiring is
        set up.
    *   If recording is disabled, mark the last-body state as stale for that
        loggable request and forward through `rpPlain`.
*   **Why:** This preserves normal proxy behavior while removing capture
    overhead during pause.

## 4. Management API Endpoints
**Files:** `internal/management/server.go`, `internal/management/api.go`,
`internal/management/health.go`

*   **What to change:**
    *   Add the runtime recording controller to `ServerDeps`.
    *   Register a new authenticated endpoint:
        `mux.Handle("/api/recording", basicAuth(apiRecordingHandler(...)))`.
    *   Add `"recording": ...` to `/health`.
    *   Implement `apiRecordingHandler`:
        *   `GET` returns `{"recording": true|false}`
        *   `PUT` accepts `{"recording": bool}`, updates state, and returns the
            new state
    *   Update `/last-request` and `/last-response` to label stale results when
        the runtime state says the last captured bodies have become stale.
*   **Why:** The management API is the correct runtime-control surface.

## 5. Web UI Updates
**Files:** `internal/web/static/index.html`, `internal/web/static/style.css`,
`internal/web/static/app.js`

*   **What to change:**
    *   Add a recording-state indicator/button in the main controls area.
    *   Fetch recording state from `/health` for display.
    *   Toggle recording state via authenticated `PUT /api/recording`.
    *   Refresh the visible recording indicator on initial load and during
        auto-refresh.
*   **Why:** The Web UI is part of the management surface and should make the
    active state obvious.

## 6. TUI Non-Changes

The TUI remains read-only for this feature.

- no HTTP client wiring in `memoryelaine tui`
- no new runtime-control keybindings
- no recording-state indicator requirement in the TUI

## 7. Required Tests

**Proxy Tests (`internal/proxy/handler_test.go`)**

*   **Add `TestHandler_RecordingPaused_Bypasses`**:
    *   Set up the proxy test environment.
    *   Pass recording state initialized to `false`.
    *   Send a request to a loggable path.
    *   Assert proxying still succeeds.
    *   Assert no SQLite log entry is written.
    *   Assert last-body stale state is marked after that paused request.

**Management Tests (`internal/management/server_test.go`)**

*   **Add `TestAPIRecording_GetAndPut`**:
    *   Initialize `ServerDeps` with recording enabled.
    *   Assert `GET /api/recording` returns `true`.
    *   `PUT` `{"recording":false}` and assert the shared state changed.
    *   Assert `GET /health` includes `"recording": false`.

*   **Add stale last-body tests**:
    *   Seed `last-request` / `last-response`.
    *   Mark stale after a paused loggable request.
    *   Assert `/last-request` and `/last-response` clearly label the values as
        stale.
