# Implementation Plan: Revised Ellipsis Truncation & Improved Emacs Bindings

## Background

### What the system is

`memoryelaine` is a single-binary Go middleware proxy for OpenAI-compatible
inference APIs. It captures request/response traffic to a local SQLite database
and provides four frontends for inspecting it: CLI, TUI (Bubbletea), Web UI
(vanilla JS), and an Emacs client (elisp package).

### Current branch state (`emacs-json`)

The current branch adds:

- A new `memoryelaine-json-view.el` module for a tree-sitter-powered JSON
  inspector buffer with folding.
- Section navigation, entry navigation, and line navigation in the Emacs show
  buffer.
- Raw copy commands for request/response headers and bodies.
- Client-side ellipsis truncation of long JSON string values in the Emacs detail
  buffer.
- An Emacs README and an Emacs 29.1+ version requirement.

### Current truncation model

There are currently three independent concepts:

| Layer | Where | Default | Meaning |
|-------|-------|---------|---------|
| Capture cap | Go proxy | 8 MB | Limits what is stored in SQLite |
| Preview cap | Management API | 64 KB | Byte-truncates `/api/logs/{id}/body` unless `full=true` |
| Display ellipsis | Emacs only | 60 chars | Shortens long JSON string values for scan readability |

The current branch exposes two concrete problems:

1. `memoryelaine-show-next-entry` and `memoryelaine-show-previous-entry` are
   wired backwards.
2. Emacs `j`/`J` and raw body copy commands can silently operate on preview
   bodies instead of canonical full bodies.

The desired UX improvements are still correct, but the original draft plan had
an architectural gap: it moved display ellipsis into the API without also
separating display-modified bodies from preview-truncated bodies in the response
metadata and client state model.

## Revised design goals

1. Fix the inverted Emacs entry navigation.
2. Make Emacs `j`/`J` and raw body copy commands auto-fetch canonical full
   bodies on demand.
3. Move JSON ellipsis logic into shared Go code so Web, TUI, and Emacs can use
   the same transformation rules.
4. Update the body API contract so clients can distinguish:
   preview truncation, display ellipsis, and canonical full-body availability.
5. Preserve valid JSON for display-oriented views instead of applying byte
   preview truncation first and then attempting JSON ellipsis on broken JSON.
6. Keep the Emacs 29.1+ stance explicit.

## Core design decisions

### 1. Shared Go transform, not "server-side everywhere"

The right abstraction is a shared Go package used in two ways:

- by the management API for Web and Emacs
- directly by the TUI, which reads from the database and does not go through the
  HTTP API

This is still the correct centralization move, but the plan should describe it
as shared transformation logic, not as a purely server-mediated feature.

### 2. Display ellipsis is not the same as preview truncation

These are separate states and must remain separate in the API and client logic:

- `truncated`: the returned payload was byte-truncated for preview purposes
- `ellipsized`: the returned payload was structurally modified for display
- `complete`: the returned payload is the canonical full body and is safe to
  treat as authoritative for copy/inspection

The existing `truncated` field is not sufficient for the revised workflow.

### 3. Display-oriented JSON should operate on valid JSON

If the API is asked for display ellipsis, it should attempt the transform on the
full stored body, not on an already byte-truncated preview. Otherwise the JSON
transform will frequently fail on the exact large payloads where it is needed.

### 4. Emacs and Web must key off `complete`, not `not truncated`

The current Emacs state logic infers full-vs-preview from `truncated`. That is
no longer correct once display ellipsis exists. The revised plan must make the
clients use explicit response metadata instead.

---

## Detailed change list

### Part 1: Shared Go package for JSON ellipsis

#### 1.1 New file: `internal/jsonellipsis/transform.go`

Create a new package:

**Package:** `memoryelaine/internal/jsonellipsis`

**Contract:**

```go
// Transform rewrites JSON string values for scan-oriented display.
//
// The transform preserves JSON structure semantically, but it does not promise
// byte-for-byte preservation of the original source formatting.
//
// Parameters:
//   - src:      raw JSON bytes
//   - limit:    maximum visible rune count for truncated JSON string values
//   - keys:     optional case-insensitive set of object keys whose values may be
//               truncated even at shallow depth; nil means all string values are
//               eligible
//   - minDepth: minimum nesting depth for truncation to apply unless the key is
//               listed in keys
//
// Returns:
//   - transformed JSON bytes
//   - changed=true if at least one string value was modified
//   - error if src is not valid JSON
func Transform(src []byte, limit int, keys map[string]bool, minDepth int) ([]byte, bool, error)
```

**Supporting definitions:**

```go
var DefaultKeys = map[string]bool{
	"prompt":    true,
	"content":   true,
	"text":      true,
	"arguments": true,
	"input":     true,
	"output":    true,
}

const DefaultLimit = 60
```

**Implementation notes:**

- Use `encoding/json.Decoder` in token mode.
- Call `Decoder.UseNumber()` to avoid gratuitous numeric coercion.
- Treat `limit` as a rune count, not a byte count.
- Return `changed=false` when the input is valid JSON but no values needed
  truncation.
- Document that whitespace / escaping / object formatting may change because the
  output is re-encoded JSON.

Do not promise byte-for-byte preservation. That is not compatible with the
proposed token-stream implementation.

#### 1.2 New file: `internal/jsonellipsis/transform_test.go`

Add tests for:

| Test name | What it verifies |
|-----------|-----------------|
| `TestTransformBasicObject` | Matching long values are ellipsized |
| `TestTransformNestedObject` | Depth-based truncation works |
| `TestTransformArray` | String values in arrays are handled |
| `TestTransformKeyFiltering` | Key filtering works |
| `TestTransformMinDepth` | Shallow values are preserved unless key-matched |
| `TestTransformNoChanges` | Valid JSON with no long strings returns `changed=false` |
| `TestTransformNonJSON` | Invalid JSON returns an error |
| `TestTransformPreservesScalars` | Numbers, booleans, and null survive semantically |
| `TestTransformUnicodeStrings` | Truncation happens at rune boundaries |
| `TestTransformDefaultKeys` | Root-level `prompt`-like keys are still eligible |

### Part 2: Management API contract changes

#### 2.1 Edit file: `internal/management/dto.go` — `BodyResponse`

Extend `BodyResponse` so it describes the returned payload precisely:

```go
type BodyResponse struct {
	Part          string `json:"part"`
	Mode          string `json:"mode"`
	Full          bool   `json:"full"`
	Content       string `json:"content"`
	IncludedBytes int    `json:"included_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
	Truncated     bool   `json:"truncated"`             // byte preview truncation
	Ellipsized    bool   `json:"ellipsized,omitempty"`  // display transform modified content
	Complete      bool   `json:"complete"`              // canonical unmodified full body
	Reason        string `json:"reason,omitempty"`
}
```

Field semantics:

- `Full`: echo of the request mode (`full=true` was requested)
- `Truncated`: the returned content was byte-truncated for preview
- `Ellipsized`: the returned content was modified for display
- `Complete`: the returned content is the canonical full unmodified body

`Complete` is the field clients should use to decide whether they need to
auto-fetch before copy / JSON inspection.

#### 2.2 Edit file: `internal/management/api.go` — `handleBody`

Add support for `?ellipsis=N`.

**Query parsing:**

```go
ellipsisLimit := 0
if v := r.URL.Query().Get("ellipsis"); v != "" {
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		ellipsisLimit = n
	}
}
```

**Response-building rules:**

1. Load the canonical source content for the requested body/mode.
2. Set `resp.TotalBytes` from the canonical source body length.
3. Default `resp.Complete = true`.
4. If `ellipsisLimit > 0`, attempt `jsonellipsis.Transform` against the full
   source body.
5. If the transform succeeds and `changed=true`:
   - return the transformed JSON
   - set `Ellipsized = true`
   - set `Complete = false`
   - do **not** apply byte preview truncation to the transformed JSON
     output, because the point of this code path is valid display-oriented JSON
6. Otherwise fall back to the existing preview/full behavior:
   - if `full=false` and content exceeds `previewBytes`, byte-truncate it
   - set `Truncated = true`
   - set `Complete = false`
7. If no ellipsis and no preview truncation occurred, keep `Complete = true`.

This is intentionally different from the original draft. The display-ellipsis
path is a display view, not a raw-preview view, so valid transformed JSON takes
priority over the byte preview cap.

#### 2.3 Edit file: `internal/management/server_test.go`

Add tests for the revised API semantics:

| Test name | What it verifies |
|-----------|-----------------|
| `TestBodyEndpointEllipsisChangedSetsFlags` | `ellipsized=true`, `complete=false`, `truncated=false` when JSON display transform modifies the payload |
| `TestBodyEndpointEllipsisNoChangeFallsBackToPreviewRules` | Large JSON with no eligible long strings still uses normal preview truncation |
| `TestBodyEndpointEllipsisIgnoredForNonJSON` | Non-JSON bodies remain on the normal preview/full path |
| `TestBodyEndpointCompleteTrueForCanonicalBody` | Raw unmodified full body returns `complete=true` |
| `TestBodyEndpointFullAndEllipsisStillNotComplete` | `?full=true&ellipsis=N` returns non-canonical display content with `complete=false` |
| `TestBodyEndpointPreviewTruncationSetsCompleteFalse` | Normal byte preview truncation still works and marks `complete=false` |

The important new edge case is "ellipsized but not preview-truncated". That
case must be covered explicitly because it is the case that broke the original
draft.

### Part 3: TUI adoption via shared Go package

#### 3.1 Edit file: `internal/tui/model.go`

Replace:

```go
truncStr(e.ReqBody, 10000)
truncStr(respBodyContent, 10000)
```

with:

```go
ellipsizeBody(e.ReqBody, 10000)
ellipsizeBody(respBodyContent, 10000)
```

Add:

```go
func ellipsizeBody(s string, maxLen int) string {
	if len(s) > 0 && (s[0] == '{' || s[0] == '[') {
		if transformed, changed, err := jsonellipsis.Transform(
			[]byte(s), jsonellipsis.DefaultLimit, jsonellipsis.DefaultKeys, 2,
		); err == nil && changed {
			s = string(transformed)
		}
	}
	return truncStr(s, maxLen)
}
```

Rationale:

- TUI does not use the management API.
- It should still reuse the same Go transform logic.
- `truncStr` remains as a terminal-width safety net, not the primary JSON
  display mechanism.

#### 3.2 Edit file: `internal/tui/model_test.go`

Add tests for:

- JSON body ellipsizes at string level
- non-JSON falls back cleanly
- unchanged JSON does not regress formatting behavior

### Part 4: Web UI changes

#### 4.1 Edit file: `internal/web/static/app.js`

Update `fetchBody` to accept an `ellipsis` argument:

```javascript
async function fetchBody(part, mode, full, ellipsis) {
    let url = `/api/logs/${id}/body?part=${part}`;
    if (mode) url += `&mode=${mode}`;
    if (full) url += `&full=true`;
    if (ellipsis && ellipsis > 0) url += `&ellipsis=${ellipsis}`;
    // ...
}
```

Define:

```javascript
const ELLIPSIS_LIMIT = 120;
```

Apply `ellipsis` on display-oriented preview loads:

- request body load button handler
- response body load button handler
- response raw/assembled toggle reloads

Do **not** pass `ellipsis` when the user explicitly requests canonical full
content.

#### 4.2 Update Web UI rendering logic to use `complete`

The current Web UI shows "Load Full" only when `truncated` is true. That is no
longer sufficient.

Update `renderBodyContent` so:

- the info line can indicate whether the current content is preview-truncated or
  ellipsized for display
- the "Load Full" button appears whenever `complete` is false
- "Load Full" fetches `full=true` with no `ellipsis`

This is required to keep full-body access available when the payload is
ellipsized-but-not-preview-truncated.

### Part 5: Emacs client changes

#### 5.1 Edit file: `emacs-memoryelaine/memoryelaine-show.el`

##### 5.1.1 Remove client-side JSON ellipsis

Delete the client-side display transform helpers:

- `memoryelaine-show--json-transform-for-display`
- `memoryelaine-show--json-object-p`
- `memoryelaine-show--json-key-name`
- `memoryelaine-show--should-ellipsize-string-p`
- `memoryelaine-show--ellipsize-string`
- `memoryelaine-show--truncate-at-any-depth-keys`

Keep `memoryelaine-show--maybe-pretty-print-json`, but simplify it so it only
pretty-prints valid JSON returned by the server.

##### 5.1.2 Update `memoryelaine-show--fetch-body`

For preview/display fetches:

- pass `ellipsis` when `memoryelaine-show-string-ellipsis-limit` is a positive
  integer and `full` is nil

For canonical fetches:

- use `full=true`
- do not pass `ellipsis`

##### 5.1.3 Make the body-state model key off `complete`

The current code infers body state from `truncated`, which is no longer valid.

Update `memoryelaine-state-detail-set-body` in
`emacs-memoryelaine/memoryelaine-state.el` so:

```elisp
(let ((complete (alist-get 'complete body-info)))
  ;; full means canonical full body, not merely "not preview-truncated"
  ...)
```

State rules:

- `full` state means canonical complete unmodified content
- `preview` state means anything else: preview-truncated or ellipsized display
  content

This is the key client-side fix required by the revised API contract.

##### 5.1.4 Fix inverted entry navigation

Change:

```elisp
(memoryelaine-show--open-neighbor-entry -1)
(memoryelaine-show--open-neighbor-entry 1)
```

to:

```elisp
(memoryelaine-show--open-neighbor-entry 1)
(memoryelaine-show--open-neighbor-entry -1)
```

##### 5.1.5 Auto-fetch canonical full bodies for `j`/`J` and raw body copy

Update:

- `memoryelaine-show-open-request-json-view`
- `memoryelaine-show-open-response-json-view`
- `memoryelaine-show-copy-request-body`
- `memoryelaine-show-copy-response-body`

Behavior:

- if cached body state is `full`, use it immediately
- otherwise fetch the canonical full body first, then continue

This should use a helper like `memoryelaine-show--fetch-body-then`, but that
helper must preserve the existing generation and entry-id staleness checks.

##### 5.1.6 Update the show-buffer notice text

The current preview banner only reflects `truncated`.

Revise body rendering so the user gets accurate feedback for all incomplete
display states, for example:

- preview-truncated content
- ellipsized display content
- both should still advertise that `t` or on-demand auto-fetch can retrieve the
  canonical full body

The important rule is: the detail buffer must never silently look canonical when
it is not.

#### 5.2 Edit file: `emacs-memoryelaine/memoryelaine.el`

Keep `memoryelaine-show-string-ellipsis-limit`, but revise the docstring to
describe the new behavior:

- it is sent as `?ellipsis=N` on preview/display fetches
- it does not affect canonical full-body fetches for copy / JSON inspector
- `nil` disables display ellipsis

Also remove redundant top-level `require` of `memoryelaine-json-view` if it is
no longer needed outside the show module.

#### 5.3 Edit file: `emacs-memoryelaine/memoryelaine-test.el`

Remove tests for deleted client-side ellipsis helpers.

Add tests for:

| Test name | What it verifies |
|-----------|-----------------|
| `memoryelaine-test-show-next-entry-direction` | `next` uses `+1` |
| `memoryelaine-test-show-previous-entry-direction` | `previous` uses `-1` |
| `memoryelaine-test-show-fetch-body-passes-ellipsis` | preview/display requests send `ellipsis` |
| `memoryelaine-test-show-fetch-body-no-ellipsis-when-full` | canonical fetches do not send `ellipsis` |
| `memoryelaine-test-state-detail-set-body-complete-means-full` | `complete=true` maps to `full` state |
| `memoryelaine-test-state-detail-set-body-ellipsized-means-preview` | `ellipsized=true` and `complete=false` do not map to `full` |
| `memoryelaine-test-show-json-view-auto-fetches-when-incomplete` | `j` fetches canonical content when cached body is not complete |
| `memoryelaine-test-show-copy-body-auto-fetches-when-incomplete` | raw copy fetches canonical content when cached body is not complete |

The old tests for client-side JSON transformation should be removed because that
logic is intentionally gone from Emacs.

#### 5.4 Edit file: `emacs-memoryelaine/README.md`

Keep and strengthen the explicit version note:

```text
The package requires Emacs 29.1+. Older versions are not supported and there
are no plans to add backward compatibility for Emacs 28 or earlier.
```

### Part 6: Summary of files touched

| File | Action | What changes |
|------|--------|-------------|
| `internal/jsonellipsis/transform.go` | CREATE | Shared JSON display-ellipsis transform |
| `internal/jsonellipsis/transform_test.go` | CREATE | Transform tests |
| `internal/management/api.go` | EDIT | Add `?ellipsis=` handling and revised response-building semantics |
| `internal/management/dto.go` | EDIT | Add `Ellipsized` and `Complete` to `BodyResponse` |
| `internal/management/server_test.go` | EDIT | Add API tests for new metadata semantics |
| `internal/tui/model.go` | EDIT | Use shared transform for JSON display |
| `internal/tui/model_test.go` | EDIT | Add TUI ellipsis tests |
| `internal/web/static/app.js` | EDIT | Pass `ellipsis`, consume `complete`, keep canonical full-body access |
| `emacs-memoryelaine/memoryelaine-show.el` | EDIT | Remove client-side transform, pass `ellipsis`, fix nav, auto-fetch full bodies, improve notices |
| `emacs-memoryelaine/memoryelaine-state.el` | EDIT | Base body state on `complete`, not `truncated` |
| `emacs-memoryelaine/memoryelaine.el` | EDIT | Update defcustom docstring and remove redundant require if appropriate |
| `emacs-memoryelaine/memoryelaine-test.el` | EDIT | Remove obsolete tests and add new behavior tests |
| `emacs-memoryelaine/README.md` | EDIT | Strengthen Emacs 29.1+ note |

---

## Implementation order

Recommended order:

1. Fix Emacs entry navigation and add auto-fetch for `j`/`J`/copy.
2. Add `internal/jsonellipsis` with tests.
3. Revise `BodyResponse` and `handleBody` semantics.
4. Update Emacs state handling to use `complete`.
5. Update Web UI to use `complete`.
6. Update TUI to call the shared transform.
7. Run full verification.

This order isolates the UX fixes from the API contract work and makes the state
model change explicit before the clients are switched over.

## Verification checklist

After implementation, verify:

- [ ] `go test ./internal/jsonellipsis/...` passes
- [ ] `go test ./internal/management/...` passes
- [ ] `go test ./internal/tui/...` passes
- [ ] `go test ./...` passes
- [ ] `golangci-lint run` reports 0 issues
- [ ] ERT tests pass
- [ ] `./scripts/build-test-lint-all.sh` passes
- [ ] Manual: Emacs `C-M-n` / `C-M-p` navigation direction is correct
- [ ] Manual: Emacs detail view clearly indicates non-canonical display content
- [ ] Manual: Emacs `j` and raw copy fetch canonical full bodies even when the
  cached preview is ellipsized but not byte-truncated
- [ ] Manual: Web UI still offers "Load Full" whenever `complete=false`
- [ ] Manual: Web UI display-oriented body view shows valid ellipsized JSON
- [ ] Manual: TUI shows ellipsized JSON values for large JSON request/response
  bodies
