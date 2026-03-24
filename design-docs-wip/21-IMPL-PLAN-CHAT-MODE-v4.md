# IMPLEMENTATION-PLAN-CHAT-SPECIALIZATION-v4.md

## 1. Overview

This revision replaces `19-IMPL-PLAN-CHAT-MODE-v3.md`. It incorporates the
v3 critique findings and additional review feedback:

1. Fix the double-echo assembly bug by adopting Top-Down Annotation.
2. Use `COALESCE`-based FTS indexing to avoid storage duplication.
3. Add explicit truncation guards.
4. Cap parent prefix search depth.
5. Specify thread endpoint behavior for non-chat and orphan entries.
6. Write tests alongside each layer, not as a trailing step.

All v3 decisions not explicitly revised here remain in effect:

- Preserve exact raw request/response bytes for debugging.
- Add derived sidecar text for search and conversation rendering.
- Link chat requests by hashing canonicalized message prefixes.
- Expose a specialized conversation view in Emacs, Web, and TUI.
- Support both streamed and non-streamed `/v1/chat/completions`.
- Keep FTS working for all endpoints.
- Render conversation view as `root -> selected entry`, not full descendant traversal.
- Preserve visibility of `system` and `developer` messages in conversation view.
- Ignore backward compatibility for existing DB contents; rollout will use a fresh DB.

## 2. Goals

### 2.1 Primary Goals

1. Fix FTS for streamed chat/completions responses by indexing assembled text
   instead of raw SSE fragments.
2. Improve readability of `/v1/chat/completions` traffic with a
   conversation-oriented view.
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

`req_body` and `resp_body` remain the source of truth. Two new nullable sidecar
columns hold derived searchable text:

- `req_text` — populated only for endpoints where extracted text differs from
  `req_body` (i.e., `/v1/chat/completions`). `NULL` otherwise.
- `resp_text` — populated only when extracted text differs from `resp_body`
  (streamed or non-streamed chat responses with extractable text). `NULL`
  otherwise.

FTS indexes use `COALESCE(req_text, req_body)` and
`COALESCE(resp_text, resp_body)`, so all endpoints remain searchable without
duplicating raw bodies into the sidecar columns.

This gives us:

- Better FTS for supported streaming endpoints.
- No search regression for unsupported endpoints.
- No storage duplication for non-chat traffic.
- Clear separation between raw bytes and derived text.

### 3.2 Deterministic Lineage via Canonical Message Hashes

For `/v1/chat/completions`, lineage will be tracked with:

- `chat_hash`
- `parent_id`
- `parent_prefix_len`
- `message_count`

`parent_prefix_len` records how many request messages matched the parent request
at write time. This removes the need for a read-time diff algorithm.

`message_count` records the total number of messages in this request. Together
with `parent_prefix_len`, it lets the thread assembly algorithm attribute
message index ranges to specific log entries when annotating the conversation.

### 3.3 Hash Algorithm Choice

We do not need a cryptographic hash for secrecy. We do want strong collision
resistance because the hash acts as a lineage key.

Decision: use `SHA-256` from the Go standard library.

Rationale:

1. The cost is negligible at the expected message volumes.
2. It avoids avoidable collision risk from weaker hashes like `FNV-1a`.
3. It keeps the implementation simple and uncontroversial.

If profiling later proves hashing is a bottleneck, we can revisit this. It is
not a sensible optimization target for the MVP.

### 3.4 Conversation View Scope

Conversation view will support only `/v1/chat/completions` in v4.

Within that scope:

1. `system`, `developer`, `user`, and `assistant` messages are all visible.
2. Plain-text messages render directly.
3. Structured or unsupported content renders as a compact placeholder plus a
   raw-mode jump target.

### 3.5 Root-To-Selected Only

`GET /api/logs/{id}/thread` returns the linear chain from the root to the
selected log entry.

It will not attempt descendant traversal in v4. That avoids branch ambiguity
and keeps semantics stable.

The response should include enough metadata to indicate where the selected
entry sits in the visible branch:

- `selected_log_id`
- `selected_turn_index`
- `total_turns_in_view`

**Definition of "turn":** A turn is one log entry in the ancestor chain — i.e.,
one request/response round-trip to the upstream API. `selected_turn_index` and
`total_turns_in_view` count log entries, not individual messages. A root entry
containing `[system, user]` plus its assistant response is turn 0 (one turn),
regardless of how many messages it contains. This keeps the count stable and
unambiguous: it always equals the number of entries in the CTE chain.

The `Messages` array in the response is a flat list of all individual messages
across all turns. The `LogID` field on each `ThreadMessage` links each message
back to its originating turn, so frontends can group or separate messages per
turn as needed for display.

This is "turn 6 of 7 in the current branch view", not "7 of 14 including
unseen descendants" or "message 12 of 25".

### 3.6 Truncation Guard

If `req_truncated == true`:

- Do not attempt to parse JSON, hash messages, or track lineage.
- Leave `req_text` as `NULL` (FTS falls back to raw `req_body` via `COALESCE`).
- Leave `parent_id`, `chat_hash`, `parent_prefix_len`, and `message_count` as
  `NULL`.
- The entry is treated as a standalone raw log.

If `resp_truncated == true`:

- Do not attempt to parse or extract the response.
- Leave `resp_text` as `NULL` (FTS falls back to raw `resp_body` via
  `COALESCE`).

Frontends should not show a "View Conversation" button for truncated entries.

### 3.7 Known Limitations (Acceptable for MVP)

**Identical Prefix Collision:** Two conversations starting with the exact same
messages will share a `chat_hash`. The `ORDER BY id DESC LIMIT 1` parent
lookup will link to the most recent match, which may cross conversation
boundaries. Because thread assembly uses Top-Down Annotation (§11.3), the
rendered conversation text is always correct — only the attributed `Log #N`
links on early messages may point to the wrong raw log. This is acceptable
for v4 and should be documented.

**Async Queue Ordering:** The writer uses a bounded channel consumed by a
single goroutine. Entries enqueue in completion order, not request order. If
Turn 2 finishes before Turn 1, it writes first and will not find Turn 1 as
its parent. The conversation UI still renders correctly from the request body;
only the lineage linkage is incomplete. This is an acceptable consequence of
prioritizing zero-latency proxying over strict serial database insertion.

**Client History Rewriting:** If a client summarizes or modifies past messages
between turns, the prefix hash will not match any existing entry. `parent_id`
will be `NULL` and the chain breaks. The conversation view will render the
current request's messages correctly as a standalone conversation. This is the
correct degraded behavior.

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

Update FTS schema to use `COALESCE` for indexing:

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS openai_logs_fts USING fts5(
    req_text,
    resp_text,
    content='openai_logs',
    content_rowid='id'
);
```

Triggers use `COALESCE` so that `NULL` sidecar columns fall back to raw bodies:

```sql
CREATE TRIGGER IF NOT EXISTS openai_logs_ai AFTER INSERT ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(rowid, req_text, resp_text)
    VALUES (new.id, COALESCE(new.req_text, new.req_body), COALESCE(new.resp_text, new.resp_body));
END;

CREATE TRIGGER IF NOT EXISTS openai_logs_ad AFTER DELETE ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(openai_logs_fts, rowid, req_text, resp_text)
    VALUES ('delete', old.id, COALESCE(old.req_text, old.req_body), COALESCE(old.resp_text, old.resp_body));
END;
```

No `AFTER UPDATE` trigger is required for the MVP because rows are insert-only
after capture. If we later add reprocessing/backfill, add an update trigger
then.

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

Backward compatibility is explicitly out of scope, but there is one
operational consequence:

- `CREATE TABLE IF NOT EXISTS` will not upgrade an old DB in place.

Therefore rollout for this feature requires deleting the old SQLite file before
starting the new build, or otherwise guaranteeing a fresh DB path.

This is acceptable for v4 and should be stated plainly in deployment notes.

## 6. Chat Extraction Package

**New file:** `internal/chat/extract.go`

Create one package responsible for canonical parsing, text extraction, and
response extraction for `/v1/chat/completions`.

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

1. If `resp_truncated == true`, return unavailable with reason.
2. If the response is SSE and `streamview.Build` succeeds, use assembled text.
3. If the response is not SSE, parse JSON and extract `choices[0].message`.
4. If the response contains tool calls, non-text content, multi-choice output,
   or unsupported structure, mark it complex/unavailable with a reason.

This function is the canonical source for assistant text in both `resp_text`
generation and thread rendering.

## 7. Canonical Hashing

**New file:** `internal/chat/hash.go`

### 7.1 Canonicalization Rule

Hash only the canonicalized request message sequence, not the entire request
body.

The canonical form for each message is:

```go
// WARNING: Field order is load-bearing for hash determinism.
// Do not reorder, rename, or remove fields without understanding
// that all existing chat_hash values will be invalidated.
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
3. If `content` is a text-only array, flatten the text parts into
   `ContentParts`.
4. If the message is structurally complex, set `IsComplex=true` and preserve
   enough canonical signal to distinguish it.
5. Ignore request-level fields such as `model`, `temperature`, `tools`, and
   other top-level parameters.

This keeps lineage tied to conversational history, not incidental request
configuration.

### 7.2 Hash Function

Implementation:

1. Convert `[]Message` to `[]CanonicalMessage`.
2. `json.Marshal` the canonical slice.
3. Compute `SHA-256`.
4. Store as lowercase hex.

### 7.3 Parent Lookup

In `internal/database/writer.go`, when inserting a `/v1/chat/completions`
request:

1. If `req_truncated == true`, skip all parsing, hashing, and lineage. Leave
   chat fields `NULL`.
2. Parse request messages.
3. Compute `message_count = len(messages)`.
4. Compute full `chat_hash` for the complete message slice.
5. Try to find the parent by hashing prefixes, with a capped search depth.

**Prefix search order (capped at 5 attempts):**

1. First try `N-2` messages (the expected case for a normal turn: the previous
   request messages without the new user message and assistant echo).
2. Then try remaining prefixes from `N-1` down to `1`, skipping `N-2`, stopping
   after 5 total attempts.
3. On first match, store:
   - `parent_id`
   - `parent_prefix_len`
4. If no match exists after the capped attempts, leave `parent_id` and
   `parent_prefix_len` null.

The parent query remains:

```sql
SELECT id FROM openai_logs WHERE chat_hash = ? ORDER BY id DESC LIMIT 1
```

The "latest match wins" policy is acceptable for v4 and should be documented
as the branch-selection rule (see §3.7 for known limitations).

**Rationale for the cap:** For a 50-message conversation with no parent (e.g.,
client rewrote history), uncapped search means ~49 hash computations and DB
queries. Since a miss at the first few prefixes almost certainly means no match
exists, capping at 5 attempts bounds the cost without meaningfully reducing
match quality.

## 8. Sidecar Text Generation

**File:** `internal/database/writer.go`

Before insert, populate sidecar fields for supported endpoints only.

### 8.1 Default Behavior For All Endpoints

Leave `req_text` and `resp_text` as `NULL`.

The `COALESCE`-based FTS trigger (§4) ensures that `NULL` sidecar columns fall
back to the raw body for indexing. This avoids duplicating raw bodies into
sidecar columns for endpoints we do not understand deeply.

### 8.2 Chat/Completions Request Text

If `RequestPath == "/v1/chat/completions"` and `req_truncated == false` and
request parsing succeeds:

1. Extract searchable text from all messages.
2. Include text from `system` and `developer` messages.
3. For text-only content arrays, flatten the text.
4. For complex content, insert a small marker token such as
   `[complex-content]` rather than dropping the message silently.
5. Set `req_text` to the extracted text.

If parsing fails, leave `req_text` as `NULL` (falls back to raw body in FTS).

### 8.3 Chat/Completions Response Text

For `/v1/chat/completions`, use `ExtractAssistantResponse`:

1. If assistant text is available, set `resp_text` to that plain text.
2. If the response is complex, unsupported, or truncated, leave `resp_text` as
   `NULL` (falls back to raw body in FTS).

This means:

- FTS is improved for supported streamed and non-streamed chat responses.
- Unsupported responses remain searchable through their raw JSON via `COALESCE`.

### 8.4 Completions Endpoint

For `/v1/completions`, it is reasonable to also use `streamview.Build` to
improve streamed response search because the support already exists.

If implementation complexity stays low, do it in the same pass. If not, the
minimum acceptable v4 scope is:

1. Fully improved chat/completions extraction.
2. `NULL` sidecar (raw fallback) preserved for all other endpoints.

## 9. Writer Changes

**File:** `internal/database/writer.go`

Update `insertSQL` and `insert(entry LogEntry)` to write the new columns.

Insert order should now include:

```sql
parent_id, chat_hash, parent_prefix_len, message_count, req_text, resp_text
```

Error handling policy:

1. Parsing failure must not block insertion.
2. On parse failure, log at `WARN` or `DEBUG` and keep sidecar columns `NULL`
   (FTS falls back to raw body).
3. Parent lookup failure due to DB/query error should log and continue with
   null parent fields rather than dropping the log entry.

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

This is explicitly an ancestor-chain query. It traverses upward from the
selected node and is then ordered chronologically.

## 11. Management API

**Files:** `internal/management/dto.go`, `internal/management/api.go`,
`internal/management/server.go`

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

### 11.3 Assembly Algorithm — Top-Down Annotation

**This section replaces the v3 assembly algorithm to fix the double-echo bug.**

The v3 algorithm stitched the conversation by appending each entry's request
messages (sliced by `parent_prefix_len`) and then its response. This caused
assistant messages to appear twice: once from the parent's response and once
echoed in the child's request body.

The fix: a chat completion request *already contains the entire conversation
history*. We do not need to stitch it together from past requests. We only need
the lineage chain to attribute historical messages to their originating log
entries.

**Revised algorithm:**

1. Fetch the ancestor chain via the CTE (`root -> selected`).
2. Parse the **selected (final) entry's** `req_body.messages`. This is the
   canonical conversation history up to the current turn.
3. Walk the CTE entries from root to selected. Use each entry's
   `parent_prefix_len` and `message_count` to determine which index range in
   the message array that entry introduced. Annotate those messages with the
   entry's `log_id` and `timestamp`.
4. Append the **selected (final) entry's** extracted assistant response (from
   `ExtractAssistantResponse`) as the last message in the thread.

**Index attribution logic:**

Given the ordered chain `[E0, E1, ..., En]` where `En` is the selected entry:

- `E0` (root): messages `[0, E0.message_count)` are attributed to `E0`.
- `Ek` (k > 0): messages `[Ek.parent_prefix_len, Ek.message_count)` are
  attributed to `Ek`. These are the new messages introduced by that turn.
- The final assistant response is attributed to `En`.

If attribution gaps or overlaps occur (e.g., due to client history
modification), attribute unmatched messages to the selected entry as a
fallback. The conversation text is always correct because it comes from the
selected entry's request body.

**Benefits:**

- Eliminates double-echo completely.
- Gracefully handles client history rewriting (summarization, message editing).
- The rendered conversation is always faithful to what the LLM actually saw.
- Lineage metadata is used only for attribution, not for content.

### 11.4 Thread Endpoint Behavior for Edge Cases

**Non-chat entries:** If the requested log entry's `request_path` is not
`/v1/chat/completions`, return HTTP `400` with error message:
`"thread view is only available for /v1/chat/completions requests"`.

**Orphan chat entries (no parent):** Return a valid `ThreadResponse` containing
just the messages from that single entry's request body plus its assistant
response. `selected_turn_index = 0`, `total_turns_in_view = 1`. This is a
valid 1-turn conversation.

**Truncated entries:** If `req_truncated == true`, return HTTP `400` with error
message: `"thread view is not available for truncated requests"`.

### 11.5 Rendering Rules

Conversation view should expose all roles:

1. `user` and `assistant` render as normal chat turns.
2. `system` and `developer` render as visible but visually subdued blocks.
3. Complex request or response content yields a placeholder such as:
   `[Complex message: view raw log #123]`

Do not hide complex turns completely.

### 11.6 Error Handling

1. Invalid ID: `400`
2. Missing log: `404`
3. Non-chat or truncated log: `400`
4. Internal parse/query failure: `500`

If an individual message or response cannot be parsed but the log row exists,
prefer partial thread output over failing the entire endpoint.

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
6. Do not show the `c` binding or "View Conversation" for non-chat or truncated
   entries.

### 12.2 Web

**Files:** `internal/web/static/index.html`, `internal/web/static/app.js`,
`internal/web/static/style.css`

1. Add a `View Conversation` button only for `/v1/chat/completions` entries
   that are not truncated.
2. Fetch `/api/logs/{id}/thread`.
3. Render conversation blocks inside the detail overlay.
4. Keep a clear switch back to raw request/response panes.
5. Style `system` and `developer` as inset note blocks rather than chat
   bubbles.

### 12.3 TUI

**File:** `internal/tui/model.go`

1. Add `modeThread`.
2. On `c`, fetch thread data for the selected chat-completion entry.
3. Render `user` and `assistant` as chat-style blocks.
4. Render `system` and `developer` as full-width dimmed panels.
5. Show the selected position in the title.
6. Do not offer `c` for non-chat or truncated entries.

## 13. Testing

Tests are written alongside each implementation layer, not deferred to the end.

### 13.1 Unit Tests (written with §6 and §7)

1. `internal/chat/hash_test.go`
   - canonical hash determinism
   - string content vs text-array content
   - complex marker behavior
   - field ordering stability (regression guard)
2. `internal/chat/extract_test.go`
   - request text extraction
   - non-stream assistant extraction
   - complex response detection
   - truncated entry guard (returns unavailable)

### 13.2 Writer Tests (written with §8 and §9)

1. `internal/database/writer_test.go`
   - `NULL` sidecar for non-chat endpoints (no duplication)
   - populated sidecar for chat/completions
   - parent lookup and `parent_prefix_len`
   - prefix search cap (stops after 5 attempts)
   - truncated request skips all chat processing

### 13.3 Reader/API Tests (written with §10 and §11)

1. `internal/database/reader_test.go`
   - `GetThreadToSelected` — multi-turn chain
   - `GetThreadToSelected` — orphan root (1-turn)
2. `internal/management/server_test.go`
   - `GET /api/logs/{id}/thread` — normal conversation
   - `GET /api/logs/{id}/thread` — non-chat entry returns 400
   - `GET /api/logs/{id}/thread` — truncated entry returns 400
   - partial thread output on mixed simple/complex messages

### 13.4 Integration Tests (written with §12)

Add an end-to-end conversation fixture that covers:

1. root request with system + user
2. streamed assistant response
3. subsequent user turn
4. non-stream assistant response
5. complex tool-call turn

Assertions:

1. raw bytes preserved
2. sidecar text populated only for chat endpoints (`NULL` for others)
3. FTS matches assembled streamed text
4. FTS still works for non-chat endpoints via `COALESCE` fallback
5. thread endpoint returns root->selected in correct order
6. no double-echo in assembled conversation

## 14. Observability

Add lightweight logging around lineage and extraction:

1. request parse failure
2. response parse failure
3. parent lookup success/failure
4. stored `parent_prefix_len`
5. prefix search cap reached (all 5 attempts exhausted)
6. truncation guard activated

This does not require new metrics for v4, but debug logs should make lineage
failures explainable.

## 15. Implementation Order

Each step includes its corresponding tests. Do not defer testing.

1. Schema and model changes.
2. Chat extraction helpers (request parsing, response extraction) + unit tests.
3. Canonical hashing (canonicalization, SHA-256, prefix search) + unit tests.
4. Writer-side sidecar generation and lineage storage + writer tests.
5. Reader thread query + reader tests.
6. Thread API endpoint, DTOs, and Top-Down Annotation assembly + API tests.
7. Emacs, Web, and TUI conversation views + integration tests.

## 16. Acceptance Criteria

The feature is complete when all of the following are true:

1. Searching for words from a streamed `/v1/chat/completions` assistant
   response succeeds via FTS.
2. Searching non-chat endpoints still works at least as well as before (via
   `COALESCE` fallback, no storage duplication).
3. A non-stream `/v1/chat/completions` response appears correctly in
   conversation view.
4. `system` and `developer` messages are visible in conversation view.
5. Conversation view for a selected log shows the root-to-selected chain in
   order, with no double-echo of assistant messages.
6. Complex messages are not silently dropped; they are represented with
   placeholders and raw-log links.
7. Raw request/response bodies remain unchanged and accessible.
8. Truncated entries are handled gracefully (no parse attempts, no conversation
   button, raw fallback for FTS).
9. Thread endpoint returns 400 for non-chat and truncated entries, and a valid
   1-turn thread for orphan roots.
