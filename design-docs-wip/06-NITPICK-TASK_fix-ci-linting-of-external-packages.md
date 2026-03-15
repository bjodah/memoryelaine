# Nitpick Task: Fix CI Linting of External Packages

## Observed failure

Current CI output includes:

```text
==> Checking formatting with gofmt
./root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.7.linux-amd64/src/cmd/compile/internal/syntax/testdata/issue20789.go:9:51: expected '(', found u
...
```

## Revised diagnosis

This does **not** primarily look like a generic "lint is traversing external
packages by design" problem.

The more likely chain is:

1. `.woodpecker.yaml` restores the cached tarball with plain `tar x`.
2. That unpack produces `./root/go/pkg/mod/...` inside the shared workspace.
3. `./scripts/run-lint-checks.sh` runs `find . -name '*.go' ... | xargs gofmt -l`.
4. Because the Go module cache is now physically under the workspace tree,
   `gofmt` walks those files too.

So the root issue is probably **workspace/cache restore polluting the repo
tree**, not that the lint script intentionally targets the external module cache
in its normal environment.

## Notes from local verification

- `./scripts/run-lint-checks.sh` anchors itself to the repository root before
  running.
- Running the script locally from both `/work` and `/` succeeds.
- `golangci-lint run ./...` is already module-scoped; the vulnerable part is the
  `find .` used for `gofmt`.

## Plan

### 1. Fix the CI cache/workspace interaction in `.woodpecker.yaml`

Preferred direction:

- stop restoring the Go module cache into the repository tree
- restore it to the actual cache location instead
- or restructure the Woodpecker workspace/cache arrangement so cached files live
  outside the checked-out repo directory

This should be the primary fix.

### 2. Harden `scripts/run-lint-checks.sh`

As a defensive improvement, restrict the `gofmt` input set to repository files
only, for example by using tracked Go files instead of `find .`.

This is a useful secondary guard, but it should not be the only fix if the CI
workspace is being polluted by cache extraction.

### 3. Verify after changes

- confirm the restored cache no longer appears under the repo tree
- confirm `./scripts/run-lint-checks.sh` only evaluates repository Go files
- confirm Woodpecker still benefits from cache reuse

## Explicit non-goal for this task

Do not broaden ignore patterns to hide arbitrary third-party trees without first
fixing the underlying cache/workspace layout problem.
