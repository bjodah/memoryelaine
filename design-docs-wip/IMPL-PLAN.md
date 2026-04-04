# Implementation Plan: WebUI Shortcut and Navigation Revisions

## Overview

This plan revises the WebUI work items from `TODO.md` and incorporates the
following product decisions:

- Help should be bound to `?`, not `h`
- In detail view, `?` should open a help popup that shows only the **Detail View**
  section
- The Query DSL section should be hidden when help is opened from detail view
- `n` / `p` in detail view should navigate across filtered page boundaries, not
  just within the currently loaded page
- No replacement keyboard scrolling shortcut is needed after removing `n` / `p`
  scrolling

The implementation is expected to touch only the static WebUI files:

- `internal/web/static/index.html`
- `internal/web/static/style.css`
- `internal/web/static/app.js`

## Clarified Requirements

### 1. Main view hint placement and copy

Current behavior:
- The help hint is rendered as a standalone node at the end of the page
- CSS uses `position: fixed`, so it stays pinned in the viewport while scrolling

Required behavior:
- The hint should be part of normal document flow and appear below the page
  content, near the bottom of the main view
- It should scroll with the page instead of remaining pinned to the viewport
- The text must be updated from `press h to show help` to `press ? to show help`

### 2. Help popup binding and scope

Main view:
- Pressing `?` opens the help popup
- The popup should show:
  - Main View
  - Query DSL

Detail view:
- Pressing `?` opens the help popup
- The popup should show:
  - Detail View only
- The Query DSL section must be hidden in this context

Interaction constraints:
- Existing detail shortcuts must keep working
- The `w` prefix flow must keep working exactly as before
- `?` should continue to close the help popup when it is already open

### 3. Detail view `n` / `p` navigation

Required behavior:
- `n` opens the next log entry in the current filtered result set
- `p` opens the previous log entry in the current filtered result set
- Navigation must work across page boundaries using the active filter query
- The selected row in the table must stay in sync with the detail view

Boundary behavior:
- If the user is on the first filtered result, `p` should do nothing except
  optionally show a short status message
- If the user is on the last filtered result, `n` should do nothing except
  optionally show a short status message

Non-goals:
- No replacement keyboard shortcut for scrolling the detail panel
- No API changes unless implementation proves them necessary

## Implementation Plan

### Task 1. Move the main-view help hint into normal page flow

Files:
- `internal/web/static/index.html`
- `internal/web/static/style.css`

Changes:
1. Keep the existing hint element near the bottom of the main page structure
2. Remove viewport-pinned positioning from `.keyboard-hint`
3. Give the hint normal-flow spacing so it visually reads as a footer-like hint
4. Update the displayed shortcut text from `h` to `?`

Acceptance criteria:
- The hint is visible below the main content
- The hint scrolls away with the page
- The hint text says `press ? to show help`

### Task 2. Make help context-aware and bind it to `?`

Files:
- `internal/web/static/index.html`
- `internal/web/static/app.js`

Changes:
1. Update shortcut labels in the help markup:
   - Main View help row should show `?`
   - Detail View help row should show `?`
2. Add explicit help context state in `app.js`, for example:
   - `main`
   - `detail`
3. Update help-open logic so:
   - opening from main view sets help context to `main`
   - opening from detail view sets help context to `detail`
4. Update help rendering/toggling so:
   - main context shows Main View and Query DSL
   - detail context shows only Detail View
5. Update global key handling:
   - main view: `?` opens help
   - detail view: `?` opens help
   - help open: `?` closes help
6. Preserve the existing `w` prefix behavior in detail view so `w h` still copies
   request headers and is not intercepted by help handling

Notes:
- Because keyboard handling is centralized, this should be implemented by
  changing the existing dispatcher rather than adding a separate listener
- The old `h` help binding should be removed from both behavior and help text

Acceptance criteria:
- `?` opens help from main view
- `?` opens help from detail view
- Main-view help shows Main View + Query DSL
- Detail-view help shows Detail View only
- `w h`, `w b`, `w H`, and `w B` still work in detail view
- `h` no longer opens help anywhere

### Task 3. Remap detail `n` / `p` from scrolling to adjacent-entry navigation

Files:
- `internal/web/static/app.js`

Changes:
1. Remove the existing detail-view `n` / `p` scroll behavior
2. Reuse the existing table-selection state:
   - `currentLogs`
   - `selectedLogId`
   - `offset`
   - current query from `queryFilter`
3. Implement adjacent-entry navigation logic for detail view:
   - if the adjacent entry is on the current page, update selection and call
     `showDetail(nextId)`
   - if the adjacent entry crosses a page boundary, update `offset`, reload the
     filtered page, restore the expected row selection, then call
     `showDetail(nextId)`
4. Keep navigation aligned with the current sort order from `/api/logs`
   (currently descending by `ts_start`)
5. Show a short status message when navigation cannot move further

Suggested implementation shape:
- Introduce a helper that computes the current selected row index
- Introduce a helper that loads a target page and returns the new `currentLogs`
- Introduce a helper like `navigateDetailByDelta(delta)` that owns all page
  boundary handling

Boundary cases to handle:
- no current selection
- empty filtered result set
- first row on first page
- last row on last page
- current selection disappears because the filter changed before navigation
- async race: user triggers multiple navigations while a page fetch is in flight

Acceptance criteria:
- `n` opens the next filtered log entry, including across page boundaries
- `p` opens the previous filtered log entry, including across page boundaries
- Table selection stays synchronized with the detail view after navigation
- At the first/last filtered result, navigation stops cleanly

## Manual Verification Checklist

### Main view

- Open the page and confirm the footer hint reads `press ? to show help`
- Scroll the page and confirm the hint scrolls with the document
- Press `?` and confirm help opens
- Confirm main-view help shows:
  - Main View
  - Query DSL
- Press `?` again and confirm help closes
- Press `h` and confirm nothing opens

### Detail view help

- Open any log entry
- Press `?` and confirm help opens
- Confirm the popup shows only the Detail View section
- Confirm the Query DSL section is not visible
- Press `?` again and confirm help closes
- Press `h` and confirm help does not open

### Detail view copy-prefix behavior

- Open any log entry
- Press `w` then `h` and confirm request headers are copied
- Press `w` then `b` and confirm request body is copied
- Press `w` then `H` and confirm response headers are copied
- Press `w` then `B` and confirm response body is copied

### Detail view navigation

- Open a detail entry from the middle of a page and confirm `n` / `p` move to
  adjacent entries
- Open the last entry on a page and confirm `n` moves to the first entry on the
  next filtered page
- Open the first entry on a page and confirm `p` moves to the last entry on the
  previous filtered page
- Confirm filtered navigation still works when a query is present
- Confirm behavior at the very first and very last filtered results

## Suggested Order

1. Task 2: help-binding change and context-aware help rendering
2. Task 1: hint text and positioning update
3. Task 3: cross-page detail navigation with manual verification

## Validation

Automated validation available in-repo:
- `node --check internal/web/static/app.js`

Recommended final verification:
- `./scripts/build-test-lint-all.sh`

Because the WebUI behavior is driven by static browser-side JavaScript, the
critical verification for this work is manual interaction testing in the browser.
