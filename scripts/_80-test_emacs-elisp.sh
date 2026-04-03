#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

EMACS_BIN="${EMACS_BIN:-emacs}"

printf '==> Running Emacs ERT tests\n'
"$EMACS_BIN" -Q --batch \
  -L "$ROOT_DIR/emacs-memoryelaine" \
  -l "$ROOT_DIR/emacs-memoryelaine/memoryelaine-test.el" \
  -f ert-run-tests-batch-and-exit
