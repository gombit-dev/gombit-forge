@AGENTS.md

## Claude Code

- Project skills: `feature`, `bugfix`, `review` (`.claude/skills/<name>/SKILL.md`).
  `review` is an adversarial merge-gate review and overrides the bundled
  `/code-review` for this repo. Invoke with `/review`, `/feature`, `/bugfix`,
  or just describe the task.
- For what has shipped, defer to AGENTS.md "Current state" — the single place
  kept current. Don't restate a snapshot of it here; that is what drifts stale.
- Prefer a direct `Read` of `docs/DESIGN.md` or `docs/ADR-001.md` over spawning
  an Explore subagent. They are two files and the answer is usually a §
  reference you should cite.
- The repo is small enough to read. Don't guess at `internal/spec` or
  `internal/compiler/graph` behavior from the docs — the docs describe intent,
  which has drifted from the code (much of it now built, some of it superseded).
  Read the code, and check `gh issue list` for what has actually shipped.
- When creating or editing GitHub issues, keep the milestone and area labels
  from the existing set. Don't rename, re-bucket, or merge issues unasked.
- Close issues with one keyword each (`Closes #2, closes #3`) — a bare
  `Closes #2, #3` only closes the first.

## Cursor skills

Cursor counterparts live under `.cursor/skills/` with the same three names.
The two trees are **duplicated, not shared** — Cursor does not reliably follow
symlinks — so `make skills-check` (run in CI) diffs them and fails on drift.
Everything must match byte for byte except each `review/SKILL.md`'s one-line
pointer to its counterpart.

Edit both sides, then run `make skills-check` before committing.
