#!/usr/bin/env bash
# Merge check. Delegates to `ateam review gate` at the release pinned in
# .github/workflows/{ci,merge-check}.yml; this wrapper
# preserves the `task merge:check` entrypoint. The binary is
# $ATEAM, else ateam on PATH. The gate's three rules:
#
#   1. The rules in force on main require every check in ATEAM_REQUIRED_CHECKS
#      (Required,Merge check) with the up-to-date setting on.
#   2. A non-draft code pull request has one implemented-by: and one reviewed-by:
#      line with (model, effort), a cross-provider reviewer not weaker than every
#      implementer under .agents/review-policy.json, and an approving review
#      record no later code commit follows.
#   3. While an issue labelled class:flake is open, fail unless this pull request
#      closes every open one.
#
# Usage: merge-check.sh <pr-number>
#   Exit 0 pass, 1 fail, 2 unavailable (a read failed or was not the shape asked for).
set -euo pipefail
export ATEAM_REQUIRED_CHECKS="${ATEAM_REQUIRED_CHECKS:-Required,Merge check}"
exec "${ATEAM:-ateam}" review gate "$@"
