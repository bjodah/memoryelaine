This v4 plan is exceptionally robust. The shift to `COALESCE`-based sidecar indexing, the bounded 5-attempt parent search, and the Top-Down Annotation logic are all elegant, production-grade solutions. This is very close to a final blueprint.

Performing a strict logical dry-run of the plan reveals **one major collision flaw regarding tool calls**, one **mathematical tweak needed for the annotation algorithm**, and two minor implementation details to watch out for.

Here are the deficiencies to adjust before writing code:

### 1. The "Complex Message" Hash Collision (Critical)
**Location:** Section 7.1 (Canonicalization Rule)

The proposed `CanonicalMessage` struct drops `ToolCalls` and `FunctionCall` entirely, replacing them with `IsComplex: true`. 
```go
type CanonicalMessage struct {
    Role         string   `json:"role"`
    IsComplex    bool     `json:"is_complex,omitempty"`
    // ...
}
```
**The Bug:** If you do this, *every* tool-call message in your database will canonicalize to the exact same JSON: `{"role":"assistant","is_complex":true}`. If a conversation relies heavily on tools (e.g., fetching weather for Boston, then fetching weather for London), the prefix hashes will collide aggressively because the actual payloads differentiating the turns have been stripped out.

**The Fix:** You need to preserve uniqueness for complex messages without getting bogged down in parsing them deeply.
Update the `CanonicalMessage` struct to include a hash of the raw bytes for complex parts:
```go
type CanonicalMessage struct {
    Role         string   `json:"role"`
    ContentText  string   `json:"content_text,omitempty"`
    ContentParts []string `json:"content_parts,omitempty"`
    Name         string   `json:"name,omitempty"`
    IsComplex    bool     `json:"is_complex,omitempty"`
    ComplexHash  string   `json:"complex_hash,omitempty"` // <-- ADD THIS
}
```
*Rule:* If the message has `tool_calls` or `function_call`, or non-text content, run `json.Compact` on those raw bytes to strip trivial whitespace differences, hash the compacted bytes, and store the result in `ComplexHash`. This guarantees collision resistance for complex turns.

### 2. Top-Down Annotation Index Math (Mathematical Tweak)
**Location:** Section 11.3 (Index attribution logic)

The plan states:
> `Ek` (k > 0): messages `[Ek.parent_prefix_len, Ek.message_count)` are attributed to `Ek`. 

**The Bug:** If a client drops or summarizes messages between turn `Ek` and the final selected turn `En`, using `Ek.message_count` as the upper bound to slice the *selected entry's* message array will cause index out-of-bounds panics or overlaps. 

**The Fix:** Because we are using the *selected entry's* message array, we must assign ownership by walking **backwards** from `En` down to `E0`. 
* `En` owns indices `[En.parent_prefix_len, len(selected_messages))`
* `En-1` owns indices `[En-1.parent_prefix_len, En.parent_prefix_len)`
* ...and so on. 
This guarantees every message in the final array is attributed to exactly one turn, with zero overlaps and zero out-of-bounds panics, regardless of how the client mutated the history.

### 3. Historical Assistant Truth vs. Client Echo (UX/Documentation)
**Location:** Section 11.3

By using Top-Down Annotation (which is the correct choice!), the UI will render the historical assistant messages *exactly as the client echoed them back in the final request*. 
If the client truncated or modified the assistant's previous response to save tokens, the Conversation UI will show the modified version, not the original bytes the proxy sent.
**The Fix:** This is actually a good thing—it shows exactly what context the LLM received for the current turn. However, you should add a note to the frontend spec (Section 12) stating that hovering/clicking a historical message's `(Log #N)` link allows the user to see the "true" historical response if they suspect the client mangled it. 

### 4. `COALESCE` and `NULL` vs. `""` (Implementation Guardrail)
**Location:** Section 8.1 & 9

**The Bug:** In Go, if a string field is empty, it defaults to `""`. If the database driver inserts `""` (an empty string) into `req_text`, the SQL `COALESCE(req_text, req_body)` will evaluate to `""`, effectively erasing that log from the FTS index!
**The Fix:** Ensure the backend developer uses `*string` pointers in the `LogEntry` model and explicitly passes `nil` when the sidecar text is not generated. SQLite requires actual `NULL` values for `COALESCE` to fall back to `req_body`. 

---

### Final Verdict
With those minor adjustments (specifically adding `ComplexHash` and walking the indices backwards), this plan is airtight. You are ready to start building!
