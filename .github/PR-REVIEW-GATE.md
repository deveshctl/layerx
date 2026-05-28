# Pre-Merge Code Review Gate

## Why this exists

PR #29 (round 8 of bug fixes) merged with 11 bugs the post-merge code-review
skill caught immediately. The skill was available before the merge — it just
wasn't run. PR #30 (the round-9 fix-up of those 11) had 4 more findings on
the same gate. The pattern: bug-fix PRs touch high-risk code by definition,
and the very review discipline that catches their regressions is the easiest
thing to skip when "we're just fixing bugs".

This document and the `review-pr.sh` helper exist to make skipping the gate
a deliberate choice instead of an oversight.

## The gate (3 layers)

### Layer 1 — Local helper (fastest, voluntary)

Before pushing the final commit on a PR branch, run:

```
bash .github/review-pr.sh
```

The script:
- prints the diff range vs `origin/main`,
- lists the files in scope,
- shows a diffstat,
- reminds you to invoke the `code-review` skill in your AI agent session.

The script does NOT run the review itself — the skill does that, with the
script's output as setup context. The skill returns up to 15 findings as
JSON. Any CONFIRMED finding is a merge blocker. PLAUSIBLE findings need
a human decision.

### Layer 2 — CLAUDE.md rule (always-on, agent-side)

`CLAUDE.md` requires the code-review skill on every non-trivial PR before
opening or merging. Trivial = a single-line typo, a comment fix, a
dependency bump. Anything that touches logic gets the skill.

The rule is in `CLAUDE.md` under "PR Review Discipline". An agent that
skips it is violating the project's instruction priority.

### Layer 3 — GitHub branch protection (enforcement, mandatory)

Even with the helper script and the agent rule, a human can still merge
without running the gate. Branch protection on `main` makes that
impossible.

#### Recommended GitHub settings (Settings → Branches → main → Edit rule)

```
[x] Require a pull request before merging
    [x] Require approvals (1)
    [x] Dismiss stale pull request approvals when new commits are pushed
    [x] Require review from Code Owners                (optional)

[x] Require status checks to pass before merging
    [x] Require branches to be up to date before merging
    Required checks:
      - build (from .github/workflows/ci.yml)
      - test  (from .github/workflows/ci.yml)

[x] Require conversation resolution before merging

[x] Do not allow bypassing the above settings           (recommended)
```

#### Optional: code-review as a required check

The `code-review` skill runs in an agent session, not on GitHub Actions, so
it cannot be enforced as a GitHub status check today. Two ways to bridge
that gap:

1. **Self-attestation in the PR description.** Add a checkbox like
   `[ ] Ran .github/review-pr.sh and the code-review skill; findings
   addressed`. Reviewers verify before approving. Cheap, honour-system.

2. **CI workflow that fails if the PR description checkbox is unticked.**
   A small workflow can grep the PR body and fail-soft if the box isn't
   ticked. Pairs well with the self-attestation approach above. Not
   implemented yet — open a follow-up issue if you want it.

## What "non-trivial" means

The CLAUDE.md rule says non-trivial PRs need the skill. The line:

| Counts as non-trivial (run the skill) | Trivial (skip is fine) |
|---|---|
| Any change in `image/`, `tui/`, `ci/`, `cmd/`, `config/`, `mcp/` | A typo in a doc string |
| New files, deleted files, renamed files | Adding a CHANGELOG bullet |
| Change to a public function signature | Reformatting (gofmt-only diff) |
| Touches concurrency, error handling, or I/O paths | Bumping a tool version in CI |
| Bug-fix PRs (especially ones that have already had a "round N") | Pinning a dep to a known-good version |
| Refactors, even pure rename ones | Adding an `// TODO:` comment |

When in doubt, run the skill. The cost is minutes; the cost of a bad merge
is the next round of bug fixes.

## What to do with findings

The skill's verifier returns one of three votes per finding:

- **CONFIRMED** — fix before merge. No exceptions.
- **PLAUSIBLE** — read the failure scenario; either fix it, prove it can't
  happen and write that proof in a comment, or open a follow-up issue and
  link it from the PR.
- **REFUTED** — already dropped by the skill; you'll never see these.

If the skill returns `[]`, you're done. Note in the PR description that
the skill was run and returned clean.

## When the gate has caught real bugs

- **PR #30, round 9** — round-10 review caught:
  1. `errNoCIRulesEnabled` had a dead branch (always returned the same
     message because `cmd.Name()` always evaluated to `"ci"`).
  2. `renderNoOp` had an empty-digest path with no test coverage.
  3. `README.md` documented only one of two possible `verdict: noop`
     forms.
  4. `indexTree` comment said hardlinks were skipped but the code didn't
     skip them.

  None of these would have been blocked by `go build` or `go vet`. All four
  were caught by the skill in a few minutes of agent time, with the gate
  doing the work this doc says it should do.
