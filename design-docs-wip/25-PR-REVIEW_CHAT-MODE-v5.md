# PR Review: Chat Mode (commit `4b9c02a`)

## Overall Assessment

This is a substantial, well-structured implementation. The schema changes, FTS
rebuild fix, writer-side enrichment, thread endpoint, and frontend integrations
are all directionally aligned with the v5 plan. The commit also comes with
meaningful test coverage and the full project verification script passes.

That said, I do **not** consider the change ready to merge as-is. I found a
small number of correctness issues where the implementation diverges from the
approved design in user-visible ways, and the current tests do not cover those
cases.

## Verification Performed

I ran:

```bash
./scripts/build-test-lint-all.sh
```

Result:

- Go tests: passed
- Build: passed
- `gofmt`: passed
- `golangci-lint`: passed
- Emacs ERT tests: passed

The findings below are therefore **logic and behavior issues not caught by the
current test suite**, not broken-build issues.

## Findings

### 1. High: complex multimodal messages are still collapsed, which breaks both lineage hashing and conversation rendering

**Files:**

- `internal/chat/hash.go`
- `internal/chat/extract.go`
- `internal/management/api.go`

**Problem**

The commit fixes collisions for `tool_calls` / `function_call`, but it does not
preserve uniqueness for other complex message forms such as non-text content
arrays (`image_url`, audio parts, etc.).

Specifically:

1. `canonicalize()` only sets `ComplexHash` for `ToolCalls` or `FunctionCall`
   in `internal/chat/hash.go:21-34`.
2. `ExtractContentString()` drops all non-text content parts in
   `internal/chat/extract.go:70-85`.
3. `buildThreadMessages()` always renders request messages using
   `chat.ExtractContentString(m.Content)` in `internal/management/api.go:517-523`
   and never emits a placeholder when content is complex.

**Impact**

Two different multimodal messages with the same role and no text content will
hash identically, which can corrupt lineage matching. On top of that, the
conversation view silently renders those messages as blank content instead of
the placeholder behavior described in the plan.

**Why this matters**

This is not just a missing enhancement. It breaks one of the core design
requirements: complex messages must remain visible in conversation view and
must not collapse into ambiguous lineage hashes.

### 2. High: `/api/logs/{id}/thread` does not enforce the documented 400 behavior for non-chat or truncated entries

**Files:**

- `internal/management/api.go`
- `internal/management/server_test.go`

**Problem**

The plan says the thread endpoint should return `400` for:

1. non-chat entries
2. truncated requests

But `handleThread()` does not check either condition before parsing the request
body:

- `internal/management/api.go:442-452`

It immediately takes the last entry in the chain and calls
`chat.ParseMessages(selected.ReqBody)`.

For non-chat entries, this can succeed with zero messages because the parser
only unmarshals into a struct containing `messages` and does not validate the
path. That means the endpoint can return `200` with an empty or misleading
thread instead of the documented `400`.

For truncated entries, there is likewise no explicit guard in `handleThread()`.

**Impact**

The API contract described in the design is not actually implemented. UI gating
reduces exposure, but direct API consumers and future UI changes will hit
incorrect behavior.

**Test gap**

The new thread endpoint tests in `internal/management/server_test.go:1259-1438`
cover:

1. single-entry happy path
2. multi-turn happy path
3. not found
4. invalid ID

They do **not** cover non-chat or truncated thread requests, which is why this
regression slips through despite the green test suite.

### 3. Medium: the writer does not implement the approved request truncation guard

**File:**

- `internal/database/writer.go`

**Problem**

The design explicitly said truncated requests must skip parsing, hashing, and
lineage tracking. The implementation does not do that.

`enrichChat()` checks:

1. chat path
2. non-empty `ReqBody`

but it never checks `entry.ReqTruncated` before parsing and hashing:

- `internal/database/writer.go:166-209`

If a truncated request body happens to remain parseable JSON, the writer will
still derive `req_text`, `message_count`, `chat_hash`, and potentially parent
lineage, which is contrary to the plan.

**Impact**

This is a correctness issue in degraded-mode behavior. It is unlikely on many
captures because truncation often produces malformed JSON, but it is still a
real mismatch between design and implementation.

### 4. Medium: raw-log navigation from conversation view is incomplete in both Web and Emacs

**Files:**

- `internal/web/static/app.js`
- `emacs/memoryelaine-thread.el`

**Problem**

The plan consistently described `(Log #N)` as a way to jump from conversation
view back to the raw log entry. That is only partially implemented.

In the Web UI:

- `internal/web/static/app.js:290-300`

`Log #N` is rendered as plain text inside `roleEl.textContent`; there is no
click target back to raw mode.

In Emacs:

- `emacs/memoryelaine-thread.el:127-144`

`(Log #N)` is inserted as styled text, not a button, and there is no command
bound to it.

**Impact**

This weakens the main escape hatch for ambiguous/complex thread messages. It is
not as severe as the backend issues above, but it is a real product regression
against the intended UX.

## Positive Notes

Several parts of the commit are notably solid:

1. The FTS rebuild fix in `internal/database/db.go:148-176` correctly avoids
   the `rebuild`/`COALESCE` trap from earlier design iterations.
2. The prefix-length guard is present in `HashPrefix()` and `buildPrefixOrder()`
   (`internal/chat/hash.go:67-76`, `internal/database/writer.go:237-251`).
3. The backward-attribution algorithm in `internal/management/api.go:493-513`
   is materially better than the earlier stitching approach and avoids the
   double-echo bug for normal text conversations.
4. The test suite is substantially better than before, especially around writer
   enrichment and FTS behavior.

## Recommendation

I would address Findings 1-3 before merging:

1. Extend `ComplexHash` handling to non-text multimodal content parts, not just
   tool/function calls.
2. Teach `buildThreadMessages()` / thread DTO construction to emit explicit
   placeholders for complex messages rather than blank content.
3. Add the missing non-chat and truncation guards in `handleThread()`.
4. Add the missing `ReqTruncated` guard in `enrichChat()`.

Finding 4 is less severe, but I would still fix it in the same PR if the goal
is to claim the conversation-view UX is complete.

## Bottom Line

The commit is close, and the architecture is mostly sound. But there are still
backend contract gaps and complex-content regressions that are significant
enough to block approval in a thorough review.
