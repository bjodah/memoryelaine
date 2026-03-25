# PR Review: `feat/chat-specialization`

Date: 2026-03-25

## Scope

Reviewed the branch delta against `main`, ran the repository validation script, inspected the new chat-specialization storage/threading code, added regression tests for confirmed failures, patched the branch, and reran validation.

## Findings

### 1. Legacy DB migration was incomplete and could break upgraded installs

Severity: critical

The branch added `parent_id`, `chat_hash`, `parent_prefix_len`, `message_count`, `req_text`, and `resp_text` to `openai_logs`, plus a reshaped FTS table. But the migration path only used `CREATE TABLE IF NOT EXISTS` / `CREATE VIRTUAL TABLE IF NOT EXISTS`, which does not alter existing tables. On an existing `main` database this left the old schema in place, while new reader/writer code immediately selected and inserted the new columns.

Concrete failure modes:

- startup could fail while creating indexes on columns that had not been added yet
- inserts could fail because `insertSQL` referenced columns missing from legacy databases
- reads could fail because `GetByID` / `GetLatest` selected missing columns
- old FTS tables/triggers stayed on `req_body` / `resp_body`, so the new `req_text` / `resp_text` search path was not actually migrated

Fix applied:

- added explicit `ALTER TABLE ... ADD COLUMN` migration for all new `openai_logs` columns
- created chat-specific indexes only after the columns exist
- dropped/recreated legacy FTS triggers on startup
- detected legacy `openai_logs_fts` layouts and recreated the FTS table
- forced an FTS rebuild when the table is new or recreated during upgrade
- switched FTS clearing in rebuilds to the FTS5 `delete-all` command

Regression coverage:

- `TestMigrate_LegacySchemaUpgradesColumnsAndFTS`

### 2. Parent lineage detection could fail on longer chat turns

Severity: high

`LogWriter.enrichChat()` only tried the first 5 candidate prefix lengths when looking for a parent conversation. That works for short turns, but fails for legitimate requests that add more than 5 messages in one turn, especially tool-heavy chats with assistant/tool/assistant/tool sequences. In those cases `parent_id` stayed unset even though the correct parent existed.

Impact:

- broken conversation chains in storage
- incomplete `/api/logs/:id/thread` reconstruction
- incorrect conversation view in the TUI/web UI

Fix applied:

- removed the 5-attempt cap and now search all valid prefix lengths in priority order

Regression coverage:

- `TestWriterChatLineage_LongTurnFallsBackBeyondFivePrefixes`

### 3. Thread reconstruction could drop streamed assistant replies for historical rows

Severity: medium

The thread endpoint only surfaced the selected entry’s assistant response when:

- `resp_text` had already been enriched and stored, or
- the raw response body was a non-streaming JSON response

That misses older streamed chat rows collected before this branch, because those rows have raw SSE in `resp_body` but no stored `resp_text`.

Fix applied:

- added `streamview.BestEffortResponseText()` to recover assistant text from:
  - stored `resp_text`
  - non-streaming JSON bodies
  - assembled SSE streams
- used that helper in the management thread endpoint and the TUI conversation view

Regression coverage:

- `TestThreadEndpoint_SSEFallbackWithoutStoredRespText`

### 4. TUI conversation view rendered complex messages as blank blocks

Severity: low

The API thread endpoint already had placeholders for tool-call-only / complex messages, but the TUI conversation view rendered the same messages with empty content. That made tool-heavy threads look broken even when attribution was correct.

Fix applied:

- reused the same placeholder behavior in the TUI thread loader for complex and empty messages

## Files Changed

- `internal/database/db.go`
- `internal/database/db_test.go`
- `internal/database/writer.go`
- `internal/database/writer_test.go`
- `internal/management/api.go`
- `internal/management/server_test.go`
- `internal/streamview/extract.go`
- `internal/tui/model.go`

## Validation

Focused regressions:

- `go test -tags sqlite_fts5 ./internal/database -run 'TestMigrate_LegacySchemaUpgradesColumnsAndFTS|TestWriterChatLineage_LongTurnFallsBackBeyondFivePrefixes'`
- `go test -tags sqlite_fts5 ./internal/management -run 'TestThreadEndpoint_SSEFallbackWithoutStoredRespText|TestThreadEndpoint_SingleEntry|TestThreadEndpoint_MultiTurnChain'`
- `go test -tags sqlite_fts5 ./internal/tui`

Full repo validation:

- `./scripts/build-test-lint-all.sh`

Result: pass

## Merge Readiness

After the fixes above, the branch is in materially better shape. The critical migration break and the two thread/lineage correctness issues are patched and covered by regression tests, and the full repository validation script passes.
