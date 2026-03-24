Here is the detailed implementation plan for the `v1/chat/completions` specialization, formatted as a standard design document. 

***

# IMPLEMENTATION-PLAN-CHAT-SPECIALIZATION.md

## 1. Goals

The primary goals of this specialization are to solve two major friction points when proxying and inspecting `v1/chat/completions` traffic:

1. **Storage Deduplication (Backend):** Chat completion requests typically contain a `messages` array that grows linearly with every turn. Storing the full JSON payload for every turn results in quadratic storage bloat. We will restructure the storage to act as a linked list (or tree), explicitly storing only the *new* messages (the delta) and a pointer to the parent request.
2. **Conversation Readability (Frontend/UIs):** Viewing a raw JSON payload with 20 historical messages is unreadable. We will introduce a specialized "Conversation / Thread View" in the Emacs client, the Terminal UI, and the Web UI. This view will traverse the linked list of requests and present the conversation linearly (like a standard chat application).

## 2. Choices, Compromises, and Alternatives Considered

### 2.1 Deduplication Strategy
* **Alternative 1: Client-Side Conversation IDs.** Rely on clients to send a custom HTTP header (e.g., `X-Conversation-ID`). 
  * *Rejected:* We cannot guarantee clients will alter their OpenAI SDK configurations to inject custom headers. The proxy must work transparently.
* **Alternative 2: Exact String Matching.** 
  * *Rejected:* JSON serialization can vary (whitespace, key ordering). String matching is brittle.
* **Chosen Approach: Canonical Hash Matching in the Background Worker.** 
  The background `LogWriter` will parse the `messages` array of incoming `/v1/chat/completions` requests. It will compute a SHA-256 hash of the canonicalized `messages[0 : len-1]` array. It will query the database for a recent request with a matching `chat_hash`. If found, it links them via a `parent_id`, sets the `req_body` to NULL, and stores only the final message(s) in a new `req_delta` column. 
  * *Compromise:* Parsing JSON in the proxy path would add latency. By moving this to the asynchronous `LogWriter`, we preserve the strict "zero-latency" proxying guarantee, at the cost of slight background CPU usage.

### 2.2 Reconstructing Deduplicated Bodies
* **Alternative 1: UIs assemble the bodies.** UIs fetch the linked list and reconstruct the JSON.
  * *Rejected:* Pushes heavy logic to Emacs Elisp, JS, and TUI separately.
* **Chosen Approach: Backend API transparency.** 
  A new endpoint `/api/logs/{id}/thread` will be created specifically for the UI Conversation mode. However, for legacy compatibility, if a client requests the raw body via `/api/logs/{id}/body`, the backend will traverse the database, reconstruct the full original `req_body`, and serve it transparently. 

### 2.3 Full Text Search (FTS) Implications
* *Bonus Win:* By deduplicating storage and making `req_body` NULL, the FTS index will only index the `req_delta`. This means searching for a specific word will yield exactly *one* result (the turn where it was introduced), rather than 50 results representing every subsequent turn of the conversation.

---

## 3. Implementation Steps: Backend (Go)

### 3.1 Database Schema & Models
**Files:** `internal/database/db.go`, `internal/database/models.go`
* **Changes:**
  * Update `schema` string in `db.go` to add new columns to `openai_logs`:
    ```sql
    ALTER TABLE openai_logs ADD COLUMN parent_id INTEGER REFERENCES openai_logs(id);
    ALTER TABLE openai_logs ADD COLUMN chat_hash TEXT;
    ALTER TABLE openai_logs ADD COLUMN req_delta TEXT;
    CREATE INDEX IF NOT EXISTS idx_chat_hash ON openai_logs(chat_hash);
    ```
  * Update `ftsSchema` triggers to coalesce `req_body` and `req_delta`:
    ```sql
    VALUES (new.id, COALESCE(new.req_body, new.req_delta), new.resp_body);
    ```
  * Update `LogEntry` in `models.go` to include `ParentID *int64`, `ChatHash *string`, and `ReqDelta *string`.

### 3.2 Asynchronous Deduplication Logic
**Files:** `internal/database/writer.go`, `internal/chat/hash.go` (New File)
* **Changes:**
  * Create `internal/chat/hash.go` with functions to unmarshal a chat request, extract the `messages` array, and compute two SHA-256 hashes (one for the full array, one for the array minus the latest user message).
  * In `writer.go`, inside the `insert(entry LogEntry)` function:
    * If `entry.RequestPath == "/v1/chat/completions" && entry.ReqBody != "" && !entry.ReqTruncated`:
      * Parse the JSON. Extract messages.
      * Calculate `parentHash` (messages `0` to `n-1`) and `currentHash` (messages `0` to `n`).
      * Execute a quick `SELECT id FROM openai_logs WHERE chat_hash = ? ORDER BY id DESC LIMIT 1` using `parentHash`.
      * If found: Set `entry.ParentID`, set `entry.ChatHash = currentHash`. JSON-encode the delta (the new messages) into `entry.ReqDelta`. Set `entry.ReqBody = ""`.
      * If not found: Set `entry.ChatHash = currentHash`. Leave `req_body` intact.

### 3.3 Database Reader Updates
**Files:** `internal/database/reader.go`
* **Changes:**
  * Add a function `GetThread(id int64) ([]LogEntry, error)`.
    * This function will use a Recursive CTE (Common Table Expression) in SQLite to fetch the requested ID and traverse *up* the `parent_id` chain to the root.
    * Reverse the resulting slice so the chronological conversation is from index `0` to `N`.
  * Update `GetByID` and `Query` to include the new columns.

### 3.4 Management API Updates
**Files:** `internal/management/api.go`, `internal/management/dto.go`, `internal/management/server.go`
* **Changes:**
  * Update `LogDetailEntry` in `dto.go` to expose `ParentID`.
  * Create `ThreadMessage` DTO: `{ LogID, Role, Content, Timestamp }`.
  * Add `apiThreadHandler(reader *database.LogReader)` mapped to `GET /api/logs/{id}/thread`.
    * Calls `GetThread(id)`.
    * Iterates through the returned `LogEntry` list.
    * Parses `ReqBody` (for root) and `ReqDelta` (for children) to extract User messages.
    * Parses `RespBody` (using the existing `streamview` package) to extract the Assistant's assembled text.
    * Returns a flat JSON array of `ThreadMessage` objects representing the chronological conversation.
  * Modify `handleBody` (for `/api/logs/{id}/body?part=req`):
    * If a request has a `ParentID` and no `ReqBody`, it must call `GetThread(id)`, reconstruct the full `messages` array by concatenating the root and all deltas, and return the reconstructed JSON, ensuring the "Raw" view still works seamlessly.

---

## 4. Implementation Steps: Frontends & UIs

### 4.1 Terminal UI (Bubbletea)
**Files:** `internal/tui/model.go`
* **Changes:**
  * Add a new view mode: `modeThread`.
  * Add keybinding `c` (Conversation) in both `modeTable` and `modeDetail` that triggers a `loadThread(id)` command.
  * Create a `threadView()` rendering function:
    * Use `lipgloss` to render chat bubbles.
    * User messages aligned to the right (or colored differently, e.g., Blue).
    * Assistant messages aligned to the left (colored Green).
    * Include metadata headers above each bubble (e.g., `Log #123 • 14:02:01`).
  * Add `esc`/`q` to return from `modeThread` back to `modeTable` or `modeDetail`.

### 4.2 Web UI
**Files:** `internal/web/static/app.js`, `internal/web/static/index.html`, `internal/web/static/style.css`
* **Changes:**
  * **HTML:** Add a `<button id="view-thread-btn" class="hidden">View Conversation</button>` inside the detail overlay. Add a `<div id="thread-container" class="hidden"></div>`.
  * **CSS:** Add styles for `.chat-bubble.user` (right-aligned, distinct background) and `.chat-bubble.assistant` (left-aligned, distinct background).
  * **JS:** 
    * In `showDetail(id)`, if the endpoint is `/v1/chat/completions`, unhide the "View Conversation" button.
    * Add click listener to fetch `/api/logs/{id}/thread`.
    * Hide the standard Request/Response sections and render the `thread-container` using the returned array of messages.

### 4.3 Emacs Client
**Files:** `emacs/memoryelaine-show.el`, `emacs/memoryelaine-http.el`, `emacs/memoryelaine.el`
* **Changes:**
  * **Keybindings:** Bind `c` in `memoryelaine-show-mode-map` and `memoryelaine-search-mode-map` to a new function `memoryelaine-show-thread`.
  * **State:** Create `memoryelaine-state--thread-data` to cache thread responses.
  * **New Buffer/Mode:** Create a new derived mode `memoryelaine-thread-mode` (derived from `special-mode`) running in a buffer named `*memoryelaine-thread*`.
  * **HTTP:** Add logic to fetch `/api/logs/{id}/thread`.
  * **Rendering:** 
    * Insert propertized text to distinguish roles.
    * Example format:
      ```text
      ==================================================
      [User] (Log #42 - 10:15:00)
      Can you write a python script to...
      
      ==================================================
      [Assistant] (Log #42)
      Certainly! Here is the script...
      
      ==================================================
      [User] (Log #43 - 10:16:30)
      Now add error handling to it.
      ```
    * Propertize the `[User]` and `[Assistant]` tags with distinct faces (e.g., `font-lock-keyword-face` vs `font-lock-string-face`).
    * Allow pressing `RET` on the `(Log #42)` header to jump directly back to the raw detail view for that specific network exchange.

---

## 5. Security & Fallback Considerations

1. **Truncated Requests:** If a request body exceeds `logging.max_capture_bytes`, it is marked as `req_truncated = true`. The background worker **must skip deduplication** for truncated requests, as parsing partial JSON will fail and computing a valid hash is impossible. These will be stored as raw text, breaking the linked list for that specific turn (acceptable fallback).
2. **Missing Upstream Responses:** If the upstream fails (502) or the response is empty, the request is still deduplicated and linked. The thread view will simply show a User message without a following Assistant message, which accurately reflects network reality.
3. **Failing Gracefully:** If the `/thread` endpoint encounters unparseable JSON in a historical `req_delta` or root `req_body`, it should log an internal error, return the thread up to the broken point, and append a synthetic system message: `[Error: Conversation history could not be fully reconstructed]`.
