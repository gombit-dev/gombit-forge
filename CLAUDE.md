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
  `internal/compiler/graph` behavior from the docs — the docs describe the
  intended system, most of which isn't built.
- When creating or editing GitHub issues, keep the milestone and area labels
  from the existing set. Don't rename, re-bucket, or merge issues unasked.
- Close issues with one keyword each (`Closes #2, closes #3`) — a bare
  `Closes #2, #3` only closes the first.

## Cursor skills

Cursor counterparts live under `.cursor/skills/` with the same three names and
shared `references/`. Keep the two sets aligned with each other and with
AGENTS.md; the adversarial review contract in
`references/adversarial-review.md` is duplicated verbatim between them and must
not drift.
