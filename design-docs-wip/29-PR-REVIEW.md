# Code Review: `feat/chat-specialization`

## Overview

The `feat/chat-specialization` branch introduces deep support for `/v1/chat/completions` traffic, including:
- **Conversation Threading**: Deterministic lineage tracking via SHA-256 message hashing and parent lookup.
- **Stream Assembly**: On-the-fly reconstruction of SSE response bodies in TUI, Web UI, and Emacs.
- **Sidecar Search**: Extraction of plain-text request/response content for improved FTS5 indexing.
- **Unified Thread API**: A new `/api/logs/{id}/thread` endpoint for top-down conversation assembly.

## Review Findings & Fixes Applied

### 1. Missing SSE Assistant Responses in Thread View (BUG)
- **Issue**: If chat enrichment was skipped (e.g., due to async queue lag or manual insertion), the thread assembly algorithm failed to extract the assistant's response for SSE streams because it only checked `RespText` or non-streaming JSON.
- **Fix**: Updated `BuildThreadMessages` in `internal/chat/thread.go` to accept an optional `sseExtractor` callback. The API and TUI now use `streamview.Build` as a fallback during thread assembly to recover the assistant text from raw SSE bytes when `RespText` is missing.
- **Verification**: Added `TestThreadEndpoint_SSEAssistantResponse_NoRespText` to `internal/management/server_test.go`.

### 2. TUI Status Filter and Title Issues (BUG)
- **Issue**: The TUI status filter label incorrectly displayed exact codes like `200` as `200xx`. Additionally, the filter only cycled through `200`, `400`, and `500`, despite the query DSL supporting wildcards like `4xx`.
- **Fix**: Corrected the filter label logic in `internal/tui/model.go`. The TUI now accurately reflects the exact status code filter being applied.

### 3. TUI Thread View Metadata (IMPROVEMENT)
- **Issue**: The TUI's thread view title re-calculated Turn N of M locally, which was inconsistent with the API's top-down attribution and could be wrong if the chain was broken.
- **Fix**: Updated the TUI to use `selected_entry_index` and `total_entries` provided by the API.

### 4. Code Deduplication
- **Issue**: Thread assembly logic was duplicated across `internal/management/api.go` and `internal/tui/model.go`.
- **Fix**: Created `internal/chat/thread.go` with a generic `BuildThreadMessages` function and a `ThreadEntry` interface. This allows both the management API and the TUI to share the same attribution algorithm without circular dependencies.

### 5. Increased Truncation Limit for TUI
- **Issue**: TUI `detailView` was truncating bodies at 2000 characters, which is often too small for chat logs.
- **Fix**: Increased the limit to 10,000 characters.

## Test Execution Results

### Go Backend
All tests passed with `-tags sqlite_fts5`:
- `internal/chat`: 26 tests passed.
- `internal/database`: 23 tests passed (including FTS and lineage).
- `internal/management`: 36 tests passed (including the new SSE thread fix).
- `internal/query`: 26 tests passed (DSL and SQL generation).
- `internal/streamview`: 38 tests passed.
- `internal/proxy`: 21 tests passed.

### Emacs Client
All 30 ERT tests passed in `emacs/memoryelaine-test.el`.

## Recommendation

The branch is **APPROVED** for merge to `main` with the applied patches. The fixes ensure that the conversation view is robust even when database enrichment is incomplete and provide a more consistent experience across all frontends.
