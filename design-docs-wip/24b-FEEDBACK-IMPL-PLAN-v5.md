This is a highly pragmatic, well-engineered implementation plan. The shift from read-time diffing to write-time prefix hashing with backward attribution (Section 11.3) elegantly solves the major complexities of chat thread reconstruction (double-echoes, missing turns, history rewriting) while preserving the zero-latency goals.

However, a close critical dissection reveals one **critical logical bug** lurking in the hash search, a few underspecified areas in the Emacs UX, and some missing edge cases in the testing strategy. 

Here is a detailed breakdown, including feedback on your documentation suggestion.

---

### 1. Critical Bug: The "Empty Prefix" Hash Collision
In **Section 7.4 (Parent Lookup)**, the plan states:
> *Then try remaining prefixes from N-1 down to 1, skipping N-2, stopping after 5 total attempts.*

**The Bug:** If `N = 1` (a brand new conversation with just one `user` message), `N-1` is `0`. If you compute the canonical hash of an empty array `[]`, it will yield a static SHA-256 hash. Because your lookup uses `ORDER BY id DESC LIMIT 1`, **every new 1-message conversation will automatically link to the most recently recorded 1-message conversation** as its parent. 

This will create a massive, tangled linked list of unrelated "turn 1" requests.

**The Fix:** Explicitly state in Section 7.4:
* *"Prefix search must abort if the prefix length is `0`. A sequence of zero messages must never be hashed or queried, as it will cause cross-conversation collisions."*

### 2. Algorithmic Edge Cases & Limits
* **The "Cap at 5" Heuristic (Section 7.4):** Capping at 5 is a smart way to bound DB costs. However, in modern agentic loops (where an LLM might execute 5-10 invisible tool calls before yielding to the user), `N` can easily grow by 10+ messages between proxied turns. If this happens, the thread breaks. 
  * *Recommendation:* This is fine for the MVP, but explicitly mention in the design doc that "High-volume agentic tool loops that append >5 messages between proxied requests will break into separate threads."
* **Backward Attribution Clamping (Section 11.3):** The clamping logic is brilliant and prevents index out-of-bounds panics if a client rewrites history. 
  * *Recommendation:* The plan correctly defines how to clamp (`lower = min(Ek.parent_prefix_len, cursor)`), but it needs to ensure that if `E1` is clamped to 0, `E0` also seamlessly processes an empty range `[0, 0)`.

### 3. Emacs UX: Underspecified Areas
Section 12.1 correctly outlines the Emacs requirements, but as an implementation guide for Elisp, it is too vague and risks resulting in a clunky UI. 

To ensure the Emacs experience feels native and polished, add these specific requirements to **Section 12.1**:
1. **New Major Mode:** Specify the creation of `memoryelaine-thread-mode` (derived from `special-mode` or `tabulated-list-mode`), distinct from `memoryelaine-show-mode`. 
2. **Text Buttons:** Explicitly specify using Emacs' `make-text-button` or `define-button-type` for the `(Log #N)` links so that hitting `RET` on them works naturally, mimicking `help-mode` cross-references.
3. **Face Definitions:** Specify the creation of dedicated faces (e.g., `memoryelaine-role-user-face`, `memoryelaine-role-system-face` inheriting from `shadow`, `memoryelaine-role-assistant-face`). Emacs users expect to be able to theme these easily.
4. **Navigation Bindings:** Standardize bindings in the thread view: `q` to `quit-window`, `g` to `memoryelaine-thread-refresh`, and `<tab>` / `<backtab>` to jump between the `(Log #N)` links.

### 4. Database Schema & FTS Nuance
In **Section 4**, the schema updates the FTS table to use `req_text` and `resp_text`:
```sql
CREATE VIRTUAL TABLE IF NOT EXISTS openai_logs_fts USING fts5(
    req_text,
    resp_text, ...
```
Currently in `v3`, the FTS table uses `req_body` and `resp_body`. 
Because you are dropping backward compatibility (Section 5), this rename is safe. Furthermore, because your Go code uses `openai_logs_fts MATCH ?` (which searches all columns in the virtual table), the Go query logic won't break. 
* *Validation:* This is sound, but the plan should explicitly remind the implementer to update the existing `ftsSchema` string in `internal/database/db.go` so they don't accidentally leave the old `req_body` column names in the `CREATE VIRTUAL TABLE` statement.

### 5. Testing Strategy Additions
Section 13 is excellent because it mandates test-driven integration. However, to cover the new algorithms properly, add these specific test cases:

* **Under 13.2 (Writer Tests):** 
  * Add: *"Empty array hashing prevention: Ensure a 1-message request does not query the DB for a 0-length parent prefix."*
* **Under 13.3 (Reader/API Tests):**
  * Add: *"Attribution Clamping: Mock a CTE chain where `parent_prefix_len` is greater than the total `message_count` of the selected entry, verifying the algorithm clamps correctly without panicking."*

### 6. Updating the SPEC Document (Your Suggestion)
Your suggestion is spot-on. Design documents inevitably drift from reality if not explicitly maintained. `12-SPEC-version3.md` is currently the authoritative ground truth for the repository.

**Add a new section at the very end of the Implementation Plan:**

```markdown
## 17. Finalization and Documentation
Once all tests pass and the implementation meets the Acceptance Criteria:
1. Rename `design-docs-main/12-SPEC-version3.md` to `design-docs-main/25-SPEC-version5.md`.
2. Update the new SPEC document to reflect the architectural reality of v5. Specifically:
   - Update the database schema section to include the new lineage and sidecar columns.
   - Update the FTS architecture documentation to explain the `COALESCE` strategy.
   - Document the `/api/logs/{id}/thread` endpoint and the `ThreadResponse` DTO.
   - Update the UI/TUI/Emacs scope sections to officially mandate the Conversation View.
```

### Summary Conclusion
The plan is highly robust. The combination of **write-time prefix matching** and **read-time backward attribution** is a massive leap over standard brittle diffing algorithms. 

By fixing the `0-length` prefix bug, tightening up the Emacs UX specifications, and enforcing the SPEC document update at the end, this plan is ready for execution.
