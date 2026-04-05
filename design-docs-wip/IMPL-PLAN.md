# Implementation Plan: TODO Follow-ups for Assembled View + Body Export

## Purpose

This document turns [TODO.md](/work/design-docs-wip/TODO.md) into an
implementation plan for two related enhancements:

1. make the assembled response view the default inspection mode
2. add explicit body export/save actions across Web, Emacs, and TUI
3. extend assembled response rendering so `reasoning_content` is visible as its
   own section instead of being silently dropped

The current codebase already has:

- raw vs assembled response support in `internal/streamview`
- a management body API used by Web + Emacs
- direct streamview use in the TUI
- copy/open-body affordances in Web + Emacs

The main gap is that the assembled representation is currently modeled as a
single flat string. The TODO requires it to become a structured presentation
with at least two logical sections:

- reasoning content: default folded when present
- assistant content: default expanded

## Current State

### Backend

- `internal/streamview.Build` returns `RawBody`, `AssembledBody`,
  `AssembledAvailable`, and a single availability reason.
- `/v1/chat/completions` assembly only extracts `choices[0].delta.content`.
- `reasoning_content` is not preserved anywhere in the assembled view.
- `database.LogWriter.SSEExtractor` uses the flat assembled string for sidecar
  extraction / FTS.

### Management API

- `GET /api/logs/{id}` exposes only coarse stream-view availability metadata.
- `GET /api/logs/{id}/body?part=resp&mode=assembled` returns one string
  payload, not a structured assembled document.

### Web UI

- detail view supports raw/assembled toggle
- assembled is not the default
- there are copy/open actions, but no download/export action
- there is no concept of folded reasoning vs expanded content

### Emacs

- detail view supports raw/assembled toggle
- assembled is not the default
- there are copy commands and full-body fetch support
- there is no save-to-file command
- there is no concept of separate reasoning/content sections

### TUI

- detail view supports raw/assembled toggle
- assembled is not the default
- there is no save-to-file flow
- there is no concept of separate reasoning/content sections

## Guiding Decisions

### 1. Keep raw storage canonical

No database schema change is required for the core feature. Raw request/response
bodies remain the source of truth. The new assembled structure stays derived.

### 2. Upgrade `streamview` from "flat text" to "structured assembled document"

The parser should produce both:

- a structured assembled representation for UI rendering
- a flat assembled text helper for existing uses like FTS/thread attribution

This avoids duplicating SSE parsing logic across clients.

### 3. Preserve a stable export path distinct from the display path

Display-oriented folding/collapsing is a UI concern. Export should write a
canonical payload for the chosen mode, not a lossy folded snapshot.

### 4. Prefer additive API changes

Web and Emacs need structured assembled data, but the existing body endpoint is
already useful for copy/export/full-fetch flows. The cleanest change is to add
structured assembled metadata without removing the current flat-body behavior
unless there is a deliberate API-versioning decision.

## Proposed Data Model Changes

Extend `internal/streamview` with an assembled document model.

Suggested shape:

```go
type SectionKind string

const (
    SectionReasoning SectionKind = "reasoning"
    SectionContent   SectionKind = "content"
)

type Section struct {
    Kind    SectionKind `json:"kind"`
    Label   string      `json:"label"`
    Text    string      `json:"text"`
    Folded  bool        `json:"folded"`
    Present bool        `json:"present"`
}

type Result struct {
    RawBody            string
    AssembledBody      string
    AssembledAvailable bool
    Reason             AvailabilityReason
    Sections           []Section
}
```

Notes:

- `AssembledBody` stays as the flat text helper.
- `Sections` is the new source for detail rendering.
- for `/v1/completions`, only a `content` section will normally exist.
- for `/v1/chat/completions`, parse both `delta.reasoning_content` and
  `delta.content` when present.

## API Plan

### Detail endpoint

Extend `GET /api/logs/{id}` so `stream_view` includes enough metadata for
clients to initialize the default detail layout without separately probing the
assembled body format.

Suggested additions:

- `default_mode`: `"assembled"` when assembled is available, else `"raw"`
- `sections`: array of section descriptors with `kind`, `label`, and
  `default_folded`

This keeps client bootstrapping simple.

### Body endpoint

Keep `GET /api/logs/{id}/body` as the full-body retrieval endpoint, but extend
assembled responses with optional structured content.

Recommended shape for `part=resp&mode=assembled`:

- keep existing `content` string for flat export/copy compatibility
- add `sections` for structured rendering

Suggested additive field:

```go
type BodySection struct {
    Kind    string `json:"kind"`
    Label   string `json:"label"`
    Content string `json:"content"`
    Folded  bool   `json:"folded"`
}
```

Why this design:

- Web and Emacs can render foldable sections directly from the API
- export/copy can continue using the flat canonical payload
- TUI can use `streamview.Result.Sections` directly without HTTP

## Implementation Phases

## Phase 1: Clarify the product contract

Update spec/docs first so the implementation has an exact contract.

Decisions to lock down:

- assembled detail view defaults to assembled when available, raw otherwise
- raw view must remain accessible in all clients
- assembled view is response-only; request body remains raw-only
- reasoning section is folded by default when present
- content section is expanded by default when present
- export/save operates on the currently selected body mode unless explicitly
  scoped otherwise

Also update `README.md` / main spec after implementation so docs match shipped
behavior.

## Phase 2: Backend parser and streamview refactor

Files:

- `internal/streamview/openai_sse.go`
- `internal/streamview/view.go`
- `internal/streamview/openai_sse_test.go`
- `internal/streamview/view_test.go`

Work:

1. extend chat chunk parsing to read `choices[0].delta.reasoning_content`
2. accumulate reasoning and content independently
3. populate `Sections`
4. derive `AssembledBody` from the structured result using a documented rule
5. keep existing availability semantics where possible

Recommended flattening rule for `AssembledBody`:

- if content exists, use content as the flat body
- if reasoning exists and content exists, keep reasoning out of flat body unless
  you explicitly want FTS/export/thread views to include it
- if reasoning exists and content is absent, either:
  - expose reasoning as flat assembled body, or
  - mark the response unavailable for assembled mode

This needs a product decision because it affects FTS and export semantics.

Tests to add/update:

- chat SSE with content only
- chat SSE with reasoning only
- chat SSE with reasoning + content interleaved
- partial parse after reasoning but before content
- completions SSE still yields only content section
- unsupported/truncated/not-SSE behavior remains unchanged

## Phase 3: Management DTO and handler updates

Files:

- `internal/management/dto.go`
- `internal/management/api.go`
- `internal/management/server_test.go`

Work:

1. extend detail DTOs so `stream_view` can describe structured assembled output
2. extend `BodyResponse` with optional `sections`
3. for `mode=assembled`, return both flat `content` and structured `sections`
4. keep preview/full/ellipsized/complete semantics intact

Test coverage:

- detail endpoint returns assembled default mode metadata
- body endpoint assembled responses include sections
- reasoning section is present and marked folded
- content section is present and marked expanded
- old raw-body behavior is unchanged

## Phase 4: Web UI changes

Files:

- `internal/web/static/app.js`
- `internal/web/static/index.html`
- `internal/web/static/style.css`

Work:

1. change detail opening so assembled becomes the default mode when available
2. render assembled response as two sections instead of one `<pre>`
3. make reasoning collapsible and default-collapsed
4. make content default-expanded
5. add download actions for:
   - request body
   - response raw body
   - response assembled body
6. preserve current copy/open/full-fetch flows

Recommended UX:

- in raw mode: current single-body rendering remains
- in assembled mode:
  - if reasoning exists: render a collapsible "Reasoning" section
  - if content exists: render a "Content" section
- download action should fetch canonical full body first, then trigger a browser
  download with the default filename

Filename logic:

- request raw: `request-body.json` or `request-body.txt`
- response assembled: `response-body-assembled.json` or
  `response-body-assembled.txt`
- response raw/parts: `response-body-parts.txt`

If "response raw" is actually JSON for non-streamed responses, there is a
possible filename ambiguity with the TODO wording. See questions below.

## Phase 5: Emacs client changes

Files:

- `emacs-memoryelaine/memoryelaine-show.el`
- `emacs-memoryelaine/memoryelaine-state.el`
- `emacs-memoryelaine/memoryelaine-test.el`
- optionally `emacs-memoryelaine/README.md`

Work:

1. default detail mode to assembled when available
2. store assembled section data separately from the flat assembled string
3. render reasoning and content as separate sections
4. make reasoning folded by default
5. add save/export commands for request body and response body
6. prompt for save path via `read-file-name`
7. fetch canonical full payload before writing to disk

Likely command design:

- add explicit interactive commands:
  - save request body
  - save response raw body
  - save response assembled body
- bind them under a prefix map rather than overloading existing copy bindings

Implementation note:

Emacs already has full-body auto-fetch logic. Reuse that path instead of adding
new ad hoc HTTP flows.

## Phase 6: TUI changes

Files:

- `internal/tui/model.go`
- `internal/tui/model_test.go`
- possibly `internal/tui/app.go`

Work:

1. default detail mode to assembled when available
2. render reasoning/content as separate sections in assembled mode
3. add per-section fold state for reasoning/content
4. add save/export keybindings
5. add a lightweight path-entry prompt/modal for writing files

Recommended TUI behavior:

- `v` still toggles raw vs assembled
- assembled detail shows:
  - `Reasoning` section, collapsed by default
  - `Content` section, expanded by default
- a save command prompts for a path, then writes the canonical full payload

This is the largest client-side delta because the TUI currently has no path
prompt workflow.

## Phase 7: Export/save implementation details

Shared rules across clients:

1. exporting request body always uses `part=req&mode=raw`
2. exporting raw response uses `part=resp&mode=raw`
3. exporting assembled response uses `part=resp&mode=assembled`
4. always fetch/use canonical full payload before writing/downloading
5. choose `.json` only when the exported payload is parseable JSON

Important distinction:

- "assembled view" is a derived presentation
- "raw export" normally means canonical unmodified payload

Because the TODO says "export raw to file" but also asks for export in assembled
mode, the implementation should define export as "save the currently selected
representation" rather than literally "raw bytes only".

## Phase 8: Documentation and acceptance tests

Update:

- `README.md`
- `design-docs-main/27-SPEC-version5.md`
- any Web/Emacs help text mentioning raw/assembled defaults or body actions

Acceptance checks:

1. supported streamed chat response opens in assembled mode by default
2. raw mode remains one action away
3. reasoning shows as its own folded section when present
4. content shows as its own expanded section when present
5. Web download writes the expected file with the expected default name
6. Emacs save prompts for a path and writes the canonical full payload
7. TUI save prompts for a path and writes the canonical full payload
8. non-streamed / truncated / unsupported responses still default to raw

## Risks and Compatibility Notes

### Risk: flattening semantics become inconsistent

Today there is one assembled string. After this change there are sections plus a
flat helper. If the flattening rule is not explicitly documented, Web/Emacs/TUI
may render one thing while export/FTS/thread attribution use another.

Mitigation:

- define one canonical flattening rule in `streamview`
- reuse it everywhere

### Risk: assembled default surprises existing users

Users who depended on raw-first inspection may see a behavior change.

Mitigation:

- preserve a clear raw toggle
- update help text/README

### Risk: TUI scope expands more than expected

Adding export plus collapsible sections likely requires new modal/prompt state.

Mitigation:

- implement TUI changes after backend/API/Web/Emacs
- keep the prompt minimal and state-local

## Contradictions / Under-Specification / Questions

### 1. "Export raw to file" conflicts with "assembled and non-assembled view"

If the user is in assembled mode, the exported payload is not raw bytes.

Recommended interpretation:

- export saves the currently selected representation
- raw response export and assembled response export are separate actions

### 2. The TODO names default filenames for `response-body-assembled.*` and `response-body-parts.txt`, but not for raw non-stream JSON responses

If a response is non-streamed JSON and the user exports the raw response body,
should the default be:

- `response-body.json`, or
- `response-body-parts.json`, or
- keep `response-body-parts.txt` for all raw responses

This needs a decision.

### 3. `reasoning_content` format is underspecified

The TODO names `reasoning_content`, but not the exact upstream dialect.

Questions:

- should support initially mean only `choices[0].delta.reasoning_content` as a
  plain string?
- should provider-specific variants also count in scope now?

### 4. What is the canonical assembled export when both reasoning and content exist?

Open question:

- should assembled export write only assistant `content`
- should it concatenate reasoning + content with separators
- should it export structured JSON instead

This is the biggest semantic gap in the TODO.

### 5. What should happen when reasoning exists but content is absent?

Possible interpretations:

- assembled mode is still available and shows only reasoning
- assembled mode is unavailable because user-visible answer content is absent

This affects both rendering and export behavior.

### 6. TUI scope is implied but not fully explicit

The TODO says:

- assembled view should be the default generally
- export shortcuts should exist in Emacs and the TUI, and download in Web
- detail-view reasoning/content change mentions Web + Emacs and says "possibly
  also the TUI?"

Question:

- should the reasoning/content section split be implemented in TUI in the same
  change set, or is TUI allowed to lag behind briefly?

## Recommended Order of Execution

1. clarify questions 1, 2, 4, 5, and 6 above
2. refactor `internal/streamview`
3. extend management DTOs + handlers
4. update Web UI
5. update Emacs
6. update TUI
7. update docs and acceptance tests

## Recommendation

The cleanest implementation is:

- keep raw storage unchanged
- make assembled default when available
- teach `streamview` to return structured sections
- keep a flat assembled helper for compatibility
- treat export as "save selected representation", not "always raw"

That resolves most of the TODO cleanly while limiting API churn.
