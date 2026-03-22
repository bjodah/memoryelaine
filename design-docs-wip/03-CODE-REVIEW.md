• Review scope: I read design-docs-wip/02-REVISED-PRD-and-IMPL_PLAN-emacs-pkg.md, design-docs-main/04-SPEC-version2.md, and the branch tip centered on commit 6ca80fb. The PRD’s server/API work spans earlier commits on this branch, so the findings below are against the current branch behavior,
  not just the small file delta in 6ca80fb.

Findings

  1. The Emacs async staleness model is too global and will discard legitimate concurrent responses. emacs/memoryelaine-http.el:15 uses a single process generation counter for every request, and emacs/memoryelaine-http.el:98 drops any response older than the most recent request. That breaks
     normal flows in emacs/memoryelaine-search.el:177 and emacs/memoryelaine-show.el:63: memoryelaine-search-refresh issues /api/logs and then /api/recording, so the second request can invalidate the first, and detail view metadata plus body-preview fetches can invalidate each other the same
     way. This should be per-buffer or per-request-family, not global.
  2. t does not actually fulfill the PRD’s “load full body” promise when the user is in assembled response view. emacs/memoryelaine-show.el:239 can fetch assembled preview content, but emacs/memoryelaine-show.el:254 only upgrades the raw request/response bodies to full. emacs/memoryelaine-
     state.el:141 also has no separate metadata/state slot for assembled-body size/truncation. Result: a large assembled response can remain preview-truncated even after pressing t.
  3. The query parser accepts unterminated quoted phrases instead of rejecting them. internal/query/parser.go:64 tokenizes a leading " without requiring a closing quote, and internal/query/parser.go:171 then accepts that token as a normal text term. The PRD explicitly says invalid query terms
     should return 400, so inputs like "unterminated should fail parsing.
  4. The initial Emacs header can misreport recording state. emacs/memoryelaine-state.el:34 defaults recording to t, and emacs/memoryelaine-search.el:58 opens the search buffer without fetching /api/recording; the real value is only loaded on manual refresh via emacs/memoryelaine-search.el:99.
     If the server is paused, the first screen still shows ●REC.
  5. PUT auth handling is inconsistent with GET auth handling. emacs/memoryelaine-http.el:130 clears cached credentials on GET 401, but emacs/memoryelaine-http.el:145 has no equivalent path for PUTs. A failed recording toggle can therefore leave bad credentials cached and produce a worse
     recovery path than a failed GET.

  The current Emacs tests do not cover the highest-risk cases above: concurrent request invalidation, full assembled-body fetch behavior, or malformed-quote parsing.

  I added scripts/run-emacs-ert-tests.sh. It runs batch ERT from repo root and supports EMACS_BIN override. I verified it locally: 25/25 Emacs tests passed, and scripts/build-and-test.sh also passed.

  Spec
  design-docs-main/04-SPEC-version2.md is not fully up to date.

  - The query DSL names in the spec/README do not match the parser. The spec says is:truncated and has:req-body / has:resp-body in design-docs-main/04-SPEC-version2.md:417 and README.md:172, but the parser implements is:req-truncated, is:resp-truncated, has:req, and has:resp in internal/query/
    parser.go:98.
  - The spec’s /api/logs response example omits limit, offset, and has_more in design-docs-main/04-SPEC-version2.md:397, but the actual DTO/API include them in internal/management/dto.go:23 and internal/management/api.go:108.
  - The spec says assembled body requests fall back to raw when assembly is unavailable in design-docs-main/04-SPEC-version2.md:464, but the implementation returns available=false plus a reason in internal/management/api.go:274.
  - The spec still describes only CLI, TUI, and Web UI surfaces in design-docs-main/04-SPEC-version2.md:11; it does not acknowledge the new Emacs client at all.

  So my answer is: partially updated, but not reliable as the sole source of truth for the branch tip.

  Improvements

  - Emacs: fix request scoping first. That is the main correctness issue. Then add tests around out-of-order callbacks, active-mode full fetch, and startup recording-state sync.
  - Server/API: now that external clients exist, formalize the management API contract. Version it, and make body metadata distinguish preview truncation from original capture truncation explicitly.
  - Querying: expose a normalized query echo/AST and parse position info in successful and failed responses so Web UI, Emacs, and future clients can all show the same query semantics.
  - Pagination: consider keyset pagination on (ts_start,id) instead of offset pagination for more stable browsing while new logs are arriving.
  - TUI: it still lags the newer server search model. Adding DSL-based search and visible recording state would make it feel aligned with the Web UI and Emacs package.
  - Web UI: cache loaded body previews/full bodies across toggles, add keyboard navigation, and surface parse/query errors inline instead of only failing the fetch.
  - Proxy/server observability: surface dropped-log queue pressure, capture truncation rates, and FTS/index health in health/metrics/UI so operators can tell when the logging path is degrading.
