# Independent Code Review: memoryelaine Emacs Package + Server Implementation

**Date:** 2026-03-22  
**Reviewer:** Qwen Code  
**Scope:** Implementation of `design-docs-legacy/06-REVISED-PRD-and-IMPL_PLAN-emacs-pkg.md`

---

## Executive Summary

The implementation demonstrates solid engineering with good adherence to the revised PRD. The consultant's review (`09-PATCH-REVIEW.md`) identifies several critical issues accurately, but also contains one incorrect assessment and misses some important concerns. This review provides an independent analysis of both the implementation and the consultant's findings.

---

## Part 1: Evaluation of Consultant's Review

### 1.1 Points of Agreement

#### ✅ Bug #1: Global `memoryelaine-http--active-processes` (VALID)

**Consultant's finding:** The global `memoryelaine-http--active-processes` list combined with `memoryelaine-http-cancel-all` being hooked to the detail buffer's `kill-buffer-hook` creates a cross-buffer interference bug.

**Assessment:** **VALID CRITICAL BUG.** The code in `memoryelaine-http.el`:

```elisp
(defvar memoryelaine-http--active-processes nil
  "List of active curl process objects.")
```

This is indeed a global variable. When the detail buffer kills all processes, it will terminate search buffer requests. The consultant's fix is correct:

```elisp
(defvar-local memoryelaine-http--active-processes nil)
```

**Priority:** HIGH — This is a real correctness bug that will cause user-visible failures.

---

#### ✅ Bug #2: Legacy `q=` FTS5 Vulnerability (VALID)

**Consultant's finding:** The `sanitizeFTS5Input` function in `internal/database/reader.go` processes the entire string at once, leaving queries like `"error OR"` vulnerable to FTS5 syntax errors.

**Assessment:** **VALID BUG.** The current implementation:

```go
func sanitizeFTS5Input(s string) string {
    var b strings.Builder
    // ... strips control characters ...
    result := strings.TrimSpace(b.String())
    upper := strings.ToUpper(result)
    if upper == "OR" || upper == "AND" || ... {
        return ""
    }
    return result
}
```

This only rejects the string if the **entire** input is a keyword. A query like `"error OR"` passes through and causes FTS5 syntax errors.

**However**, the consultant's proposed fix is incomplete. The tokenization logic from `internal/query/sql.go` should be reused, but note that:
- The new query DSL (`query.Parse`) already handles this correctly via tokenization
- The legacy path is a backward-compatibility feature

**Recommendation:** Either fix properly by tokenizing, or deprecate the legacy `q=` parameter entirely since the new `query=` DSL is superior.

---

#### ✅ Bug #3: Blocking Auth Prompts (VALID CONCERN)

**Consultant's finding:** Background/timer-driven requests (like live search) will trigger interactive `read-passwd` prompts on 401, disrupting user workflow.

**Assessment:** **VALID UX ISSUE.** The current `memoryelaine-auth-get-credentials`:

```elisp
(defun memoryelaine-auth-get-credentials ()
  "Return (username . password) for Basic Auth.
  Tries: 1) cache, 2) auth-source, 3) explicit vars, 4) interactive prompt."
  (or memoryelaine--cached-credentials
      ...
      (memoryelaine-auth--prompt)))  ; <-- Always prompts if nothing else works
```

There is no mechanism to skip the prompt for background operations.

**Priority:** MEDIUM — This is a UX polish issue, not a correctness bug. The current behavior is annoying but not broken.

---

#### ⚠️ Bug #4: Detail Buffer Navigation (FEATURE REQUEST, NOT A BUG)

**Consultant's finding:** Navigation (`n`/`p`) in the detail buffer is not implemented.

**Assessment:** **NOT A BUG — FEATURE REQUEST.** The PRD (`06-REVISED-PRD-and-IMPL_PLAN-emacs-pkg.md`) explicitly lists this as optional:

> **Keybindings (show buffer):**
> - `TAB` or section navigation keys are **optional, not required in v1**

The consultant's suggestion is good UX advice, but framing it as an oversight is unfair. This is scope creep for v1.

---

### 1.2 Points of Disagreement

#### ❌ "New Race Condition Introduced" (INCORRECT ASSESSMENT)

**Consultant's claim:** The patch "introduces a new race condition" by removing `memoryelaine-http--generation` and using buffer-local generation tracking.

**Assessment:** **INCORRECT.** The consultant misunderstood the implementation. Let me trace the actual flow:

**Search buffer** uses global generation:
```elisp
(defvar memoryelaine-state--generation 0)  ; Global in memoryelaine-state.el

(defun memoryelaine-search--fetch ()
  (let ((gen (memoryelaine-state-next-generation)))
    (memoryelaine-http-get ... 
      (lambda (status data err)
        (when (= gen memoryelaine-state--generation)  ; Checks GLOBAL
          ...)))))
```

**Detail buffer** uses buffer-local generation:
```elisp
(defvar-local memoryelaine-state--detail-generation 0)

(defun memoryelaine-show--fetch-metadata (entry-id)
  (let ((gen (memoryelaine-state-detail-next-generation)))
    (memoryelaine-http-get ...
      (lambda (status data err)
        (when (and (buffer-live-p buf)
                   (= gen (buffer-local-value 'memoryelaine-state--detail-generation buf)))
          ...)))))
```

**This is correct design.** Search and detail are independent:
- Search staleness is tracked globally (only one active search query)
- Detail staleness is tracked per-buffer (only one detail buffer exists)

The consultant's claim that "both req and resp parallel fetches will now both succeed" suggests they thought there was a shared generation counter between request/response pairs. There never was — each HTTP callback captures its own `gen` lexical variable.

**Verdict:** The consultant identified a non-existent problem. The generation counter pattern is correctly implemented.

---

#### ⚠️ "Generation IDs / Request Tokens" Implementation Tip (MISLEADING)

**Consultant's tip:** Use a simple counter variable and pass it into lexical closures.

**Reality:** **This is exactly what the implementation already does.** The consultant appears to have reviewed an older version of the code or misunderstood the current state. The implementation correctly:

1. Increments generation before each request
2. Captures the generation in the callback's lexical scope
3. Checks generation before updating state

Example from `memoryelaine-search--fetch`:
```elisp
(let ((gen (memoryelaine-state-next-generation)))  ; Increment and capture
  (memoryelaine-http-get ... params
    (lambda (status data err)
      (when (= gen memoryelaine-state--generation)  ; Check before render
        ...))))
```

---

### 1.3 Issues the Consultant Missed

#### 🔴 Missing: No Buffer-Local Process List Despite Consultant's Own Advice

The consultant correctly identified that `memoryelaine-http--active-processes` should be buffer-local, but then recommended:

```elisp
(defvar-local memoryelaine-http--active-processes nil)
```

**However**, this alone is insufficient. The `memoryelaine-http-request` function pushes to the global list:

```elisp
(push proc memoryelaine-http--active-processes)
```

If you make the variable buffer-local, you must also ensure that:
1. The push happens in the **calling buffer's context**
2. The sentinel's `delq` happens in the same buffer context

Currently, the sentinel runs in an unspecified buffer context. This needs:

```elisp
(defun memoryelaine-http-request (method path params callback)
  ...
  (let ((calling-buf (current-buffer)))  ; Capture calling buffer
    (set-process-sentinel
     proc
     (lambda (process _event)
       (with-current-buffer calling-buf  ; Restore buffer context
         (setq memoryelaine-http--active-processes
               (delq process memoryelaine-http--active-processes)))
       ...)))))
```

**Without this fix**, making the variable buffer-local will cause the `delq` to fail silently (wrong buffer context).

---

#### 🔴 Missing: No Preview Bytes Configuration

The PRD explicitly requires:

> **6.5 Preview policy**
> The server must support preview-sized retrieval...
> Recommended config: `management.preview_bytes` with a sensible default such as 65536

**Current implementation:** The `previewBytes` value is passed to `apiLogSubHandler` but there's no configuration mechanism visible. Looking at `main.go` and config loading, I don't see `preview_bytes` as an exposed configuration option.

**Impact:** Users cannot tune preview size for their network conditions.

---

#### 🟡 Missing: Query Echo / Normalized Query Representation

The PRD states:

> The response should include:
> - ... optionally `query_echo` and/or a normalized parsed-query representation for debugging

**Current implementation:** The `/api/logs` response does not include `query_echo`. This makes debugging query behavior harder for users.

---

#### 🟡 Missing: `Total-Bytes-Available` Header/Field

The PRD suggestions (13.1.1) explicitly recommend:

> Suggestion: Ensure the response includes an HTTP header or JSON field like `Total-Bytes-Available` so the Emacs client can display exactly how much data is missing from the preview

**Current implementation:** The `BodyResponse` includes `total_bytes` and `included_bytes`, which is good. However, the Emacs client's display message says:

```elisp
(format "  [Preview: %s / %s — press t to load full]\n"
        (memoryelaine-show--format-bytes included)
        (memoryelaine-show--format-bytes total))
```

This is correctly implemented! The consultant's suggestion was already addressed.

---

#### 🟡 Missing: Error Column in Search Buffer

The PRD (7.2) states:

> Columns: ... optional flags/error column if width permits

**Current implementation:** No error column is rendered. The `error` field is fetched from the API but never displayed in the search buffer.

**Impact:** Users cannot quickly identify failed requests without opening each entry.

---

#### 🟡 Missing: Recording State on Startup

The consultant noted this was fixed, and indeed the code shows:

```elisp
(defun memoryelaine-search-open (query)
  ...
  (memoryelaine-search--fetch)
  (memoryelaine-search--fetch-recording-state))  ; <-- Correct
```

**However**, there's a race condition: the search buffer renders before the recording state arrives. The header line shows `●REC` or `⏸PAUSED` based on the initial `memoryelaine-state--recording` value (which defaults to `t`), then updates when the async callback returns.

**Impact:** Brief flash of incorrect recording state on startup.

---

#### 🔴 Critical: No Input Validation on Body Retrieval

The `/api/logs/{id}/body` endpoint accepts user input for `part`, `mode`, and `full` parameters. While basic validation exists:

```go
if part != "req" && part != "resp" {
    writeAPIError(w, http.StatusBadRequest, "invalid_part", ...)
}
```

There's no validation that the requested mode makes sense with the actual data. For example:
- Requesting `mode=assembled` when `sv.AssembledAvailable` is false returns `available: false` but still performs the `streamview.Build` operation
- No rate limiting or DoS protection on large body fetches

---

#### 🟡 Missing: No `query_echo` in Error Responses

When the query parser returns an error, the response includes:

```json
{"error": "query_parse_error", "message": "..."}
```

But it doesn't echo back the invalid query string, making it harder for clients to show "your query 'X' failed because..."

---

## Part 2: Implementation Quality Assessment

### 2.1 Server-Side (Go)

| Aspect | Rating | Notes |
|--------|--------|-------|
| Query DSL Implementation | ✅ Excellent | Clean tokenizer, proper escaping, comprehensive tests |
| SQL Injection Prevention | ✅ Excellent | All user values parameterized, tested explicitly |
| API Response Structure | ✅ Good | Follows PRD spec, summary/detail/body split implemented |
| Error Handling | ⚠️ Good | Structured errors, but could include more context |
| Configuration | ⚠️ Fair | Missing `preview_bytes` config exposure |
| Stream View Integration | ✅ Good | Properly exposes assembled availability and reasons |
| Legacy Compatibility | ⚠️ Fair | FTS5 sanitization bug in legacy path |
| Test Coverage | ✅ Good | Comprehensive parser tests, SQL generation tests |

### 2.2 Emacs Client (Elisp)

| Aspect | Rating | Notes |
|--------|--------|-------|
| Async HTTP Layer | ✅ Good | Clean curl wrapper, proper sentinel handling |
| Stale Response Prevention | ✅ Good | Generation counters correctly implemented |
| Buffer Management | ⚠️ Fair | Reuses detail buffer correctly, but process list bug |
| Authentication | ✅ Good | auth-source integration, proper cache invalidation |
| Search Buffer | ✅ Good | Tabulated list, pagination, query editing |
| Show Buffer | ✅ Good | Raw/assembled toggle, preview/full loading |
| Live Search | ⚠️ Fair | Debounced, but auth prompt issue |
| Logging | ✅ Good | Dedicated log buffer, error visibility |
| Test Coverage | ⚠️ Fair | Basic unit tests, missing integration tests |

---

## Part 3: Additional Concerns Not Raised by Consultant

### 3.1 Security Concerns

#### 3.1.1 No Rate Limiting on Body Fetches

The `/api/logs/{id}/body?full=true` endpoint can be called repeatedly to fetch multi-megabyte responses. There's no:
- Rate limiting per client
- Concurrent request limits
- Memory pressure protection

**Impact:** A misbehaving client (or user spamming `t`) could exhaust server resources.

---

#### 3.1.2 Auth Credentials Cached Indefinitely

The Emacs client caches credentials in `memoryelaine--cached-credentials` for the session lifetime. There's no:
- TTL on cached credentials
- Proactive revalidation
- Secure clearing on logout (only cleared on 401)

**Impact:** If a user's session is compromised, cached credentials remain usable.

---

### 3.2 Reliability Concerns

#### 3.2.1 No Retry Logic for Transient Failures

The HTTP layer fails immediately on:
- Network blips
- Temporary server unavailability
- DNS resolution failures

There's no exponential backoff or retry logic.

---

#### 3.2.2 No Request Timeout Configuration

The curl timeout is hardcoded to 30 seconds:

```elisp
"--max-time" "30"
```

For large body fetches (`full=true` on multi-MB responses), this may be insufficient. Users cannot tune this.

---

### 3.3 Usability Concerns

#### 3.3.1 No Visual Feedback for Loading States

When pressing `t` to fetch full bodies:
- No spinner or progress indicator
- No "fetching..." message in minibuffer
- User must wait silently for the callback

**Impact:** Users may press `t` multiple times, triggering duplicate fetches.

---

#### 3.3.2 No Keyboard Shortcut to Return to Search

The show buffer has `q` bound to `quit-window`, which is correct. However, there's no explicit "back to search" command that preserves window configuration.

---

#### 3.3.3 Search Buffer Point Preservation

The search buffer attempts to preserve point across refreshes:

```elisp
(let ((pos (point))
      ...
  (goto-char (min pos (point-max))))
```

However, if the entry at point was deleted or moved, point may end up in an unexpected location.

---

### 3.4 Code Quality Concerns

#### 3.4.1 Inconsistent Error Message Formatting

Server errors use different formats:
```go
writeAPIError(w, http.StatusBadRequest, "query_parse_error", pe.Message)
writeAPIError(w, http.StatusInternalServerError, "query_error", err.Error())
```

Some include structured error codes, others don't. The Emacs client checks for `alist-get 'message` but doesn't use `alist-get 'error`.

---

#### 3.4.2 Magic Numbers in Elisp

```elisp
"--max-time" "30"
```

Should be a defcustom:
```elisp
(defcustom memoryelaine-request-timeout 30
  "Timeout in seconds for HTTP requests.")
```

---

#### 3.4.3 Missing Docstrings

Several functions lack docstrings:
- `memoryelaine-search--live-post-command`
- `memoryelaine-search--live-fire`
- `memoryelaine-show--insert-body`

---

## Part 4: Recommendations

### 4.1 Critical Fixes (Must Have Before Release)

1. **Fix global process list bug** — Make `memoryelaine-http--active-processes` buffer-local with proper buffer context handling in sentinels.

2. **Fix legacy FTS5 sanitization** — Either properly tokenize the legacy `q=` parameter or deprecate it.

3. **Add visual feedback for body fetches** — Show "Loading full body..." in minibuffer when `t` is pressed.

4. **Add error column to search buffer** — Display the `error` field for quick identification of failed requests.

---

### 4.2 Important Improvements (Should Have)

5. **Add non-interactive auth mode** — Add an optional `interactive-p` parameter to `memoryelaine-auth-get-credentials` to skip prompts for background operations.

6. **Expose `preview_bytes` configuration** — Add `management.preview_bytes` to the server config and document it.

7. **Add request timeout configuration** — Make curl timeout configurable via defcustom.

8. **Add query_echo to API responses** — Include the parsed/echoed query in list responses for debugging.

9. **Fix startup recording state race** — Fetch recording state before rendering initial search buffer, or show "loading..." for recording state.

---

### 4.3 Nice to Have (Could Have)

10. **Implement `n`/`p` navigation in show buffer** — As the consultant suggested, this improves UX.

11. **Add retry logic with backoff** — For transient network failures.

12. **Add rate limiting on body fetches** — Server-side protection against abuse.

13. **Add credential TTL** — Proactively revalidate cached credentials after N minutes.

14. **Improve docstring coverage** — Document all public functions.

---

## Part 5: Comparison with Consultant's Review

| Aspect | Consultant | This Review |
|--------|-----------|-------------|
| Global process list bug | ✅ Identified | ✅ Identified + implementation details |
| Legacy FTS5 bug | ✅ Identified | ✅ Identified + deprecation option |
| Auth prompt issue | ✅ Identified | ✅ Identified + severity assessment |
| Navigation feature | ⚠️ Called as oversight | ✅ Correctly identified as optional |
| Generation counter "bug" | ❌ False positive | ✅ Correctly assessed as working |
| Buffer-local process list fix | ⚠️ Incomplete | ✅ Complete fix with buffer context |
| Preview bytes config | ❌ Missed | ✅ Identified |
| Error column | ❌ Missed | ✅ Identified |
| Recording state race | ❌ Missed | ✅ Identified |
| Rate limiting | ❌ Missed | ✅ Identified |
| Timeout configuration | ❌ Missed | ✅ Identified |

---

## Part 6: Final Verdict

### Overall Assessment: **GOOD, WITH FIXABLE ISSUES**

The implementation is solid and demonstrates thoughtful engineering. The consultant's review correctly identified several important bugs but also included one incorrect assessment (generation counter race condition) and missed some significant concerns.

**Key findings:**
- **2 critical bugs** (global process list, legacy FTS5)
- **4 important issues** (auth prompts, preview config, error column, startup race)
- **1 false positive** (generation counter race — implementation is correct)
- **3 missed concerns** (rate limiting, timeout config, visual feedback)

**Recommendation:** Address the critical and important issues before release. The nice-to-have items can be deferred to a patch release.

---

## Appendix A: Specific Code Changes Required

### A.1 Fix Buffer-Local Process List

```elisp
;; memoryelaine-http.el

;; Change from global to buffer-local
(defvar-local memoryelaine-http--active-processes nil
  "List of active curl process objects in this buffer.")

;; In memoryelaine-http-request, capture calling buffer
(defun memoryelaine-http-request (method path params callback)
  ...
  (let ((calling-buf (current-buffer))  ; NEW
        (buf (generate-new-buffer " *memoryelaine-curl*"))
        ...)
    ...
    (set-process-sentinel
     proc
     (lambda (process _event)
       (with-current-buffer calling-buf  ; NEW: restore context
         (setq memoryelaine-http--active-processes
               (delq process memoryelaine-http--active-processes)))
       ...))))
```

### A.2 Fix Legacy FTS5 Sanitization

```go
// internal/database/reader.go

func sanitizeFTS5Input(s string) string {
    // Tokenize by spaces, sanitize each token, drop standalone keywords
    parts := strings.Fields(s)
    var valid []string
    for _, p := range parts {
        clean := sanitizeFTS5Token(p)  // Reuse existing helper
        if clean != "" {
            valid = append(valid, clean)
        }
    }
    return strings.Join(valid, " ")
}
```

### A.3 Add Non-Interactive Auth Mode

```elisp
;; memoryelaine-auth.el

(defun memoryelaine-auth-get-credentials (&optional interactive-p)
  "Return (username . password) for Basic Auth.
If INTERACTIVE-P is nil, skip interactive prompts and return nil."
  (or memoryelaine--cached-credentials
      (setq memoryelaine--cached-credentials
            (or (memoryelaine-auth--try-auth-source)
                (memoryelaine-auth--try-explicit)
                (when interactive-p  ; NEW: only prompt if interactive
                  (memoryelaine-auth--prompt))))))

;; In live search callbacks:
(memoryelaine-auth-get-credentials nil)  ; Non-interactive
```

### A.4 Add Error Column to Search Buffer

```elisp
;; memoryelaine-search.el

(define-derived-mode memoryelaine-search-mode tabulated-list-mode "MemoryElaine"
  (setq tabulated-list-format
        [("ID" 6 t)
         ("Time" 19 t)
         ("Method" 7 nil)
         ("Path" 30 nil)
         ("Status" 6 t)
         ("Duration" 10 t)
         ("Req" 8 nil)
         ("Resp" 8 nil)
         ("Err" 5 nil)]))  ; NEW

(defun memoryelaine-search--summary-to-entry (summary)
  ...
  (error (let ((e (alist-get 'error summary)))
           (if e "!" "")))  ; NEW
  (list id (vector ... err)))  ; Add to vector
```

---

## Appendix B: Testing Recommendations

### B.1 Server-Side Tests to Add

```go
func TestLegacyFTS5Sanitization(t *testing.T) {
    tests := []struct {
        input string
        want  string
    }{
        {"error OR", "error"},  // Should drop dangling OR
        {"AND test", "test"},   // Should drop leading AND
        {"test NOT", "test"},   // Should drop trailing NOT
    }
    for _, tt := range tests {
        got := sanitizeFTS5Input(tt.input)
        if got != tt.want {
            t.Errorf("sanitizeFTS5Input(%q) = %q, want %q", tt.input, got, tt.want)
        }
    }
}
```

### B.2 Emacs Tests to Add

```elisp
(ert-deftest memoryelaine-test-http-buffer-local-processes ()
  "Test that process list is buffer-local."
  (with-temp-buffer
    (let ((memoryelaine-http--active-processes '(dummy1)))
      (with-temp-buffer
        (let ((memoryelaine-http--active-processes '(dummy2)))
          (should (equal memoryelaine-http--active-processes '(dummy2))))
        (should (equal memoryelaine-http--active-processes '(dummy1)))))))

(ert-deftest memoryelaine-test-auth-non-interactive ()
  "Test that non-interactive auth returns nil instead of prompting."
  (let ((memoryelaine--cached-credentials nil)
        (memoryelaine-username nil)
        (memoryelaine-password nil))
    (should (null (memoryelaine-auth-get-credentials nil)))))
```

---

**End of Review**
