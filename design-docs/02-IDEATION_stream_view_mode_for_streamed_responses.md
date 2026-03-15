# Stream view mode for streamed responses

## Background

The current proxy stores the raw captured response body. For streamed responses,
this is useful for debugging, but it is often not the most readable format for a
human inspecting logs.

For example, Server-Sent Events (SSE) responses for OpenAI-compatible APIs are
typically stored as a sequence of `data:` events. That preserves the original
stream, but it means a user must mentally reconstruct the final assistant reply
from many small chunks.

The core problem is therefore not primarily database storage overhead. The core
problem is log readability.

## Proposal

Add a `Stream view mode` toggle to log viewers:

- `Raw`: show the stored response body exactly as captured.
- `Assembled`: parse supported streamed response formats and present the
  reconstructed assistant text.

This should be a view-layer feature in the TUI and web UI. The database should
continue storing the raw captured response body as the canonical record.

## Why this direction

This approach keeps the system safer and simpler than changing the storage
format:

- Raw captured data remains available for debugging, regression testing, and
  future parser improvements.
- No schema migration is required.
- The user-facing problem is solved where it appears: in the log viewer.
- Unsupported or partially captured streams can still be viewed without data
  loss.

## Initial Scope

The first implementation should support assembled rendering only for known,
high-value streamed endpoints:

- `/v1/chat/completions`
- `/v1/completions`

This limited scope improves implementability and keeps the parser contract
narrow enough to test properly.

The assembled mode should only be offered when all of the following are true:

- the request path is one of the supported paths
- the response appears to be streamed SSE data
- the stored response body is not truncated
- parsing succeeds, or parsing succeeds partially with enough recovered text to
  show a clearly marked partial assembled view

If any of these conditions are not met, the UI should fall back to `Raw` and
should not pretend that an assembled view is available.

## Behavior

### Raw mode

`Raw` mode shows the exact stored response body, including SSE framing and event
boundaries.

This mode is the source of truth for debugging.

### Assembled mode

`Assembled` mode is a derived presentation of the raw response body.

For `/v1/chat/completions`, the viewer should assemble text deltas into a single
assistant response string.

For `/v1/completions`, the viewer should assemble streamed text chunks into one
final completion string.

The assembled view is best-effort. It is not a replacement for the raw log.

If the stream is interrupted or the final event is malformed, but enough earlier
events were parsed successfully, the viewer may show a partial assembled view
with an explicit warning state such as `ReasonPartialParse`.

The first version should not silently collapse unsupported structures into a
misleading assembled string. In particular:

- multi-choice streams should not silently display only choice `0`
- tool-call-only streams should not display a blank assembled result as if
  nothing happened

These cases should surface explicit status metadata and fall back to `Raw`.

## Non-Goals

- Do not replace raw storage with assembled storage.
- Do not discard stream structure in the database.
- Do not attempt broad support for every possible OpenAI-compatible streaming
  variant in the first version.
- Do not expose assembled output for truncated or unparsable streams as if it
  were authoritative.

## Implementation Notes

- The parsing logic should live close to the viewer layer or in a shared helper
  used by both TUI and web UI.
- The viewer should clearly indicate the active mode: `Raw` or `Assembled`.
- If assembled parsing fails completely, the UI should surface that plainly and
  keep `Raw` available.
- If assembled parsing succeeds only partially, the UI should still present the
  recovered text with an explicit warning state.
- Assembled output should always be treated as plain text in the UI, not as
  HTML or Markdown.
- The API may eventually expose both raw and assembled representations, but the
  first version should avoid duplicating the raw response body when the client
  already has access to it via the stored log entry.

## Acceptance Criteria

1. A user inspecting a supported streamed `/v1/chat/completions` response can
   switch between `Raw` and `Assembled` views.
2. A user inspecting a supported streamed `/v1/completions` response can switch
   between `Raw` and `Assembled` views.
3. For partially parsed streamed responses, the viewer can show recovered
   assembled text with an explicit partial-warning state.
4. For truncated streamed responses, only `Raw` is shown.
5. For unsupported or non-streamed responses, `Raw` remains the only view.
6. Raw stored response data remains unchanged in the database schema and log
   records.
