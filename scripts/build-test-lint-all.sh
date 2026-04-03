#!/usr/bin/env bash
set -euo pipefail
SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
"$SCRIPTS_DIR/_20-test_go-code.sh"
"$SCRIPTS_DIR/_40-build_go-code.sh"
"$SCRIPTS_DIR/_60-lint_go-code.sh"
"$SCRIPTS_DIR/_70-test_javascript.sh"
"$SCRIPTS_DIR/_80-test_emacs-elisp.sh"
