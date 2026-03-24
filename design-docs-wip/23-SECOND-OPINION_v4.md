• Findings

  1. High: the COALESCE FTS design in design-docs-wip/21-IMPL-PLAN-CHAT-MODE-v4.md:229 is not complete because it only covers trigger-time inserts/deletes. The existing rebuild path in internal/database/db.go:118 uses INSERT INTO openai_logs_fts(openai_logs_fts) VALUES('rebuild'), which will
     rebuild from openai_logs.req_text / resp_text directly, not from COALESCE(...). With NULL sidecars, a rebuild can silently drop non-chat text from FTS. This is the main issue I would still fix before implementation.
  2. High: the top-down annotation math in design-docs-wip/21-IMPL-PLAN-CHAT-MODE-v4.md:598 is still too weak as written. I agree with the external critique in design-docs-wip/22-CRITIQUE_v4.md:34: using [parent_prefix_len, message_count) against the selected request’s message array is unsafe
     when history has been rewritten. This needs either backward attribution or explicit clamping based on the next entry’s boundary.
  3. Medium: the complex-message canonicalization in design-docs-wip/21-IMPL-PLAN-CHAT-MODE-v4.md:358 is still underspecified. I agree with design-docs-wip/22-CRITIQUE_v4.md:7 that IsComplex=true by itself is not enough. The plan says “preserve enough canonical signal” but never defines how. A
     ComplexHash-style field or equivalent concrete rule should be added.
  4. Medium: the bounded 5-attempt parent search in design-docs-wip/21-IMPL-PLAN-CHAT-MODE-v4.md:408 is acceptable for MVP, but I would not call it “production-grade.” It is a deliberate lossy heuristic. That is fine, but it should be described that way.
  5. Low: the turn-index convention is slightly muddy. design-docs-wip/21-IMPL-PLAN-CHAT-MODE-v4.md:131 and design-docs-wip/21-IMPL-PLAN-CHAT-MODE-v4.md:625 imply zero-based indexing, while the UI examples read like one-based human display. The API/frontend contract should state that
     explicitly.

  Take on 21

  v4 is materially better than v3. The double-echo issue is addressed at the design level, truncation handling is much clearer, and non-chat/truncated behavior is now specified. It is close to implementable.

  I would still consider it not quite final until the FTS rebuild behavior, complex canonicalization, and top-down attribution math are made explicit.

  Take on 22

  I mostly agree with the external critique:

  - I agree on the complex-message collision concern.
  - I agree on the attribution math fix.
  - I agree on the NULL vs "" implementation guardrail.
  - I agree with the UX note about historical assistant text being the client-echoed context, not necessarily the original raw response.

  What I do not agree with is the implied conclusion that those are the only remaining issues. The critique missed the FTS rebuild inconsistency, which is the most concrete backend flaw still present in v4.

  So my summary is: 21 is strong and close, 22 catches two important design gaps, but it misses one backend issue that should be fixed before coding starts.
