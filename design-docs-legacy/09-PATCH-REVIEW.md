The proposed patch does a great job addressing the most critical bugs, but it introduces a new race condition and leaves a few of the secondary pitfalls unresolved. 

Here is an evaluation of the patch, including what works, what is still overlooked, and how to fix the remaining issues.

### What the Patch Fixes Successfully
1. **The Global Async Staleness Bug:** By removing `memoryelaine-http--generation` and returning the process object instead, you have successfully shifted staleness tracking to the caller (`memoryelaine-state--detail-generation`), which operates buffer-locally. The `req` and `resp` parallel fetches will now both succeed.
2. **Full Fetch (`t` command) for Assembled Views:** The patch correctly fires a third request for `"assembled"` `full` when the user triggers a full fetch.
3. **Startup Recording State:** Calling `memoryelaine-search--fetch-recording-state` on load fixes the false `●REC` indicator.
4. **PUT Auth Consistency:** The 401 check and cache clearing is now properly mirroring the GET logic.
5. **JSON Pretty Printing:** `memoryelaine-show--maybe-pretty-print-json` uses a safe `condition-case` wrapper. If the body is an SSE stream that happens to start with `{` (but isn't valid JSON), it will gracefully fall back to raw text. This is a very clean addition.

---

### What is Still Overlooked / New Bugs Introduced

#### 1. `memoryelaine-http-cancel-all` kills requests globally (New Bug)
You added `(add-hook 'kill-buffer-hook #'memoryelaine-http-cancel-all nil t)` to the detail buffer.
* **The Problem:** `memoryelaine-http--active-processes` is a **global** list. If a user is typing a live search in `*memoryelaine*`, and they concurrently close the `*memoryelaine-entry*` detail buffer (perhaps in a split window), `cancel-all` will violently kill the background search processes. 
* **The Fix:** Make the active process list buffer-local, or filter the list by the buffer that initiated it. 
  ```elisp
  ;; Change to buffer-local variable
  (defvar-local memoryelaine-http--active-processes nil)
  ```

#### 2. Legacy `q=` Search Still Vulnerable to FTS5 500 Errors
While your fix to the new query DSL (`internal/query/sql.go`) works perfectly because it tokenizes and wraps spaces in quotes, your fix to the legacy parameter in `internal/database/reader.go` (`sanitizeFTS5Input`) is flawed.
* **The Problem:** `sanitizeFTS5Input` processes the *entire* string at once. If a user queries `/api/logs?q=error OR`, the function strips control characters but leaves `"error OR"`. Because it isn't wrapped in quotes, FTS5 sees a dangling `OR` binary operator and will still throw an `fts5: syntax error near OR`, resulting in an HTTP 500.
* **The Fix:** In `internal/database/reader.go`, reuse the tokenization logic. Split the legacy input by spaces, sanitize each token, and drop standalone keywords before rejoining:
  ```go
  func sanitizeFTS5Input(s string) string {
      // Simplest fix: just split by spaces, sanitize, and drop dangling operators
      parts := strings.Fields(s)
      var valid []string
      for _, p := range parts {
          clean := sanitizeFTS5Token(p) // reuse the logic from sql.go
          if clean != "" {
              valid = append(valid, clean)
          }
      }
      return strings.Join(valid, " ")
  }
  ```

#### 3. Blocking Auth Prompts in Background Tasks
* **The Problem:** If a user's basic auth credentials expire or are wrong, a background `live-search` request will hit a 401, clear the cache, and the *next* idle keystroke will trigger `memoryelaine-auth-get-credentials`, which will yank the user's focus into a `read-passwd` minibuffer prompt mid-sentence.
* **The Fix:** Background/timer-driven requests should bypass the interactive prompt. You could add an optional `interactive-p` flag to your HTTP/Auth functions. If `nil`, `memoryelaine-auth--prompt` is skipped, the request fails gracefully, and a message instructs the user to hit `g` (refresh) to re-authenticate manually.

#### 4. Detail Buffer Navigation (`n`/`p`) 
* **The Problem:** Navigating through multiple logs still requires quitting the detail buffer, moving the cursor down, and hitting enter again.
* **The Fix:** (Optional but highly recommended for UX). Add `n` and `p` to `memoryelaine-show-mode-map` that looks at `memoryelaine-state--offset` in the search buffer and automatically calls `memoryelaine-show-entry` on the adjacent ID.
