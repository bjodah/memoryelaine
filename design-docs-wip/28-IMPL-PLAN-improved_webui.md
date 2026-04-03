# Implementation Plan: Improved Web UI

## 1. Background & Motivation

The memoryelaine web UI (served from `internal/web/static/`) currently provides basic log viewing functionality through mouse-driven interactions only. Users must click table rows to open the detail popup, click "X" or click outside the popup to close it, click "Load Request/Response Body" buttons to view body content, and click "Load Full" to fetch truncated bodies. There are zero keyboard shortcuts, no copy-to-clipboard capability, no visual row-selection indicator, and bodies are never loaded by default.

Meanwhile, the TUI (`internal/tui/`) and Emacs frontend (`emacs-memoryelaine/`) already offer rich keyboard-driven workflows with intuitive keybindings (j/k for navigation, enter to open, ESC/q to close, w-prefix for copy operations, etc.). The wishlist (26-WISHLIST.md) calls for bringing this same level of keyboard ergonomics to the web UI.

Additionally, now that ellipsis compaction is available on the server side (long JSON strings are shortened with `…` in preview mode), it is safe to load bodies by default for entries under a reasonable size threshold (e.g. 128 KiB), removing the current friction of requiring a click to see any body content.

This document lays out a concrete implementation plan covering:
1. Keyboard shortcuts for the main view (table)
2. Keyboard shortcuts for the entry detail popup
3. Visual row-selection indicator
4. Bodies loaded by default (when under size threshold)
5. Copy-to-clipboard buttons for bodies and headers
6. JSON pretty-printing for parseable bodies
7. A help popup describing all shortcuts and the query DSL with examples
8. A subtle hint at the bottom-left of the page ("press 'h' to show help")

---

## 2. Files That Need Changing

### 2.1. `internal/web/static/index.html`
**Why:** Needs new HTML elements for the help popup, keyboard hint text, and copy buttons will be injected dynamically but the help overlay needs static markup.

**Changes:**
- Add a new `#help-overlay` div (analogous to `#detail-overlay`) containing a `.help-panel` with sections for keyboard shortcuts and query DSL examples
- Add a `.keyboard-hint` div at the bottom of the page with text "press 'h' to show help"
- No structural changes to the table or detail overlay are needed; selection indicator and copy buttons will be injected via JavaScript

### 2.2. `internal/web/static/app.js`
**Why:** This is the primary file that will receive the bulk of the new logic: keyboard event handlers, selection state management, copy-to-clipboard functions, default body loading, JSON pretty-printing, and help popup rendering.

**Changes:**
- **Keyboard event dispatcher:** Add a global `keydown` listener that routes events based on whether the detail overlay or help overlay is visible
- **Main-view shortcuts:**
  - `j` / `k`: Move selection up/down through table rows; update a CSS class on the selected `<tr>`
  - `Enter`: Open the detail popup for the currently selected row
  - `/`: Focus the `#query-filter` input (and let default browser behavior handle text entry)
  - `R`: Toggle recording state (calls existing `toggleRecordingState()`)
  - `h`: Open the help overlay
- **Detail-view shortcuts:**
  - `ESC` / `u`: Close the detail overlay (same logic as clicking X)
  - `v`: Toggle stream view (raw/assembled) — already has UI buttons; simulate a click on the appropriate button
  - `c`: Open conversation view — simulate a click on `#view-conversation-btn` if present
  - `t`: Load full bodies — simulate clicks on all "Load Full" buttons currently visible
  - `j` / `J`: Open request/response body in a new window/tab as raw text (or trigger copy + announce)
- **Selection indicator:** In `renderTable()`, add a `data-index` attribute to each `<tr>` and apply a `.selected` CSS class to the currently focused row; add a `selectedRowIndex` state variable
- **Help overlay:** Implement `showHelp()` / `hideHelp()` functions that toggle the `#help-overlay` visibility
- **Copy to clipboard:** Implement `copyToClipboard(text, label)` using `navigator.clipboard.writeText()` with a fallback to `document.execCommand('copy')` for older browsers; add copy buttons to each body/header section in the detail panel
- **Default body loading:** After rendering the detail panel, automatically trigger body fetches for request and response bodies when `entry.req_bytes` / `entry.resp_bytes` are below 128 KiB (131072 bytes); otherwise show a "Body too large to load automatically (X MB). Click to load." button
- **JSON pretty-printing:** In `renderBodyContent()`, attempt `JSON.parse()` on `bodyData.content`; if successful, re-insert using `JSON.stringify(parsed, null, 2)` with proper indentation; if parsing fails, display raw content as before
- **Query-filter focus guard:** When the query input is focused, keyboard shortcuts (`j`, `k`, `Enter`, `R`, `h`) should be suppressed so typing in the query box works normally; `/` should still focus the input even when already focused (selects all text)
- **Escape guard:** When help overlay is open, `ESC` should close it (same as detail overlay)

### 2.3. `internal/web/static/style.css`
**Why:** Needs styles for the selection indicator, help panel, copy buttons, pretty-printed JSON, and keyboard hint text.

**Changes:**
- **Row selection indicator:** Add a `.selected` rule for `<tr>` that adds a left-border accent (e.g. `border-left: 3px solid var(--accent)`) and slightly different background
- **Help overlay:** Styles for `#help-overlay` (similar to `#detail-overlay`), `.help-panel` (similar to `.detail-panel` but maybe narrower, e.g. `max-width: 700px`), `.help-section` for grouping shortcuts vs. query examples, `.help-shortcut-row` for aligning key labels with descriptions, `kbd` element styling for key badges
- **Copy buttons:** `.copy-btn` class — small, subtle button (icon or text "copy") positioned near body headers; hover state; disabled state during copy
- **Keyboard hint:** `.keyboard-hint` — fixed or absolute positioned at bottom-left, gray/muted text, small font size
- **JSON pretty-print:** The existing `pre` styling works; may add a `.json-pretty` class if differentiation is needed
- **Body size warning:** `.body-size-warning` — gray/yellow text for bodies that exceed the auto-load threshold

---

## 3. Detailed Implementation Breakdown

### 3.1. Main View — Keyboard Shortcuts & Selection

**State variables to add:**
```javascript
let selectedRowIndex = -1; // -1 means no selection
let helpOverlayOpen = false;
let queryInputFocused = false;
```

**Key handler structure:**
```javascript
document.addEventListener('keydown', (e) => {
    // If query input is focused, suppress most shortcuts except '/'
    if (queryInputFocused && e.key !== '/') return;
    
    if (helpOverlayOpen) {
        if (e.key === 'Escape') { hideHelp(); e.preventDefault(); }
        return;
    }
    
    if (detailOverlay.classList.contains('hidden') === false) {
        // Detail overlay is open
        if (e.key === 'Escape' || e.key === 'u') { closeDetail(); e.preventDefault(); }
        if (e.key === 'v') { /* toggle stream view */ e.preventDefault(); }
        if (e.key === 'c') { /* open conversation */ e.preventDefault(); }
        if (e.key === 't') { /* load full bodies */ e.preventDefault(); }
        return;
    }
    
    // Main view shortcuts
    switch (e.key) {
        case 'j': selectRow(selectedRowIndex + 1); e.preventDefault(); break;
        case 'k': selectRow(selectedRowIndex - 1); e.preventDefault(); break;
        case 'Enter': if (selectedRowIndex >= 0) openSelectedDetail(); e.preventDefault(); break;
        case '/': queryFilter.focus(); queryFilter.select(); e.preventDefault(); break;
        case 'R': if (e.shiftKey) { toggleRecordingState(); e.preventDefault(); } break;
        case 'h': showHelp(); e.preventDefault(); break;
    }
});
```

Note: The wishlist says `R` (uppercase), which means `Shift+R`. We check `e.key === 'R'` or `e.shiftKey && e.key === 'r'`.

**Selection rendering:**
In `renderTable()`, after creating each `<tr>`, add:
```javascript
tr.dataset.index = i;
if (i === selectedRowIndex) tr.classList.add('selected');
```

Add `selectRow(index)` function that clamps to valid range, removes `.selected` from all rows, and adds it to the target row. Also scrolls the row into view if needed.

**Open selected detail:**
```javascript
function openSelectedDetail() {
    const tr = tbody.querySelector(`tr[data-index="${selectedRowIndex}"]`);
    if (tr) tr.click(); // Reuse the existing click handler which calls showDetail()
}
```

### 3.2. Detail View — Keyboard Shortcuts

The detail view shortcuts will be handled in the same global `keydown` listener, gated on `!detailOverlay.classList.contains('hidden')`.

- **`ESC` / `u`:** Call `detailOverlay.classList.add('hidden')`
- **`v`:** Find `.sv-toggle-btn` elements and simulate clicking the inactive one
- **`c`:** Find `#view-conversation-btn` and click it (only if present and not disabled)
- **`t`:** Find all `.load-body-btn` with text "Load Full" and click them

### 3.3. Help Overlay

**HTML structure in `index.html`:**
```html
<div id="help-overlay" class="hidden">
    <div class="help-panel">
        <button id="close-help">✕</button>
        <h2>Keyboard Shortcuts</h2>
        <div class="help-section">
            <h3>Main View</h3>
            <div class="help-shortcut-row"><kbd>j</kbd> / <kbd>k</kbd> <span>Select next/previous row</span></div>
            <div class="help-shortcut-row"><kbd>Enter</kbd> <span>Open detail for selected row</span></div>
            <div class="help-shortcut-row"><kbd>/</kbd> <span>Focus query input</span></div>
            <div class="help-shortcut-row"><kbd>R</kbd> <span>Toggle recording</span></div>
            <div class="help-shortcut-row"><kbd>h</kbd> <span>Show this help</span></div>
        </div>
        <div class="help-section">
            <h3>Detail View</h3>
            <div class="help-shortcut-row"><kbd>Esc</kbd> / <kbd>u</kbd> <span>Close detail</span></div>
            <div class="help-shortcut-row"><kbd>v</kbd> <span>Toggle raw/assembled view</span></div>
            <div class="help-shortcut-row"><kbd>c</kbd> <span>Open conversation view</span></div>
            <div class="help-shortcut-row"><kbd>t</kbd> <span>Load full bodies</span></div>
            <div class="help-shortcut-row"><kbd>Copy</kbd> buttons <span>Click or use mouse to copy bodies/headers</span></div>
        </div>
        <h2>Query DSL Examples</h2>
        <div class="help-section">
            <div class="help-shortcut-row"><code>status:200</code> <span>Filter by status code</span></div>
            <div class="help-shortcut-row"><code>status:4xx</code> <span>Filter by status range</span></div>
            <div class="help-shortcut-row"><code>method:POST</code> <span>Filter by HTTP method</span></div>
            <div class="help-shortcut-row"><code>path:/chat/completions</code> <span>Filter by request path</span></div>
            <div class="help-shortcut-row"><code>since:1h</code> <span>Entries from the last hour</span></div>
            <div class="help-shortcut-row"><code>has:req has:resp</code> <span>Entries with request/response bodies</span></div>
            <div class="help-shortcut-row"><code>-status:500</code> <span>Negate a filter</span></div>
            <div class="help-shortcut-row"><code>"exact phrase"</code> <span>Quoted phrase search</span></div>
            <div class="help-shortcut-row"><code>status:2xx method:POST path:/chat hello world</code> <span>Combined example</span></div>
        </div>
    </div>
</div>
```

**CSS for help panel:**
- `#help-overlay`: Same positioning as `#detail-overlay` (fixed, full-screen, semi-transparent background)
- `.help-panel`: Similar to `.detail-panel` but `max-width: 700px`
- `kbd`: Inline-block, padding, border, background, monospace font, border-radius
- `.help-shortcut-row`: Flex layout, gap between key and description
- `.help-section`: Margin-bottom for spacing

### 3.4. Bodies Loaded by Default

In `showDetail()`, after rendering the panel HTML, instead of always inserting a "Load Body" button, check the body size:

```javascript
const AUTO_LOAD_THRESHOLD = 131072; // 128 KiB

function renderBodySection(containerId, part) {
    const container = document.getElementById(containerId);
    const hasBody = part === 'req' ? entry.has_request_body : entry.has_response_body;
    const bodyBytes = part === 'req' ? entry.req_bytes : entry.resp_bytes;
    
    if (!hasBody) {
        container.innerHTML = '<em>No body</em>';
        return;
    }
    
    if (bodyBytes > AUTO_LOAD_THRESHOLD) {
        const warn = document.createElement('div');
        warn.className = 'body-size-warning';
        warn.textContent = `Body is ${formatBytes(bodyBytes)} (exceeds auto-load threshold). `;
        const loadBtn = document.createElement('button');
        loadBtn.textContent = 'Load anyway';
        loadBtn.className = 'load-body-btn';
        loadBtn.addEventListener('click', () => loadBody(part, container, false, ELLIPSIS_LIMIT));
        container.innerHTML = '';
        container.appendChild(warn);
        container.appendChild(loadBtn);
        return;
    }
    
    // Auto-load: fetch immediately
    container.innerHTML = '<em>Loading…</em>';
    loadBody(part, container, false, ELLIPSIS_LIMIT);
}
```

### 3.5. Copy-to-Clipboard Buttons

After rendering body content in `renderBodyContent()`, prepend a copy button:

```javascript
function addCopyButton(container, text, label) {
    const btn = document.createElement('button');
    btn.textContent = '📋 Copy';
    btn.className = 'copy-btn';
    btn.title = `Copy ${label}`;
    btn.addEventListener('click', async () => {
        try {
            await navigator.clipboard.writeText(text);
            btn.textContent = '✓ Copied!';
            btn.disabled = true;
            setTimeout(() => {
                btn.textContent = '📋 Copy';
                btn.disabled = false;
            }, 2000);
        } catch (e) {
            // Fallback
            const ta = document.createElement('textarea');
            ta.value = text;
            document.body.appendChild(ta);
            ta.select();
            document.execCommand('copy');
            document.body.removeChild(ta);
            btn.textContent = '✓ Copied!';
            setTimeout(() => { btn.textContent = '📋 Copy'; btn.disabled = false; }, 2000);
        }
    });
    container.insertBefore(btn, container.firstChild);
}
```

Add copy buttons for:
- Request headers (compact JSON)
- Request body (raw content, or pretty-printed if JSON)
- Response headers (compact JSON)
- Response body (raw content, or pretty-printed if JSON)

For headers, add the copy button right after the `<pre>` element containing the headers.

### 3.6. JSON Pretty-Printing

In `renderBodyContent()`, after receiving `bodyData.content`:

```javascript
function tryPrettyPrint(content) {
    if (!content || content.length === 0) return content;
    const trimmed = content.trim();
    if (trimmed[0] !== '{' && trimmed[0] !== '[') return content;
    try {
        const parsed = JSON.parse(trimmed);
        return JSON.stringify(parsed, null, 2);
    } catch (e) {
        return content;
    }
}
```

Call this before setting `pre.textContent`. Note: If the body is ellipsized (contains `…` characters), `JSON.parse` will fail and the raw content is shown — this is correct behavior since ellipsized content is not valid JSON anyway.

### 3.7. Keyboard Hint Text

Add to `index.html`, just before the closing `</div>` of `#app`:
```html
<div class="keyboard-hint">press <kbd>h</kbd> to show help</div>
```

CSS:
```css
.keyboard-hint {
    position: fixed;
    bottom: 0.5rem;
    left: 0.75rem;
    font-size: 0.75rem;
    color: var(--border);
    opacity: 0.6;
    pointer-events: none;
}
```

Adjust color for dark mode in the media query.

---

## 4. Implementation Order (Suggested)

1. **Keyboard hint text + help overlay HTML/CSS** — low-risk, purely additive
2. **Main-view keyboard shortcuts (j/k/Enter, /, R, h)** — core navigation
3. **Detail-view keyboard shortcuts (ESC, u, v, c, t)** — core detail interaction
4. **Bodies loaded by default (with size threshold)** — changes default behavior
5. **Copy-to-clipboard buttons** — new functionality, no breaking changes
6. **JSON pretty-printing** — enhancement on top of body rendering
7. **Copy keyboard shortcuts (Emacs-inspired `w`-prefix or direct buttons)** — optional polish

---

## 5. Risks & Considerations

- **Browser clipboard API:** `navigator.clipboard.writeText()` requires HTTPS or localhost. The fallback to `execCommand('copy')` covers HTTP but is deprecated. This is acceptable for a development/local tool.
- **Keyboard shortcut conflicts:** The `/` key must focus the query input even when typing in it; all other shortcuts must be suppressed when the query input is focused.
- **Large body auto-loading:** The 128 KiB threshold should be configurable in the future via a JS constant at the top of the file for easy tweaking.
- **Accessibility:** Keyboard shortcuts should have `aria-label` descriptions and the help overlay should be focus-trapped (nice-to-have, not required for v1).
- **Mobile/tablet:** Keyboard shortcuts are inherently desktop-oriented; the existing mouse-driven UI should remain fully functional.
- **No server-side changes required:** All changes are purely client-side (HTML, CSS, JS). The existing API endpoints (`/api/logs`, `/api/logs/:id`, `/api/logs/:id/body`, `/api/logs/:id/thread`, `/api/recording`, `/health`) are sufficient.
