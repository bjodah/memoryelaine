# IMPLEMENTATION-PLAN-CHAT-SPECIALIZATION.md

## 1. Overview & Goals

The `memoryelaine` proxy effectively captures traffic with zero added latency, but the developer experience when inspecting `/v1/chat/completions` traffic suffers from two major issues:
1. **Unreadable JSON Arrays:** Users currently have to read raw JSON arrays of historical messages, making it difficult to understand the actual flow of a conversation.
2. **Broken Full-Text Search (FTS) on Streams:** The FTS5 engine currently indexes the raw Server-Sent Events (SSE) byte stream. Because words are chunked across SSE payloads (e.g., `{"cont`, `ent": "Hello"}`), searching for "Hello" fails.

**Goals of this Implementation:**
1. **Fix FTS for Streams:** Extract continuous plain-text from both requests and responses asynchronously, indexing this assembled text instead of the raw network bytes.
2. **Conversation Lineage:** Link sequential requests in the same conversation using cryptographic hashes of message prefixes.
3. **Threaded UIs:** Introduce a specialized, linear "Conversation View" in the Emacs, Terminal, and Web UIs that hides the JSON/SSE framing and presents a clean chat interface.

---

## 2. Background & Architectural Decisions

Following a review of various deduplication and storage strategies, we have established the following architectural constraints:

### 2.1 Decision A: Canonical Storage + Sidecar Text for FTS
We will **strictly preserve** the exact raw network bytes in `req_body` and `resp_body`. Maintaining raw bytes is critical for debugging SDK/network anomalies (e.g., malformed SSE frames). 
To fix the FTS search bug, the asynchronous background worker will parse the requests and responses, extract the plain text, and store it in two new "sidecar" columns: `req_text` and `resp_text`. The SQLite FTS5 virtual table will be repointed to index these new text columns instead of the raw JSON/SSE columns.

### 2.2 Decision B: Lineage Tracking without Deduplication
While storing the full message history on every turn results in quadratic storage growth, attempting to deduplicate the storage by nulling out `req_body` introduces severe risks. Top-level parameters (e.g., `temperature`, `tools`) may change between turns, and reconstructing a bit-for-bit perfect request payload from deltas is brittle.
Instead, we will add a `parent_id` and a `chat_hash` column. The background worker will compute prefix hashes to link requests together, allowing the UI to traverse the thread. Storage deduplication is explicitly deferred.

### 2.3 Decision C: Text-Only MVP for Conversation Views
The initial Conversation View will focus strictly on standard text interactions. If a message contains complex multi-modal arrays, tool-calls, or function-calls, the UI will display a placeholder (e.g., `[Complex turn: View in Raw Mode]`). This keeps the UI rendering logic simple while ensuring no data is hidden.

---

## 3. Backend Implementation: Database Schema & FTS

**File:** `internal/database/db.go`
*Because backward compatibility is not required, we will directly update the schema constants.*

1. **Update `schema`:**
   Add new columns to the `openai_logs` table:
   ```sql
   parent_id INTEGER REFERENCES openai_logs(id),
   chat_hash TEXT,
   req_text TEXT,
   resp_text TEXT,
   ```
   Add a new index: `CREATE INDEX IF NOT EXISTS idx_chat_hash ON openai_logs(chat_hash);`

2. **Update `ftsSchema`:**
   Repoint FTS to use the new text columns:
   ```sql
   CREATE VIRTUAL TABLE IF NOT EXISTS openai_logs_fts USING fts5(
       req_text,
       resp_text,
       content='openai_logs',
       content_rowid='id'
   );
   ```
   Update the triggers to insert `new.req_text` and `new.resp_text`.

**File:** `internal/database/models.go`
* Add `ParentID *int64`, `ChatHash *string`, `ReqText *string`, and `RespText *string` to the `LogEntry` struct.

---

## 4. Backend Implementation: Async Worker & Lineage Hashing

**File:** `internal/chat/hash.go` (New File)
* Create structs to unmarshal the `/v1/chat/completions` request `messages` array.
* Write a function `HashMessages(messages []Message) string` that computes a deterministic SHA-256 hash of the JSON representation of the messages array.
* Write a function `ExtractRequestText(messages []Message) string` that concatenates the string content of all messages into a single space-separated string for FTS indexing.

**File:** `internal/database/writer.go`
1. Update `insertSQL` to include the new columns.
2. In the `insert(entry LogEntry)` method, add a pre-processing step:
   * **Sidecar Text Generation:**
     * If `RequestPath == "/v1/chat/completions"`, parse `ReqBody`. Populate `ReqText` using `ExtractRequestText`.
     * Use `streamview.Build(&entry)` to get the `AssembledBody`. If `AssembledAvailable` is true, save it to `RespText`. If false, copy `RespBody` to `RespText` as a fallback.
   * **Lineage Tracking:**
     * If parsing `ReqBody` succeeds, we have a slice of messages $M$ of length $N$.
     * Compute `hash := HashMessages(M)`. Save as `entry.ChatHash`.
     * To find the parent, iterate $i$ backwards from $N-1$ down to 1 (usually, the previous request had $N-2$ messages: missing the new assistant reply and the new user prompt).
     * Hash the prefix $M[0:i]$ and query the database: `SELECT id FROM openai_logs WHERE chat_hash = ? ORDER BY id DESC LIMIT 1`.
     * Upon the first match, set `entry.ParentID`, and break the loop.

---

## 5. Backend Implementation: Management API & Thread Assembly

**File:** `internal/database/reader.go`
* Add `GetThread(id int64) ([]LogEntry, error)`.
  * Use a Recursive CTE to traverse the `parent_id` chain:
    ```sql
    WITH RECURSIVE thread AS (
      SELECT * FROM openai_logs WHERE id = ?
      UNION ALL
      SELECT o.* FROM openai_logs o
      INNER JOIN thread t ON t.parent_id = o.id
    )
    SELECT * FROM thread ORDER BY id ASC;
    ```
  * Note: The CTE naturally returns the root first if ordered by ID ASC, making chronologic rendering easy.

**File:** `internal/management/dto.go`
* Add `ThreadMessage` struct:
  ```go
  type ThreadMessage struct {
      LogID     int64  `json:"log_id"`
      Role      string `json:"role"`
      Content   string `json:"content"`
      Timestamp int64  `json:"timestamp"`
      IsComplex bool   `json:"is_complex"` // True if tool-calls / image arrays are present
  }
  ```
* Add `ThreadResponse` struct containing `[]ThreadMessage`.

**File:** `internal/management/api.go` & `server.go`
* Register `GET /api/logs/{id}/thread`.
* In the handler:
  1. Call `reader.GetThread(id)`.
  2. For the *first* (root) `LogEntry`, parse the full `ReqBody` `messages` array and append them all to the `ThreadResponse`.
  3. For the root, and *every subsequent* `LogEntry` in the slice, append the *Assistant's* assembled response (using `streamview.Build(entry).AssembledBody`).
  4. For *every subsequent* `LogEntry` in the slice, diff its `ReqBody.messages` against the previous entry's `ReqBody.messages` to find the *new* User message, and append it.
  5. If any message contains tool calls or non-string content, set `IsComplex = true` and `Content = "[Complex turn: View in Raw Mode]"`.
  6. Return the linear array of `ThreadMessage`s.

---

## 6. Frontend Implementation: Emacs, Web, and TUI

### 6.1 Emacs Client (`emacs/`)
* **`memoryelaine-http.el`**: No changes needed (already handles async JSON).
* **`memoryelaine-show.el`**: 
  * Add keybinding `c` to trigger `memoryelaine-show-thread`.
* **`memoryelaine-thread.el`** (New File):
  * Define `memoryelaine-thread-mode` (derived from `special-mode`).
  * Function `memoryelaine-thread-open(id)`: Fetches `/api/logs/{id}/thread`.
  * Render the JSON response into a highly readable, propertized buffer:
    ```text
    ==================================================
    [User] (Log #42 - 10:15:00)
    Can you write a python script to...
    
    ==================================================
    [Assistant] (Log #42 - 10:15:02)
    Certainly! Here is the script...
    ```
  * Use distinct font-lock faces for `[User]` vs `[Assistant]`.
  * Make the `(Log #N)` text a clickable button (or mapped to `RET`) that calls `memoryelaine-show-entry N`, allowing users to jump from the conversation view into the raw network bytes of that specific turn.

### 6.2 Web UI (`internal/web/static/`)
* **`index.html`**: Add a `<button id="view-thread-btn" class="hidden">View Conversation</button>` to the detail overlay. Add a `<div id="thread-container" class="hidden"></div>`.
* **`style.css`**: Add styles for chat bubbles (`.chat-bubble.user` right-aligned with blue background; `.chat-bubble.assistant` left-aligned with gray background).
* **`app.js`**: 
  * If the entry is `/v1/chat/completions`, unhide the `view-thread-btn`.
  * On click, fetch `/api/logs/{id}/thread`.
  * Hide the raw request/response panes, unhide `thread-container`, and render the `ThreadMessage` array as standard chat UI bubbles. Add a hyperlink on each bubble pointing back to the raw view for that specific `log_id`.

### 6.3 Terminal UI (`internal/tui/`)
* **`model.go`**:
  * Add `modeThread` to `viewMode`.
  * In `handleKey`, if `c` is pressed on a valid chat completion log, fire a `tea.Cmd` to fetch the thread.
  * Create `threadView() string`:
    * Use `lipgloss` to style User messages with a right-margin and specific border color.
    * Use `lipgloss` to style Assistant messages with a left-margin and alternating border color.
    * Print `[Complex turn: View in Raw Mode]` in red/dim text when `IsComplex` is true.
  * Map `esc`/`q` to return from `modeThread` to the previous mode (`modeTable` or `modeDetail`).
