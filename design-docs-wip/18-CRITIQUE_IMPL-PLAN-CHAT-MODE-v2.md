# CRITIQUE: IMPLEMENTATION PLAN FOR CHAT MODE SPECIALIZATION (v2)

**Document Type:** Design Review & Critique  
**Reviewed Document:** `17-IMPL-PLAN-CHAT-MODE-v2.md`  
**Date:** 2026-03-24  
**Reviewer:** AI Code Assistant

---

## Executive Summary

The implementation plan is **fundamentally sound** and addresses the two critical UX issues (unreadable JSON arrays and broken FTS on streams) with well-reasoned architectural decisions. The plan demonstrates strong technical judgment, particularly in:

1. **Decision A (Sidecar Text):** Correctly prioritizes debuggability over storage efficiency
2. **Decision B (Lineage without Deduplication):** Avoids premature optimization that could introduce brittleness
3. **Decision C (Text-Only MVP):** Sensible scope limitation for initial release

However, the plan suffers from **moderate under-specification** in several critical areas. A junior engineer would likely complete 70-80% of the implementation correctly but would require significant guidance on edge cases, error handling, and several architectural decisions that are left implicit.

**Overall Assessment:** ✅ **Feasible with revisions** — The plan is technically sound but requires additional detail before implementation.

---

## 1. Strengths of the Plan

### 1.1 Architectural Decisions

| Decision | Assessment | Rationale |
|----------|-----------|-----------|
| **Sidecar Text for FTS** | ✅ Excellent | Preserves raw bytes for debugging while solving the chunking problem. The separation of concerns is clean. |
| **Lineage via Hashing** | ✅ Strong | Cryptographic hashes provide deterministic linking without requiring session IDs from clients. |
| **No Deduplication** | ✅ Pragmatic | Correctly identifies that reconstruction brittleness outweighs storage benefits. |
| **Text-Only MVP** | ✅ Wise | Avoids scope creep; complex content types can be handled in future iterations. |

### 1.2 Database Design

- Using `parent_id` + `chat_hash` is the correct approach for thread traversal
- FTS5 repointing to sidecar columns is the minimal viable change
- Adding `idx_chat_hash` index is necessary and well-identified

### 1.3 UI Strategy

- Consistent interaction model across all three frontends (Emacs, Web, TUI)
- Clear escape hatch to raw mode for complex content
- Log ID navigation from thread view back to raw bytes is well-considered

---

## 2. Critical Gaps & Under-Specification

### 2.1 HIGH SEVERITY: Lineage Hashing Algorithm Ambiguity

**Location:** Section 4, "Lineage Tracking"

**Problem:** The plan states:
> "Compute `hash := HashMessages(M)`. Save as `entry.ChatHash`."

But fails to specify:
1. **What JSON serialization format?** (compact vs. pretty-print, key ordering, whitespace handling)
2. **How to handle optional fields?** (e.g., `name`, `function_call` may be null vs. absent)
3. **How to handle floating-point numbers?** (e.g., `temperature: 0.5` vs `0.5000000001`)

**Risk:** Two requests with semantically identical messages but different JSON encodings will produce different hashes, breaking thread linkage.

**Recommendation:**
```go
// Specify canonical JSON marshaling
func HashMessages(messages []Message) string {
    // 1. Sort map keys
    // 2. Use json.Marshal (compact, no whitespace)
    // 3. Normalize null vs. absent fields
    // 4. Hash the canonical JSON bytes
    canonical, _ := json.Marshal(messages) // json.Marshal sorts keys
    hash := sha256.Sum256(canonical)
    return hex.EncodeToString(hash[:])
}
```

**Missing Specification:** The plan should explicitly state whether the hash includes:
- Only `role` + `content` fields? ✅ (recommended)
- All fields including `name`, `function_call`, `tool_calls`?
- System messages? (These may be injected server-side and vary between requests)

**Action Required:** Add a "Canonical JSON Serialization" subsection to Section 4.

---

### 2.2 HIGH SEVERITY: Parent Linking Algorithm Is Incomplete

**Location:** Section 4, "Lineage Tracking"

**Problem:** The plan states:
> "iterate $i$ backwards from $N-1$ down to 1... Hash the prefix $M[0:i]$ and query the database"

This algorithm has **O(N²)** hash computations per request, where N is the message count. For a conversation with 50 messages, this requires 1,275 hash computations and database queries.

**Additional Issues:**
1. **No fallback strategy:** What if no parent is found? (e.g., first request in a thread, or messages were modified)
2. **No handling for branching conversations:** What if the user edits a previous message and continues from there? Multiple parents could match.
3. **No handling for system messages:** If the system injects a system message differently between requests, the hash won't match.

**Recommendation:**
```go
// Optimized algorithm with caching
func FindParentID(messages []Message, db *sql.DB) (*int64, error) {
    // Edge case: no messages or only system message
    if len(messages) <= 1 {
        return nil, nil
    }
    
    // Try the most likely case first: previous request had N-2 messages
    // (missing the new assistant reply and the new user prompt)
    likelyPrefixLen := len(messages) - 2
    if likelyPrefixLen > 0 {
        prefixHash := HashMessages(messages[:likelyPrefixLen])
        var parentID int64
        err := db.QueryRow(
            "SELECT id FROM openai_logs WHERE chat_hash = ? ORDER BY id DESC LIMIT 1",
            prefixHash,
        ).Scan(&parentID)
        if err == nil {
            return &parentID, nil
        }
    }
    
    // Fallback: try other prefix lengths (rare cases)
    for i := len(messages) - 3; i >= 1; i-- {
        prefixHash := HashMessages(messages[:i])
        var parentID int64
        err := db.QueryRow(
            "SELECT id FROM openai_logs WHERE chat_hash = ? ORDER BY id DESC LIMIT 1",
            prefixHash,
        ).Scan(&parentID)
        if err == nil {
            return &parentID, nil
        }
    }
    
    return nil, nil // No parent found
}
```

**Action Required:**
1. Specify the optimized algorithm (try most likely prefix first)
2. Add explicit handling for "no parent found" case (set `parent_id = NULL`)
3. Document expected behavior for branching conversations (last match wins)

---

### 2.3 MEDIUM SEVERITY: Thread Assembly Logic Is Underspecified

**Location:** Section 5, "Thread Assembly"

**Problem:** The plan states:
> "For *every subsequent* `LogEntry` in the slice, diff its `ReqBody.messages` against the previous entry's `ReqBody.messages` to find the *new* User message"

This "diff" operation is **non-trivial** and underspecified:
1. What if messages were reordered? (unlikely but possible with some SDKs)
2. What if the user edited a previous message? (message content changed, not appended)
3. What if there are multiple new messages? (batch requests)

**Current Plan's Approach:**
```
Root: [M1, M2, M3] → User: M1, M2, M3 | Assistant: A3
Turn 2: [M1, M2, M3, A3, M4] → User: M4 | Assistant: A4
Turn 3: [M1, M2, M3, A3, M4, A4, M5] → User: M5 | Assistant: A5
```

**Issue:** The diff algorithm assumes messages are only appended. If a user edits M2 and resends, the diff will incorrectly identify M2 as "new."

**Recommendation:**
```go
// Explicit diff algorithm specification
func FindNewUserMessages(prevMessages, currMessages []Message) []Message {
    // Simple approach: find messages in curr that are not in prev
    // Use hash comparison for efficiency
    prevHashes := make(map[string]bool)
    for _, m := range prevMessages {
        h := hashMessage(m)
        prevHashes[h] = true
    }
    
    var newMessages []Message
    for _, m := range currMessages {
        h := hashMessage(m)
        if !prevHashes[h] && m.Role == "user" {
            newMessages = append(newMessages, m)
        }
    }
    return newMessages
}

func hashMessage(m Message) string {
    // Hash only role + content for comparison
    data := struct {
        Role    string `json:"role"`
        Content string `json:"content"`
    }{Role: m.Role, Content: m.Content}
    canonical, _ := json.Marshal(data)
    hash := sha256.Sum256(canonical)
    return hex.EncodeToString(hash[:])
}
```

**Action Required:** Add pseudocode for the diff algorithm in Section 5.

---

### 2.4 MEDIUM SEVERITY: Complex Content Detection Is Vague

**Location:** Section 2.3 (Decision C) and Section 5

**Problem:** The plan states:
> "If a message contains complex multi-modal arrays, tool-calls, or function-calls, the UI will display a placeholder"

But fails to specify:
1. **Exact detection criteria:** What JSON structure indicates "complex"?
2. **Partial complexity:** What if only one message in a turn is complex?
3. **Tool call streaming:** Some APIs stream tool calls separately from content.

**Recommendation:**
```go
// Explicit detection logic
type MessageComplexity struct {
    IsComplex bool
    Reason    string // "tool_calls", "function_call", "content_array", "image_url", "unknown_type"
}

func DetectMessageComplexity(msg Message) MessageComplexity {
    // Check for tool_calls array
    if len(msg.ToolCalls) > 0 {
        return MessageComplexity{true, "tool_calls"}
    }
    
    // Check for function_call object
    if msg.FunctionCall != nil {
        return MessageComplexity{true, "function_call"}
    }
    
    // Check for content array (vs. string)
    if msg.ContentArray != nil {
        // Check if any array element is non-text
        for _, item := range msg.ContentArray {
            if item.Type != "text" {
                return MessageComplexity{true, item.Type} // "image_url", "input_audio", etc.
            }
        }
        // All items are text, so not complex
        return MessageComplexity{false, ""}
    }
    
    // Content is a string (simple case)
    return MessageComplexity{false, ""}
}
```

**Action Required:** Add explicit JSON structure examples for "complex" vs. "simple" messages.

---

### 2.5 MEDIUM SEVERITY: FTS Trigger Update Missing Detail

**Location:** Section 3, "Update `ftsSchema`"

**Problem:** The plan states:
> "Update the triggers to insert `new.req_text` and `new.resp_text`"

But the current schema has **two triggers** (`openai_logs_ai` for INSERT and `openai_logs_ad` for DELETE), and the plan only mentions updating one.

**Current Schema:**
```sql
CREATE TRIGGER IF NOT EXISTS openai_logs_ai AFTER INSERT ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(rowid, req_body, resp_body)
    VALUES (new.id, new.req_body, new.resp_body);
END;

CREATE TRIGGER IF NOT EXISTS openai_logs_ad AFTER DELETE ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(openai_logs_fts, rowid, req_body, resp_body)
    VALUES ('delete', old.id, old.req_body, old.resp_body);
END;
```

**Required Update:**
```sql
-- UPDATE trigger (not shown in plan!)
CREATE TRIGGER IF NOT EXISTS openai_logs_ai AFTER INSERT ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(rowid, req_text, resp_text)
    VALUES (new.id, new.req_text, new.resp_text);
END;

-- DELETE trigger (also needs update!)
CREATE TRIGGER IF NOT EXISTS openai_logs_ad AFTER DELETE ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(openai_logs_fts, rowid, req_text, resp_text)
    VALUES ('delete', old.id, old.req_text, old.resp_text);
END;
```

**Additional Issue:** What about **UPDATE** triggers? If `req_text` or `resp_text` is updated (e.g., bug fix, reprocessing), the FTS index won't reflect changes without an UPDATE trigger.

**Recommendation:** Add an UPDATE trigger:
```sql
CREATE TRIGGER IF NOT EXISTS openai_logs_au AFTER UPDATE ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(openai_logs_fts, rowid, req_text, resp_text)
    VALUES ('delete', old.id, old.req_text, old.resp_text);
    INSERT INTO openai_logs_fts(rowid, req_text, resp_text)
    VALUES (new.id, new.req_text, new.resp_text);
END;
```

**Action Required:** Specify all three triggers (INSERT, UPDATE, DELETE) in the plan.

---

### 2.6 LOW SEVERITY: Recursive CTE Direction Is Confusing

**Location:** Section 5, "GetThread"

**Problem:** The plan states:
> "Note: The CTE naturally returns the root first if ordered by ID ASC"

This is **incorrect**. The CTE as written:
```sql
WITH RECURSIVE thread AS (
  SELECT * FROM openai_logs WHERE id = ?  -- Starts from the given ID (leaf)
  UNION ALL
  SELECT o.* FROM openai_logs o
  INNER JOIN thread t ON t.parent_id = o.id  -- Traverses UP to parents
)
SELECT * FROM thread ORDER BY id ASC;
```

This traverses **from leaf to root** (child → parent → grandparent), then sorts by ID ASC. While this does produce chronological order, the explanation is misleading.

**Clarification Needed:**
- The CTE starts from the selected log entry (which could be any turn in the conversation)
- It traverses **upward** via `parent_id` to find all ancestors
- The `ORDER BY id ASC` ensures chronological display (root first, then children)

**Edge Case Not Addressed:** What if the user wants to see the thread starting from a **middle** message? Currently, the CTE only shows ancestors, not descendants.

**Example:**
```
M1 → M2 → M3 → M4 → M5
              ↑
         (user selects M3)
```

Current behavior: Shows M1, M2, M3 (ancestors only)  
Expected behavior: Should show M1, M2, M3, M4, M5 (entire thread)

**Recommendation:** Use a bidirectional CTE:
```sql
WITH RECURSIVE thread AS (
  -- Anchor: start from the selected log
  SELECT * FROM openai_logs WHERE id = ?
  
  UNION ALL
  
  -- Traverse UP to ancestors
  SELECT o.* FROM openai_logs o
  INNER JOIN thread t ON t.parent_id = o.id
  
  UNION ALL
  
  -- Traverse DOWN to descendants
  SELECT o.* FROM openai_logs o
  INNER JOIN thread t ON o.parent_id = t.id
)
SELECT * FROM thread ORDER BY id ASC;
```

**Action Required:** Clarify the CTE behavior and decide whether to support viewing entire threads or only ancestor chains.

---

### 2.7 LOW SEVERITY: Error Handling Is Missing

**Location:** Throughout the document

**Problem:** The plan does not specify error handling for:
1. **JSON parsing failures:** What if `ReqBody` is malformed JSON?
2. **Database query failures:** What if the parent lookup query fails?
3. **Stream parsing failures:** What if `streamview.Build` returns `ReasonParseFailed`?
4. **API handler errors:** What HTTP status codes should be returned for various errors?

**Recommendation:** Add an "Error Handling" subsection to each implementation section:
```go
// Example: Sidecar Text Generation with error handling
func PopulateSidecarText(entry *LogEntry) {
    if entry.RequestPath != "/v1/chat/completions" {
        entry.ReqText = ""
        return
    }
    
    var req ChatCompletionRequest
    if err := json.Unmarshal([]byte(entry.ReqBody), &req); err != nil {
        slog.Warn("failed to parse request body for sidecar text", "error", err)
        entry.ReqText = "" // Leave empty, don't fail the insert
        return
    }
    
    entry.ReqText = ExtractRequestText(req.Messages)
    
    // Response text from streamview
    sv := streamview.Build(entry)
    if sv.AssembledAvailable {
        entry.RespText = sv.AssembledBody
    } else {
        // Fallback to raw body
        if entry.RespBody != nil {
            entry.RespText = *entry.RespBody
        } else {
            entry.RespText = ""
        }
    }
}
```

**Action Required:** Add explicit error handling specifications for each component.

---

## 3. Missed Opportunities

### 3.1 No Migration Strategy for Existing Data

**Problem:** The plan states "backward compatibility is not required," but this means existing logs won't have `req_text`, `resp_text`, `parent_id`, or `chat_hash`. FTS searches on existing logs will fail.

**Recommendation:** Add a one-time migration script:
```sql
-- Migration: Populate sidecar text for existing chat completion logs
-- (Run as background job, not blocking)

-- Step 1: Add columns (already in plan)
-- Step 2: Backfill req_text and resp_text
-- This would need to be done in application code since it requires JSON parsing

-- Step 3: Rebuild FTS index after backfill
INSERT INTO openai_logs_fts(openai_logs_fts) VALUES('rebuild');
```

**Action Required:** Add a "Migration" section with a backfill strategy.

---

### 3.2 No Performance Benchmarks or Limits

**Problem:** The plan doesn't specify:
1. **Maximum conversation length:** What happens if a thread has 500 messages?
2. **Maximum message size:** What if a single message is 100KB?
3. **Query timeout:** How long should the parent lookup query wait?

**Recommendation:** Add explicit limits:
```go
const (
    MaxMessagesForLineage = 100      // Don't hash more than 100 messages
    MaxMessageSize = 32 * 1024       // 32KB per message
    ParentLookupTimeout = 5 * time.Second
    MaxThreadDepth = 200             // Max messages to return in thread view
)
```

**Action Required:** Add a "Limits & Performance" subsection.

---

### 3.3 No Testing Strategy

**Problem:** The plan doesn't specify:
1. **Unit tests:** What functions need unit tests?
2. **Integration tests:** How to test thread assembly end-to-end?
3. **Test data:** How to generate realistic conversation histories?

**Recommendation:** Add a "Testing" section:
```
## 7. Testing

### 7.1 Unit Tests
- `internal/chat/hash_test.go`: Test `HashMessages` determinism
- `internal/chat/hash_test.go`: Test `ExtractRequestText` with various message types
- `internal/database/writer_test.go`: Test parent linking with mock conversations

### 7.2 Integration Tests
- `test/chat-thread-integration_test.go`: Simulate a multi-turn conversation
  and verify thread assembly returns correct linear view

### 7.3 Test Data
- Create fixture: `test/fixtures/sample-conversation.json` with 10-turn conversation
```

**Action Required:** Add a "Testing" section to the plan.

---

### 3.4 No Observability or Debugging Tools

**Problem:** The plan doesn't specify:
1. **Logging:** What should be logged for debugging lineage issues?
2. **Metrics:** How to track parent lookup success rate?
3. **Debug endpoints:** How to inspect why two messages didn't link?

**Recommendation:**
```go
// Add logging for lineage tracking
func FindParentID(messages []Message, db *sql.DB) (*int64, error) {
    start := time.Now()
    defer func() {
        slog.Debug("parent lookup completed", "duration", time.Since(start))
    }()
    
    // ... lookup logic ...
    
    if parentID == nil {
        slog.Debug("no parent found for conversation",
            "message_count", len(messages),
            "prefix_hash", prefixHash)
    } else {
        slog.Debug("parent found",
            "parent_id", *parentID,
            "prefix_len", likelyPrefixLen)
    }
}

// Add metrics
var (
    parentLookupSuccess = promauto.NewCounter(...)
    parentLookupFailure = promauto.NewCounter(...)
    parentLookupDuration = promauto.NewHistogram(...)
)
```

**Action Required:** Add an "Observability" subsection.

---

## 4. Suggestions for Improvement

### 4.1 Consider Adding `conversation_id` Column

**Rationale:** While hash-based lineage is elegant, some OpenAI-compatible APIs include an explicit `conversation_id` or `thread_id` parameter. Having a dedicated column would:
1. Allow explicit thread grouping when available
2. Simplify debugging (human-readable IDs vs. hashes)
3. Enable future features like thread naming, tagging, etc.

**Recommendation:** Add optional column:
```sql
conversation_id TEXT, -- Optional, for APIs that provide explicit thread IDs
```

---

### 4.2 Add `message_count` Column for Quick Filtering

**Rationale:** Being able to quickly filter by conversation length would be useful:
```sql
message_count INTEGER, -- Number of messages in this turn's request

-- Query: Show only conversations with more than 5 turns
SELECT * FROM openai_logs WHERE message_count > 5
```

---

### 4.3 Consider Caching Hash Computations

**Rationale:** The parent lookup algorithm computes multiple SHA-256 hashes. For long conversations, this could be expensive.

**Recommendation:** Cache prefix hashes:
```go
func ComputePrefixHashes(messages []Message) []string {
    hashes := make([]string, len(messages)+1)
    var buf bytes.Buffer
    
    for i := 0; i <= len(messages); i++ {
        if i == 0 {
            hashes[i] = hashEmpty()
        } else {
            buf.Reset()
            canonical, _ := json.Marshal(messages[i-1])
            buf.Write(canonical)
            hashes[i] = hashBytes(buf.Bytes())
        }
    }
    return hashes
}
```

---

### 4.4 Add Thread Metadata to List View

**Rationale:** The current plan only shows thread info in the detail view. Adding thread metadata to the list view would improve discoverability:
```go
// Add to LogSummary
type LogSummary struct {
    // ... existing fields ...
    IsChatCompletion bool    `json:"is_chat_completion"`
    ThreadSize       *int    `json:"thread_size"` // Number of messages in thread
    IsThreadRoot     bool    `json:"is_thread_root"`
}
```

---

## 5. Junior Engineer Readiness Assessment

### 5.1 What's Clear ✅

| Topic | Clarity | Notes |
|-------|---------|-------|
| Database schema changes | ✅ Clear | Column names and types are explicit |
| FTS repointing | ✅ Clear | SQL is provided |
| New file locations | ✅ Clear | File paths are specified |
| UI interaction model | ✅ Clear | Key bindings and behavior described |

### 5.2 What's Ambiguous ⚠️

| Topic | Clarity | Risk |
|-------|---------|------|
| Hash algorithm | ❌ Unclear | High risk of incorrect implementation |
| Parent lookup optimization | ❌ Missing | Risk of O(N²) performance |
| Thread assembly diff | ⚠️ Vague | Risk of incorrect message detection |
| Complex content detection | ⚠️ Vague | Risk of inconsistent UI behavior |
| Error handling | ❌ Missing | Risk of silent failures |
| FTS trigger updates | ⚠️ Incomplete | Risk of FTS index corruption |

### 5.3 What's Missing ❌

| Topic | Impact |
|-------|--------|
| Migration strategy | Existing data won't work |
| Testing plan | No quality assurance |
| Observability | Hard to debug issues |
| Performance limits | Risk of DoS via long conversations |
| API error codes | Inconsistent error responses |

### 5.4 Readiness Score

| Category | Score | Notes |
|----------|-------|-------|
| **Architecture** | 9/10 | Sound decisions, well-reasoned |
| **Database Design** | 8/10 | Minor gaps in trigger specs |
| **Backend Implementation** | 6/10 | Missing critical algorithm details |
| **Frontend Implementation** | 7/10 | Clear enough for implementation |
| **Error Handling** | 3/10 | Largely absent |
| **Testing** | 2/10 | Not addressed |
| **Migration** | 2/10 | Not addressed |
| **Overall** | **5.3/10** | **Not ready for junior engineer** |

---

## 6. Recommended Revisions

### 6.1 Critical (Must Have Before Implementation)

1. **Add "Canonical JSON Serialization" subsection** to Section 4
   - Specify exact marshaling options
   - Provide example input/output

2. **Add "Parent Lookup Algorithm" pseudocode** to Section 4
   - Include optimization (try likely prefix first)
   - Specify fallback behavior

3. **Add "Thread Assembly Diff Algorithm"** to Section 5
   - Provide explicit pseudocode
   - Include edge case handling

4. **Complete FTS trigger specifications** in Section 3
   - Include INSERT, UPDATE, DELETE triggers
   - Provide full SQL

5. **Add "Error Handling" subsections** to Sections 4-5
   - Specify behavior for each failure mode
   - Include logging requirements

### 6.2 Important (Should Have)

6. **Add "Migration Strategy" section**
   - Backfill plan for existing data
   - FTS rebuild instructions

7. **Add "Testing" section**
   - Unit test requirements
   - Integration test scenarios
   - Test data fixtures

8. **Add "Limits & Performance" section**
   - Maximum conversation length
   - Query timeouts
   - Rate limiting considerations

### 6.3 Nice to Have

9. **Add "Observability" section**
   - Logging requirements
   - Metrics to track
   - Debug endpoints

10. **Add "API Error Codes" section**
    - HTTP status codes for each error type
    - Error response format

---

## 7. Conclusion

The implementation plan demonstrates strong architectural thinking and correctly identifies the core technical challenges. The decisions to preserve raw bytes, use hash-based lineage, and limit initial scope to text-only are all sound.

However, the plan is **not yet detailed enough** for a junior engineer to implement successfully without significant guidance. The critical gaps in the hashing algorithm, parent lookup logic, and error handling would likely result in a buggy or incomplete implementation.

**Recommendation:** Revise the plan to address the "Critical" and "Important" items in Section 6 before beginning implementation. This will require approximately 2-4 hours of additional design work but will save 10-20 hours of rework during implementation.

**Estimated Implementation Effort (after revisions):**
- Backend (DB schema, worker, API): 2-3 days
- Emacs UI: 1 day
- Web UI: 1 day
- TUI: 1 day
- Testing: 1-2 days
- **Total:** 6-8 days

---

## Appendix A: Revised Section 4 (Example)

Here's how Section 4 should read after incorporating the feedback:

---

## 4. Backend Implementation: Async Worker & Lineage Hashing

### 4.1 File: `internal/chat/hash.go` (New File)

#### Data Structures

```go
package chat

import (
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
)

// Message represents a single message in a chat completion request.
// Only role and content are used for hashing and text extraction.
type Message struct {
    Role    string `json:"role"`
    Content string `json:"content"`
    // Other fields (name, tool_calls, etc.) are ignored for lineage tracking
}

// ChatCompletionRequest represents the request body for /v1/chat/completions.
type ChatCompletionRequest struct {
    Messages []Message `json:"messages"`
    // Other fields (model, temperature, etc.) are ignored for lineage tracking
}
```

#### Canonical Hash Function

```go
// HashMessages computes a deterministic SHA-256 hash of the messages array.
// The hash is computed on the canonical JSON representation:
// - Compact (no whitespace)
// - Keys sorted alphabetically (default json.Marshal behavior)
// - Null values included (not omitted)
//
// Example:
//   messages := []Message{{Role: "user", Content: "Hello"}}
//   hash := HashMessages(messages)
//   // Returns: "a1b2c3..." (64-character hex string)
func HashMessages(messages []Message) string {
    // Use json.Marshal for canonical representation
    // json.Marshal sorts keys alphabetically and produces compact output
    canonical, err := json.Marshal(messages)
    if err != nil {
        // This should never happen for valid Message structs
        return ""
    }
    
    hash := sha256.Sum256(canonical)
    return hex.EncodeToString(hash[:])
}
```

#### Text Extraction Function

```go
// ExtractRequestText concatenates all message contents into a single
// space-separated string for FTS indexing.
//
// Example:
//   messages := []Message{
//       {Role: "user", Content: "Hello"},
//       {Role: "assistant", Content: "Hi there!"},
//   }
//   text := ExtractRequestText(messages)
//   // Returns: "Hello Hi there!"
func ExtractRequestText(messages []Message) string {
    var parts []string
    for _, m := range messages {
        if m.Content != "" {
            parts = append(parts, m.Content)
        }
    }
    return strings.Join(parts, " ")
}
```

### 4.2 File: `internal/database/writer.go`

#### Sidecar Text Generation

```go
func (w *LogWriter) populateSidecarText(entry *LogEntry) {
    // Only process chat completion requests
    if entry.RequestPath != "/v1/chat/completions" {
        return
    }
    
    // Parse request body
    var req chat.ChatCompletionRequest
    if err := json.Unmarshal([]byte(entry.ReqBody), &req); err != nil {
        slog.Warn("failed to parse request body for sidecar text",
            "id", entry.ID, "error", err)
        return
    }
    
    // Extract request text
    entry.ReqText = chat.ExtractRequestText(req.Messages)
    
    // Extract response text from assembled stream
    sv := streamview.Build(entry)
    if sv.AssembledAvailable {
        entry.RespText = sv.AssembledBody
    } else if entry.RespBody != nil {
        // Fallback to raw body
        entry.RespText = *entry.RespBody
    }
}
```

#### Lineage Tracking

```go
func (w *LogWriter) populateLineage(entry *LogEntry) {
    // Only process chat completion requests
    if entry.RequestPath != "/v1/chat/completions" {
        return
    }
    
    // Parse request body
    var req chat.ChatCompletionRequest
    if err := json.Unmarshal([]byte(entry.ReqBody), &req); err != nil {
        return // Already logged in populateSidecarText
    }
    
    messages := req.Messages
    if len(messages) == 0 {
        return
    }
    
    // Compute hash for this request's full message list
    entry.ChatHash = chat.HashMessages(messages)
    
    // Find parent by trying prefix lengths (most likely first)
    entry.ParentID = w.findParentID(messages)
}

func (w *LogWriter) findParentID(messages []Message) *int64 {
    // Edge case: single message (no parent possible)
    if len(messages) <= 1 {
        return nil
    }
    
    // Try most likely case first: previous request had N-2 messages
    // (missing the new assistant reply and the new user prompt)
    likelyPrefixLen := len(messages) - 2
    if likelyPrefixLen > 0 {
        prefixHash := chat.HashMessages(messages[:likelyPrefixLen])
        var parentID int64
        err := w.db.QueryRow(
            "SELECT id FROM openai_logs WHERE chat_hash = ? ORDER BY id DESC LIMIT 1",
            prefixHash,
        ).Scan(&parentID)
        if err == nil {
            return &parentID
        }
    }
    
    // Fallback: try other prefix lengths (rare cases like edited messages)
    for i := len(messages) - 3; i >= 1; i-- {
        prefixHash := chat.HashMessages(messages[:i])
        var parentID int64
        err := w.db.QueryRow(
            "SELECT id FROM openai_logs WHERE chat_hash = ? ORDER BY id DESC LIMIT 1",
            prefixHash,
        ).Scan(&parentID)
        if err == nil {
            return &parentID
        }
    }
    
    // No parent found
    return nil
}
```

---

**End of Document**
