# IMPLEMENTATION-PLAN-CHAT-SPECIALIZATION-v3.md

## 1. Overview

This revision replaces the ambiguous parts of `17-IMPL-PLAN-CHAT-MODE-v2.md` and incorporates the decisions made during review:

1. Support both streamed and non-streamed `/v1/chat/completions`.
2. Keep FTS working for all endpoints, even if chat/completions gets the richest extraction.
3. Render conversation view as `root -> selected entry`, not full descendant traversal.
4. Preserve visibility of `system` and `developer` messages in conversation view.
5. Ignore backward compatibility for existing DB contents; rollout will use a fresh DB.

The central idea remains correct:

- Preserve exact raw request/response bytes for debugging.
- Add derived sidecar text for search and conversation rendering.
- Link chat requests by hashing canonicalized message prefixes.
- Expose a specialized conversation view in Emacs, Web, and TUI.

## 2. Goals

### 2.1 Primary Goals

1. Fix FTS for streamed chat/completions responses by indexing assembled text instead of raw SSE fragments.
2. Improve readability of `/v1/chat/completions` traffic with a conversation-oriented view.
3. Preserve raw-mode debugging with no loss of fidelity.

### 2.2 Secondary Goals

1. Avoid regressing search for non-chat endpoints.
2. Make thread reconstruction deterministic rather than heuristic.
3. Keep the MVP constrained to linear `root -> selected` conversation rendering.

### 2.3 Non-Goals

1. No storage deduplication.
2. No descendant traversal or branch explorer in the first version.
3. No backward-compatible migration path for existing SQLite files.

## 3. Key Decisions

### 3.1 Canonical Storage + Sidecar Text

`req_body` and `resp_body` remain the source of truth. New sidecar columns will hold derived searchable text:

- `req_text`
- `resp_text`

Important constraint: these columns must be populated for all endpoints, not just chat/completions, otherwise global FTS regresses.

Rule:

1. Default `req_text = req_body`.
2. Default `resp_text = resp_body`.
3. For supported endpoints, overwrite the defaults with extracted plain text.

This gives us:

- Better FTS for supported streaming endpoints.
- No search regression for unsupported endpoints.
- Clear separation between raw bytes and derived text.

### 3.2 Deterministic Lineage via Canonical Message Hashes

For `/v1/chat/completions`, lineage will be tracked with:

- `chat_hash`
- `parent_id`
- `parent_prefix_len`
- `message_count`

`parent_prefix_len` is the critical addition missing in v2. It records how many request messages matched the parent request at write time. This removes the need for a vague read-time diff algorithm.

### 3.3 Hash Algorithm Choice

We do not need a cryptographic hash for secrecy. We do want strong collision resistance because the hash acts as a lineage key.

Decision: use `SHA-256` from the Go standard library.

Rationale:

1. The cost is negligible at the expected message volumes.
2. It avoids avoidable collision risk from weaker hashes like `FNV-1a`.
3. It keeps the implementation simple and uncontroversial.

If profiling later proves hashing is a bottleneck, we can revisit this. It is not a sensible optimization target for the MVP.

### 3.4 Conversation View Scope

Conversation view will support only `/v1/chat/completions` in v3.

Within that scope:

1. `system`, `developer`, `user`, and `assistant` messages are all visible.
2. Plain-text messages render directly.
3. Structured or unsupported content renders as a compact placeholder plus a raw-mode jump target.

### 3.5 Root-To-Selected Only

`GET /api/logs/{id}/thread` returns the linear chain from the root to the selected log entry.

It will not attempt descendant traversal in v3. That avoids branch ambiguity and keeps semantics stable.

The response should include enough metadata to indicate where the selected entry sits in the visible branch, for example:

- `selected_log_id`
- `selected_turn_index`
- `total_turns_in_view`

This is "7 of 7 in the current branch view", not "7 of 14 including unseen descendants".

## 4. Database Changes

**File:** `internal/database/db.go`

Update `openai_logs`:

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
```

Add indexes:

```sql
CREATE INDEX IF NOT EXISTS idx_chat_hash ON openai_logs(chat_hash);
CREATE INDEX IF NOT EXISTS idx_parent_id ON openai_logs(parent_id);
```

Update FTS schema to index sidecar text:

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS openai_logs_fts USING fts5(
    req_text,
    resp_text,
    content='openai_logs',
    content_rowid='id'
);
```

Triggers:

```sql
CREATE TRIGGER IF NOT EXISTS openai_logs_ai AFTER INSERT ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(rowid, req_text, resp_text)
    VALUES (new.id, new.req_text, new.resp_text);
END;

CREATE TRIGGER IF NOT EXISTS openai_logs_ad AFTER DELETE ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(openai_logs_fts, rowid, req_text, resp_text)
    VALUES ('delete', old.id, old.req_text, old.resp_text);
END;
```

No `AFTER UPDATE` trigger is required for the MVP because rows are insert-only after capture. If we later add reprocessing/backfill, add an update trigger then.

**File:** `internal/database/models.go`

Add fields to `LogEntry`:

```go
ParentID        *int64  `json:"parent_id" db:"parent_id"`
ChatHash        *string `json:"chat_hash" db:"chat_hash"`
ParentPrefixLen *int    `json:"parent_prefix_len" db:"parent_prefix_len"`
MessageCount    *int    `json:"message_count" db:"message_count"`
ReqText         *string `json:"req_text" db:"req_text"`
RespText        *string `json:"resp_text" db:"resp_text"`
```

## 5. Rollout Constraint

Backward compatibility is explicitly out of scope, but there is one operational consequence:

- `CREATE TABLE IF NOT EXISTS` will not upgrade an old DB in place.

Therefore rollout for this feature requires deleting the old SQLite file before starting the new build, or otherwise guaranteeing a fresh DB path.

This is acceptable for v3 and should be stated plainly in deployment notes.

## 6. Chat Extraction Package

**New file:** `internal/chat/extract.go`

Create one package responsible for canonical parsing, text extraction, and response extraction for `/v1/chat/completions`.

### 6.1 Request Structures

Define request-side types narrowly around what we need:

```go
type ChatCompletionRequest struct {
    Messages []Message `json:"messages"`
}

type Message struct {
    Role         string          `json:"role"`
    Content      json.RawMessage `json:"content"`
    ToolCalls    json.RawMessage `json:"tool_calls,omitempty"`
    FunctionCall json.RawMessage `json:"function_call,omitempty"`
    Name         *string         `json:"name,omitempty"`
}
```

`Content` must remain `json.RawMessage` because chat content may be:

- a string
- an array of typed content parts
- null

### 6.2 Response Structures

Support both response forms:

1. Streamed SSE via existing `streamview` support.
2. Non-stream JSON responses via direct parsing.

Add a helper such as:

```go
type AssistantExtraction struct {
    Text      string
    Available bool
    IsComplex bool
    Reason    string
}
```

and implement:

```go
func ExtractAssistantResponse(entry *database.LogEntry) AssistantExtraction
```

Behavior:

1. If the response is SSE and `streamview.Build` succeeds, use assembled text.
2. If the response is not SSE, parse JSON and extract `choices[0].message`.
3. If the response contains tool calls, non-text content, multi-choice output, or unsupported structure, mark it complex/unavailable with a reason.

This function is the canonical source for assistant text in both `resp_text` generation and thread rendering.

## 7. Canonical Hashing

**New file:** `internal/chat/hash.go`

### 7.1 Canonicalization Rule

Hash only the canonicalized request message sequence, not the entire request body.

The canonical form for each message is:

```go
type CanonicalMessage struct {
    Role         string   `json:"role"`
    ContentText  string   `json:"content_text,omitempty"`
    ContentParts []string `json:"content_parts,omitempty"`
    Name         string   `json:"name,omitempty"`
    IsComplex    bool     `json:"is_complex,omitempty"`
}
```

Rules:

1. Always include `role`.
2. If `content` is a plain string, store it as `ContentText`.
3. If `content` is a text-only array, flatten the text parts into `ContentParts`.
4. If the message is structurally complex, set `IsComplex=true` and preserve enough canonical signal to distinguish it.
5. Ignore request-level fields such as `model`, `temperature`, `tools`, and other top-level parameters.

This keeps lineage tied to conversational history, not incidental request configuration.

### 7.2 Hash Function

Implementation:

1. Convert `[]Message` to `[]CanonicalMessage`.
2. `json.Marshal` the canonical slice.
3. Compute `SHA-256`.
4. Store as lowercase hex.

### 7.3 Parent Lookup

In `internal/database/writer.go`, when inserting a `/v1/chat/completions` request:

1. Parse request messages.
2. Compute `message_count`.
3. Compute full `chat_hash` for the complete message slice.
4. Try to find the parent by hashing prefixes from most likely to least likely.

Order:

1. First try `N-2` messages, which is the expected case for a normal turn.
2. Then try remaining prefixes from `N-1` down to `1`, skipping `N-2`.
3. On first match, store:
   - `parent_id`
   - `parent_prefix_len`
4. If no match exists, leave `parent_id` and `parent_prefix_len` null.

The parent query remains:

```sql
SELECT id FROM openai_logs WHERE chat_hash = ? ORDER BY id DESC LIMIT 1
```

The "latest match wins" policy is acceptable for v3 and should be documented as the branch-selection rule.

## 8. Sidecar Text Generation

**File:** `internal/database/writer.go`

Before insert, populate sidecar fields.

### 8.1 Default Behavior For All Endpoints

Set:

```go
entry.ReqText = ptr(entry.ReqBody)
entry.RespText = entry.RespBody
```

This preserves existing search behavior for endpoints we do not understand deeply.

### 8.2 Chat/Completions Request Text

If `RequestPath == "/v1/chat/completions"` and request parsing succeeds:

1. Extract searchable text from all messages.
2. Include text from `system` and `developer` messages.
3. For text-only content arrays, flatten the text.
4. For complex content, insert a small marker token such as `[complex-content]` rather than dropping the message silently.

### 8.3 Chat/Completions Response Text

For `/v1/chat/completions`, use `ExtractAssistantResponse`:

1. If assistant text is available, set `resp_text` to that plain text.
2. If the response is complex or unsupported, keep the default raw fallback in `resp_text`.

This means:

- FTS is improved for supported streamed and non-streamed chat responses.
- Unsupported responses remain searchable through their raw JSON.

### 8.4 Completions Endpoint

For `/v1/completions`, it is reasonable to also keep using `streamview.Build` to improve streamed response search because the support already exists.

If implementation complexity stays low, do it in the same pass. If not, the minimum acceptable v3 scope is:

1. Fully improved chat/completions extraction.
2. Raw fallback preserved for all other endpoints.

## 9. Writer Changes

**File:** `internal/database/writer.go`

Update `insertSQL` and `insert(entry LogEntry)` to write the new columns.

Insert order should now include:

```sql
parent_id, chat_hash, parent_prefix_len, message_count, req_text, resp_text
```

Error handling policy:

1. Parsing failure must not block insertion.
2. On parse failure, log at `WARN` or `DEBUG` and keep raw fallback text.
3. Parent lookup failure due to DB/query error should log and continue with null parent fields rather than dropping the log entry.

This keeps capture reliability above thread perfection.

## 10. Reader Changes

**File:** `internal/database/reader.go`

Add:

```go
func (r *LogReader) GetThreadToSelected(id int64) ([]LogEntry, error)
```

SQL:

```sql
WITH RECURSIVE thread AS (
  SELECT * FROM openai_logs WHERE id = ?
  UNION ALL
  SELECT o.* FROM openai_logs o
  INNER JOIN thread t ON t.parent_id = o.id
)
SELECT * FROM thread ORDER BY id ASC;
```

This is explicitly an ancestor-chain query. It does not attempt descendant traversal.

Do not describe it as "naturally returns the root first". It traverses upward from the selected node and is then ordered chronologically.

## 11. Management API

**Files:** `internal/management/dto.go`, `internal/management/api.go`, `internal/management/server.go`

### 11.1 Endpoint

Add:

```text
GET /api/logs/{id}/thread
```

Dispatch this from the existing `/api/logs/` sub-handler.

### 11.2 Response Shape

Suggested DTOs:

```go
type ThreadMessage struct {
    LogID      int64  `json:"log_id"`
    Role       string `json:"role"`
    Content    string `json:"content"`
    Timestamp  int64  `json:"timestamp"`
    IsComplex  bool   `json:"is_complex"`
    RawOnly    bool   `json:"raw_only"`
    Complexity string `json:"complexity,omitempty"`
}

type ThreadResponse struct {
    SelectedLogID     int64           `json:"selected_log_id"`
    SelectedTurnIndex int             `json:"selected_turn_index"`
    TotalTurnsInView  int             `json:"total_turns_in_view"`
    Messages          []ThreadMessage `json:"messages"`
}
```

### 11.3 Assembly Algorithm

Do not diff raw message arrays at read time.

Instead:

1. Load `root -> selected`.
2. For the root entry:
   - Parse all request messages.
   - Append them in order.
   - Append the assistant response for the root entry if extractable.
3. For each later entry:
   - Parse its request messages.
   - Use `parent_prefix_len` to slice only the messages newly introduced by that request.
   - Append those new request messages in order.
   - Append the assistant response for that log entry if extractable.

This is deterministic and matches the lineage decision made at write time.

### 11.4 Rendering Rules

Conversation view should expose all roles:

1. `user` and `assistant` render as normal chat turns.
2. `system` and `developer` render as visible but visually subdued blocks.
3. Complex request or response content yields a placeholder such as:
   `[Complex message: view raw log #123]`

Do not hide complex turns completely.

### 11.5 Error Handling

1. Invalid ID: `400`
2. Missing log: `404`
3. Internal parse/query failure: `500`

If an individual message or response cannot be parsed but the log row exists, prefer partial thread output over failing the entire endpoint.

## 12. Frontend Changes

### 12.1 Emacs

**Files:** `emacs/memoryelaine-show.el`, `emacs/memoryelaine-thread.el` (new)

1. Add `c` in show mode to open conversation view for chat/completions logs.
2. Reuse existing async HTTP helpers.
3. Render roles distinctly:
   - `user`
   - `assistant`
   - `system`
   - `developer`
4. Keep `(Log #N)` clickable back to raw detail view.
5. Show a header like `Conversation to Log #42 (turn 7 of 7)`.

### 12.2 Web

**Files:** `internal/web/static/index.html`, `internal/web/static/app.js`, `internal/web/static/style.css`

1. Add a `View Conversation` button only for `/v1/chat/completions`.
2. Fetch `/api/logs/{id}/thread`.
3. Render conversation blocks inside the detail overlay.
4. Keep a clear switch back to raw request/response panes.
5. Style `system` and `developer` as inset note blocks rather than chat bubbles.

### 12.3 TUI

**File:** `internal/tui/model.go`

1. Add `modeThread`.
2. On `c`, fetch thread data for the selected chat-completion entry.
3. Render `user` and `assistant` as chat-style blocks.
4. Render `system` and `developer` as full-width dimmed panels.
5. Show the selected position in the title.

## 13. Testing

Add explicit tests. This was missing from v2.

### 13.1 Unit Tests

1. `internal/chat/hash_test.go`
   - canonical hash determinism
   - string content vs text-array content
   - complex marker behavior
2. `internal/chat/extract_test.go`
   - request text extraction
   - non-stream assistant extraction
   - complex response detection
3. `internal/database/writer_test.go`
   - default sidecar text fallback for non-chat endpoints
   - parent lookup and `parent_prefix_len`

### 13.2 Reader/API Tests

1. `internal/database/reader_test.go`
   - `GetThreadToSelected`
2. `internal/management/server_test.go`
   - `GET /api/logs/{id}/thread`
   - partial thread output on mixed simple/complex messages

### 13.3 Integration Tests

Add an end-to-end conversation fixture that covers:

1. root request with system + user
2. streamed assistant response
3. subsequent user turn
4. non-stream assistant response
5. complex tool-call turn

Assertions:

1. raw bytes preserved
2. sidecar text populated
3. FTS matches assembled streamed text
4. thread endpoint returns root->selected in correct order

## 14. Observability

Add lightweight logging around lineage and extraction:

1. request parse failure
2. response parse failure
3. parent lookup success/failure
4. stored `parent_prefix_len`

This does not require new metrics for v3, but debug logs should make lineage failures explainable.

## 15. Implementation Order

1. Schema and model changes.
2. Chat extraction helpers for request and non-stream response parsing.
3. Writer-side sidecar generation and lineage storage.
4. Reader thread query.
5. Thread API and DTOs.
6. Emacs, Web, and TUI conversation views.
7. Tests.

## 16. Acceptance Criteria

The feature is complete when all of the following are true:

1. Searching for words from a streamed `/v1/chat/completions` assistant response succeeds via FTS.
2. Searching non-chat endpoints still works at least as well as before.
3. A non-stream `/v1/chat/completions` response appears correctly in conversation view.
4. `system` and `developer` messages are visible in conversation view.
5. Conversation view for a selected log shows the root-to-selected chain in order.
6. Complex messages are not silently dropped; they are represented with placeholders and raw-log links.
7. Raw request/response bodies remain unchanged and accessible.
