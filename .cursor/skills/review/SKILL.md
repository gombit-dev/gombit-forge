---
name: review
description: Adversarial senior review of a Gombit Forge diff or pull request. Determines whether the change deserves to merge against its claimed contract, the locked decisions in AGENTS.md, and the identity/ownership invariants in ADR-001. Use when the user asks for a code review, PR review, diff review, or to check a change before merge.
---

# Review

Review the current diff (or a named PR/branch) as an adversarial senior
reviewer. Read `AGENTS.md` first. This skill overrides generic, agreeable, or
checklist-only review habits, and the bundled `/code-review`, for this repo.

The Claude Code counterpart is `.claude/skills/review/SKILL.md`. Keep both aligned,
including the shared references.

## When not to use

- Implementing a new issue → `feature`
- Reproducing and fixing a defect → `bugfix` (review the fix afterward with this)

## Setup

1. Determine the review surface: `git diff`, `git diff main...HEAD`, or the
   named PR (`gh pr diff <n>`).
2. Identify the linked issue and its acceptance criteria. Compare the
   implementation against the issue, not against the PR description.
3. Read the design sections the change touches — `docs/DESIGN.md` and
   `docs/ADR-001.md` are the authority, and findings should cite § numbers.
4. Load [references/checklist.md](references/checklist.md). Walk only the
   sections the diff touches.
5. **Read [references/adversarial-review.md](references/adversarial-review.md)
   in full before writing any review.** That file is the review: persona,
   method, severity, output format, and merge standard.

## How to review

Follow [references/adversarial-review.md](references/adversarial-review.md)
exactly.

Technical analysis comes first; personality is presentation only. Never
manufacture a blocker to satisfy the persona — an `# APPROVE` with no fake
findings beats a theatrical `# REQUEST CHANGES`.

Do not re-litigate locked decisions (AGENTS.md). If the author re-litigated
one without an ADR, that is **BLOCKING**.

This repo is small and fully readable. Prove a defect against the actual code
before reporting it. Where you cannot, label it `LIKELY` or `QUESTION` as the
contract requires — do not upgrade a suspicion to `CONFIRMED`.

Two verification habits this repo has already needed:

- Before claiming a test is inadequate, check whether it fails without the
  change. Before accepting one as proof, check the same.
- A panicking test aborts the binary, so subsequent subtests never run and
  read as passing. Run them individually before concluding a path is covered.

The review must begin with exactly one of `# APPROVE`, `# COMMENT`, or
`# REQUEST CHANGES`, then a short opening assessment, numbered findings, and
`# VERDICT`.

## Posting

Findings go as inline comments on the PR, anchored to the offending lines; if
the change is clean, post `LGTM` and approve.

Posting is outward-facing and irreversible, so **confirm before posting unless
the user already asked you to**. When they asked only for the review, write it
in the terminal and stop. Be concise in every comment: maximum signal, minimum
tokens, but enough context for the author to act.
