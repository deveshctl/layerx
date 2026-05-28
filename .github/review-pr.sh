#!/usr/bin/env bash
# review-pr.sh — pre-merge code review gate.
#
# Run this on a PR branch before merging. It prepares the diff range, prints
# context, and reminds you to invoke the `code-review` skill in your AI agent
# session. The skill does the actual analysis (5-angle finder + verifier +
# sweep, max 15 findings); this script just sets the stage and documents the
# expected workflow so it's the same every time.
#
# Why this exists: round-8 of layerx's bug-fix cycle merged a PR that itself
# introduced 11 bugs. The same code-review skill that found them post-merge
# was available before the merge — it just wasn't run. This script is the
# enforcement seam.
#
# Usage:
#   .github/review-pr.sh                 # review HEAD vs origin/main
#   .github/review-pr.sh main            # review HEAD vs main (no remote)
#   .github/review-pr.sh origin/release  # review against any base ref
#
# Exit codes:
#   0  diff prepared and printed; review next in the AI agent session
#   1  not a git repo, or base ref missing, or working tree clean (nothing to review)

set -euo pipefail

BASE="${1:-origin/main}"

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "review-pr.sh: not inside a git work tree" >&2
  exit 1
fi

if ! git rev-parse --verify "$BASE" >/dev/null 2>&1; then
  cat >&2 <<EOF
review-pr.sh: base ref '$BASE' not found.

Try:
  git fetch origin                       # refresh remote refs
  .github/review-pr.sh main              # use a local branch
  .github/review-pr.sh origin/develop    # use a different remote ref
EOF
  exit 1
fi

# Three-dot diff: changes on HEAD that are not in BASE (the PR's contribution).
# Includes uncommitted working-tree changes too — the gate runs before the
# commit just as often as after.
RANGE="$BASE...HEAD"
DIFF_FILES=$(git diff --name-only "$RANGE")
WORKTREE_FILES=$(git diff --name-only HEAD)
ALL_FILES=$(printf '%s\n%s\n' "$DIFF_FILES" "$WORKTREE_FILES" | sort -u | sed '/^$/d')

if [ -z "$ALL_FILES" ]; then
  echo "review-pr.sh: no diff vs '$BASE' and clean working tree — nothing to review." >&2
  exit 1
fi

CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
COMMITS_AHEAD=$(git rev-list --count "$RANGE" 2>/dev/null || echo "?")
FILE_COUNT=$(printf '%s\n' "$ALL_FILES" | wc -l | tr -d ' ')

cat <<EOF
==================================================================
                   PRE-MERGE CODE REVIEW GATE
==================================================================
branch:        $CURRENT_BRANCH
base:          $BASE
commits ahead: $COMMITS_AHEAD
files changed: $FILE_COUNT

------------------------------------------------------------------
Files in this review:
------------------------------------------------------------------
EOF

printf '%s\n' "$ALL_FILES" | sed 's/^/  /'

cat <<EOF

------------------------------------------------------------------
Diffstat (range $RANGE + uncommitted):
------------------------------------------------------------------
EOF
git diff --stat "$RANGE" || true
git diff --stat HEAD || true

cat <<EOF

==================================================================
                   NEXT STEP — RUN IN AGENT
==================================================================

In your AI agent session, invoke the code-review skill. The skill:
  - reads the diff via 'git diff $RANGE' (and HEAD)
  - runs 5 finder angles + verifier + sweep
  - returns up to 15 findings as JSON

Treat any CONFIRMED finding as a merge blocker. PLAUSIBLE findings
need a human decision. REFUTED findings are dropped by the skill.

Do NOT merge until:
  [ ] go build ./...   passes
  [ ] go vet ./...     passes
  [ ] CI is green
  [ ] code-review skill returned [] OR every finding is fixed/triaged
  [ ] CHANGELOG.md updated for any user-visible change

==================================================================
EOF
