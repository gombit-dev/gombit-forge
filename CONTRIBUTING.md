# Contributing to Gombit Forge

Thanks for your interest. Forge is pre-alpha and moves fast, so this guide is
short and opinionated. Read it before opening your first pull request.

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## The one-sentence test

Every change is measured against the product thesis:

> **Forge edits a declarative application model; Gombit turns that model into
> ordinary software.**

If a change makes Forge a runtime dependency of generated apps, reimplements
something Gombit already owns, or turns generated source into an editable
round-trip representation, it is almost certainly wrong. See the locked
decisions below.

## Read these first

- [`AGENTS.md`](AGENTS.md) — the working agreement, current state, and the
  Gombit integration boundary. This is the single source kept current for what
  has actually shipped.
- [`docs/DESIGN.md`](docs/DESIGN.md) — product scope, milestones, and the locked
  MVP decisions (§33).
- [`docs/ADR-001.md`](docs/ADR-001.md) — identity, symbol allocation, file
  ownership, and the backend extension ABI.
- [`docs/ADR-004.md`](docs/ADR-004.md) — generation ownership. Read it before
  writing any generator.

The design docs describe a system that mostly does not exist yet. **Read the
code, not just the docs**, before asserting how something works.

## Development setup

Requires **Go 1.25.7** (the toolchain auto-resolves via `GOTOOLCHAIN`).

```bash
git clone https://github.com/gombit-dev/gombit-forge
cd gombit-forge
go test ./... -short
```

The root module's unit tests need no external toolchain. Two suites do:

- The **M0 end-to-end gate** scaffolds and runs a real application, so it wants
  the [`gombit`](https://github.com/gombit-dev/gombit) CLI (**≥ v0.1.2**),
  [`atlas`](https://atlasgo.io), and Docker on PATH. It skips automatically if
  any is missing or if run with `-short`:

  ```bash
  go test ./internal/compiler -run TestM0EndToEnd -v
  ```

- The **control-plane Postgres tests** (`controlplane/…`, via `make cp-test`)
  apply the committed Atlas migration to a throwaway Postgres, so they need
  `atlas` and Docker. They skip under `-short` and when Docker is absent; but
  with Docker present and `atlas` missing they **fail** rather than skip, since
  a green run that silently skipped every integration test is worse than a loud
  one. CI runs them as `make cp-test-short`, which skips them.

## Commands

```bash
make all           # fmt-check + vet + test + skills-check — the full CI gate
make test          # go test ./... -count=1
make race          # go test ./... -race
make cover         # coverage summary
make golden        # rewrite the canonical-JSON golden file (deliberate only)
make skills-check  # the .claude and .cursor skill trees must match byte for byte
```

CI runs `fmt-check`, `vet`, `skills-check`, `test`, and `race` on every push and
pull request. `make all` is the gate to run locally before you push.

## How work is organized

GitHub issues are the unit of work. Each issue carries an **area label** and
lives under exactly one **milestone** (`F0`, then `M0`–`M7`). Epics are labelled
`epic`; don't start a task whose epic describes an unmet prerequisite. Check
`gh issue list` and `git log` before claiming how anything works.

When editing issues, keep the existing milestone and area labels — don't rename,
re-bucket, or merge issues unasked. Close issues with one keyword each
(`Closes #2, closes #3`); a bare `Closes #2, #3` only closes the first.

## The skills, and why they are duplicated

This repo ships three task workflows as skills:

- **`feature`** — implement a new capability.
- **`bugfix`** — reproduce, fix, and regression-test a defect.
- **`review`** — an adversarial merge-gate review that overrides the bundled
  `/code-review` for this repo.

They live under `.claude/skills/<name>/SKILL.md` for Claude Code and
`.cursor/skills/<name>/SKILL.md` for Cursor. **The two trees are duplicated, not
shared** — Cursor does not reliably follow symlinks. `make skills-check` (run in
CI) diffs them and fails on any drift. Everything must match byte for byte
except each `review/SKILL.md`'s one-line pointer to its counterpart.

If you touch a skill: **edit both sides**, then run `make skills-check` before
committing.

## Conventions that reviews enforce

- **Determinism is a contract, not a nicety.** The same compiler version and the
  same spec must produce byte-identical output — no map iteration in ordered
  output, no timestamps, no randomness in generated artifacts. `internal/spec`
  has a golden file pinning canonical JSON; regenerate it deliberately with
  `make golden`, never to make a test pass.
- **Authored order is preserved, not sorted.** Order drives form-field order and
  navigation order, so it is meaningful.
- **Three states stay separate** (ADR-001 §36): spec validity, ABI
  compatibility, and build health. Don't collapse them.
- **Validation accumulates diagnostics** rather than failing on the first
  problem, and every diagnostic carries a stable `Code` plus the offending
  entity's stable ID.
- **The graph refuses to build over an invalid spec**, so generation stages
  carry no defensive nil checks. Preserve that invariant: if you add a reference
  to the graph, resolve it to a pointer or don't claim it's resolved.
- **Generated code consumes Gombit's public APIs and never duplicates Gombit
  infrastructure** (ADR-004 D3). No Forge-specific router, ORM layer,
  auth/permission/admin implementation, divergent response envelope, or vendored
  Gombit internals — and never import Gombit *internal* packages.
- **Standard library first.** A dependency needs a reason.
- **Comments explain why.** A comment that describes behavior the code doesn't
  have is a bug, not a docs nit.

## Locked decisions — do not re-litigate

These are settled in DESIGN.md §33 and ADR-001. Changing one needs a new ADR,
not a pull request:

- **D1 spec-first.** `ProjectSpec` is the source of truth. Generated source is
  not an editable round-trip representation.
- **D2 compiler, not runtime.** Generated apps are ordinary Gombit apps with no
  Forge runtime dependency; a deployed app must keep working if Forge vanishes.
- **D4 PostgreSQL only** for managed hosting (export may target any driver);
  **D5 cookie/session auth**; **D6 structured pages, not a freeform canvas**;
  **D10/D11 export is mandatory and one-way**; **D12 use Gombit's contracts** —
  never build a Forge-specific auth, migration, API, admin, or ORM system.

## Submitting a change

1. Branch from `main`.
2. Make the change with tests. For a bug, the regression test must **fail
   without the fix and pass with it** — a test that passes both ways proves
   nothing.
3. Run `make all` and make it green.
4. Open a pull request describing what changed and why, and link the issue it
   closes.
5. The adversarial review runs on the PR. Treat its findings as **claims, not
   facts**: reproduce a reported defect against the current code before fixing
   it, and confirm the fix. Push fixes and re-request review until it is clean
   and CI is green.

A few standing rules from the working agreement:

- **Verify before asserting.** Read the code; the docs describe intent.
- **Beware a panicking test** — it aborts the binary, so later subtests silently
  never run and appear to pass.
- **Say what's done, what's skipped, and what's uncertain.** Report failures
  with their output.

## License

Forge is licensed under **AGPL-3.0** (see [LICENSE](LICENSE)). By contributing,
you agree that your contributions are licensed under the same terms.
