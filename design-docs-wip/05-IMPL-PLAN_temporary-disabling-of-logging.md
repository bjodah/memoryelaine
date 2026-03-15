# Implementation Plan for Temporarily Disabling Logging

I would like to add the functionality to temporarily disable logging. So we need to track a recording state. I want to be able to probe its value via the management endpoint, is it conventional to include it in e.g. /health? or a dedicated enpoint. I would also like to be able to set it "on"/"off" via an API call or via TUI, or via Web-UI. I also want to recording state to be clearly visible in both the TUI and Web-UI.

## 0. General insights

To safely toggle logging without restarting the proxy, we need an `atomic.Bool` state shared between the Proxy handler and the Management API.

**Crucial Architecture Insight:** The `memoryelaine tui` is executed as a separate CLI process. It reads directly from SQLite. Because the memory state (recording on/off) lives inside the `memoryelaine serve` process, the TUI *must* use an HTTP client to talk to the management API to fetch and toggle the state.

Here is the detailed, file-by-file implementation plan.

## 1. Core State & Command Wiring
**File:** `cmd/serve.go`
*   **What to change:**
    *   Create `recordingState := &atomic.Bool{}` and initialize it to `true`.
    *   Pass `recordingState` into `proxy.Handler`.
    *   Pass `recordingState` into `management.NewMux` via `management.ServerDeps`.
*   **Why:** This establishes a thread-safe source of truth in memory for the proxy server.

## 2. Proxy Bypass Logic
**File:** `internal/proxy/proxy.go`
*   **What to change:**
    *   Update the `Handler` signature to accept `recordingState *atomic.Bool`.
    *   In the returned `http.HandlerFunc`, immediately after checking `logPathSet`, add:
        ```go
        if !recordingState.Load() {
            rpPlain.ServeHTTP(w, r)
            return
        }
        ```
*   **Why:** If recording is paused, we want zero overhead. Falling back to `rpPlain` achieves this perfectly, functioning exactly like an un-logged path.

## 3. Management API Endpoints
**Files:** `internal/management/server.go`, `internal/management/api.go`, `internal/management/health.go`
*   **What to change:**
    *   **`server.go`**: Add `RecordingState *atomic.Bool` to `ServerDeps`. Register a new endpoint: `mux.Handle("/api/recording", basicAuth(apiRecordingHandler(deps.RecordingState), deps.Auth.Username, deps.Auth.Password))`.
    *   **`health.go`**: Add `"recording": deps.RecordingState.Load()` to the JSON response map. (This satisfies the requirement to probe it via `/health`).
    *   **`api.go`**: Create `apiRecordingHandler`. It handles two methods:
        *   `GET`: Returns `{"recording": true|false}`.
        *   `PUT` (or `POST`): Decodes a JSON body `{"recording": bool}`, updates `state.Store(value)`, and returns the new state.
*   **Why:** Allows external systems (and our Web UI / TUI) to read and manipulate the state.

## 4. Web UI Updates
**Files:** `internal/web/static/index.html`, `internal/web/static/style.css`, `internal/web/static/app.js`
*   **What to change:**
    *   **`index.html`**: Add a button in the `.controls` div: `<button id="recording-toggle-btn">🔴 REC</button>`.
    *   **`style.css`**: Add styles to make the button look distinct when paused (e.g., greyed out with `⏸ PAUSED` text).
    *   **`app.js`**:
        *   Add a function `fetchRecordingState()` that GETs `/health` (or `/api/recording`), updates a local JS variable, and updates the button text/color.
        *   Add a click listener to the button that sends a `PUT /api/recording` with the inverted state, then re-fetches.
        *   Call `fetchRecordingState()` on initial load and piggyback it onto the `autoRefreshTimer` if active.
*   **Why:** Provides the required visual indicator and toggle mechanism in the Web UI.

## 5. TUI Updates
**Files:** `cmd/tui.go`, `internal/tui/app.go`, `internal/tui/model.go`
*   **What to change:**
    *   **`cmd/tui.go` & `app.go`**: The TUI needs to make HTTP calls. Pass `cfg.Management` into `tui.Run()`.
    *   **`model.go`**:
        *   Add `management config.ManagementConfig` and `isRecording bool` to `Model`.
        *   Create two new `tea.Cmd`s: `checkRecordingCmd` (GET `/api/recording` via `http.Client` using Basic Auth) and `toggleRecordingCmd` (PUT `/api/recording`).
        *   Add new message types: `recordingStateMsg(bool)`.
        *   On `Init()`, return `tea.Batch(m.loadLogs, m.checkRecordingCmd)`.
        *   In `handleKey`, add a binding for `"p"` (Pause/Play). When pressed, return `m.toggleRecordingCmd`.
        *   In `tableView()` and `detailView()`, append `[🔴 REC]` or `[⏸ PAUSED]` to the `title` variable at the top of the screen.
*   **Why:** The TUI is a separate process. It must use HTTP to mutate the active server's state.

## 6. Required Tests

**Proxy Tests (`internal/proxy/handler_test.go`)**
*   **Add `TestHandler_RecordingPaused_Bypasses`**:
    *   Set up the proxy test environment.
    *   Pass an `atomic.Bool` set to `false`.
    *   Send a request to a loggable path (e.g., `/v1/chat/completions`).
    *   Assert that `rec.Code == 200` but `lw.DroppedCount() == 0` (and `lw.queue` is empty if checked via sleep), proving it used `rpPlain`.

**Management Tests (`internal/management/server_test.go`)**
*   **Add `TestAPIRecording_GetAndPut`**:
    *   Initialize `ServerDeps` with `RecordingState` set to `true`.
    *   Perform a `GET /api/recording` (with auth) and assert it returns `{"recording": true}`.
    *   Perform a `PUT /api/recording` with body `{"recording": false}`.
    *   Assert the response is `200 OK`.
    *   Assert `RecordingState.Load() == false` internally.
    *   Check `GET /health` to ensure `"recording": false` appears there too.
