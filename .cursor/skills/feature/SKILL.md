---
name: feature
description: Implements a Gombit Forge issue or new capability as a scoped, tested change. Use when the user asks to implement an issue, add a compiler stage, extend ProjectSpec, land a milestone item, or build a new feature from docs/DESIGN.md or docs/ADR-001.md.
---

# Feature

Implement one Forge unit of work. Read `AGENTS.md` first. `docs/DESIGN.md` is
authoritative for scope; `docs/ADR-001.md` for identity, ownership and the
extension ABI.

## When not to use

- Defect, regression, or failing test → `bugfix`
- Judging whether a change should merge → `review`

## Setup

1. Find the issue and its acceptance criteria (`gh issue view <n>`). If none
   exists, agree on scope before writing code.
2. Check the epic for unmet prerequisites. M0 gates everything; F0 gates the
   extension ABI.
3. Read the design sections the issue cites. Implement against those, not
   against a paraphrase.
4. Read the code you're extending. Most of the system described in the docs
   does not exist yet — verify rather than assume.

## Implementing

Scope is the issue. Don't quietly widen it, and don't narrow it either — if
part is blocked, finish everything else and say plainly what you left and why.

Hold these invariants; they are the product, not preferences:

- **Determinism.** Same compiler version + same spec = byte-identical output.
  No map iteration in ordered output, no timestamps, no randomness. Preserve
  authored order; never sort a user-meaningful sequence.
- **Identity.** Stable IDs are identity. Labels, storage names and code symbols
  are separate domains — a relabel must never move a code symbol.
- **Ownership.** Compiler output goes under `internal/forge_generated/**` and
  nowhere else. Never write or rewrite `internal/extensions/**` beyond a
  one-time stub.
- **Separate states.** Spec validity, ABI compatibility and build health are
  three questions. Don't collapse them.
- **Use Gombit.** Migrations, OpenAPI, TS client, auth, admin and GORM belong
  to Gombit. If Forge needs framework behavior, prefer adding it to Gombit.

Make invalid states unrepresentable where you can; reject them early where you
can't. A comment claiming a contract the code doesn't implement is a defect —
either build the behavior or delete the claim.

## Testing

Write tests that could fail. For each one ask what incorrect implementation
would still pass it, then cover that instead.

Verify a new test actually fails without your change — revert the source, run
it, restore. A test that passes both ways is decoration. Note that a panicking
test aborts the binary, so later subtests silently never run; check them
individually.

Cover the boundaries this repo keeps hitting: empty and nil collections,
duplicate names across and within namespaces, dangling references, malformed
input that `Unmarshal` accepts, and defaults or literals that reach generated
output verbatim.

If canonical JSON changes, regenerate the golden file with `make golden`
deliberately — never to make a red test green.

## Finishing

Run `make all` and `make race`. Report what passed, what you skipped, and
anything uncertain. Don't commit, push, or open a PR unless asked.
