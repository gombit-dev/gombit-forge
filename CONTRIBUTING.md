# Contributing to Gombit Forge

Thanks for wanting to help. This page covers how to get set up, how to get a
change reviewed, and the bar a change has to clear. Forge is pre-alpha and moves
fast, so read it before your first pull request.

- **Bugs and features** → [open an issue](https://github.com/gombit-dev/gombit-forge/issues/new)
- **Security vulnerabilities** → **not** an issue; see [SECURITY.md](SECURITY.md)
- **Behaviour expectations** → [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)

## The one-sentence test

Every change is measured against the product thesis:

> **Forge edits a declarative application model; Gombit turns that model into
> ordinary software.**

If a change makes Forge a runtime dependency of generated apps, reimplements
something Gombit already owns, or turns generated source into an editable
round-trip representation, it is almost certainly wrong. See
[Locked decisions](#locked-decisions--do-not-re-litigate).

## Prerequisites

| Tool | Version | Needed for |
| --- | --- | --- |
| Go | each module's `go.mod` is authoritative (auto-resolves via `GOTOOLCHAIN`) | everything |
| `gombit` CLI | the control plane's pin (`make install-gombit`); ≥ v0.8.0 (`internal/gombit.MinimumVersion`) | the end-to-end suites (scaffold, migrate, build and run a real app) |
| Atlas | Community Edition v1.3.0, the version CI pins (`.github/actions/install-atlas`) | migration diffing in the e2e and control-plane tests |
| Docker | any recent | throwaway Postgres for the e2e and control-plane tests |
| Node.js | 22+ | the editor SPA under `controlplane/web`, and the generated frontend in the e2e suite (`node`, `npm`, `npx`) |

The root module's unit tests need **none** of the external tools — only Go.
Install the `gombit` CLI when you need the integration suites. This installs
the version `controlplane/go.mod` pins, the one CI tests with; install a newer
release by hand when you want to try it before the pin moves:

```bash
make install-gombit
```

## Getting set up

```bash
git clone https://github.com/gombit-dev/gombit-forge.git
cd gombit-forge
go test ./... -short
```

The repo is **two Go modules**: the root module
(`github.com/gombit-dev/gombit-forge`) is the compiler and stays Gombit-free —
it drives Gombit only through the CLI — and `controlplane/` is the Gombit
application, where the runtime dependency on Gombit is allowed to live. Root
`./...` never reaches into the nested module, so the control plane has its own
`cp-*` make targets. A `go.work` at the repo root is a gitignored local-dev
convenience; create it with `go work init . ./controlplane`. Never run
`go work sync` — it rewrites the root module's `go.sum` to workspace-wide
versions and drags Gombit into the Gombit-free compiler.

## Read these first

The design docs describe intent, which has drifted from the code in both
directions — much of the intended system now exists (F0–M7), and some pieces
never will (superseded by ADRs). **Read the code and check `gh issue list`**,
not just the docs, before asserting how something works.

- [`AGENTS.md`](AGENTS.md) — the working agreement, current state, and the Gombit
  integration boundary. The single source kept current for what has shipped.
- [`docs/DESIGN.md`](docs/DESIGN.md) — product scope, milestones, locked MVP
  decisions (§33).
- [`docs/ADR-001.md`](docs/ADR-001.md) — identity, symbol allocation, file
  ownership, the backend extension ABI.
- [`docs/ADR-004.md`](docs/ADR-004.md) — generation ownership. Read it before
  writing any generator.

## How work is organised

GitHub issues are the unit of work. Each carries an **area label** and lives
under exactly one **milestone** (`F0`, then `M0`–`M7`). Epics are labelled
`epic`; don't start a task whose epic describes an unmet prerequisite. Two things
follow:

- **One issue → one pull request** where practical, and the PR links its issue.
- When editing issues, keep the existing milestone and area labels — don't
  rename, re-bucket, or merge issues unasked. Close issues with one keyword each
  (`Closes #2, closes #3`); a bare `Closes #2, #3` only closes the first.

Check `gh issue list` and `git log` before claiming how anything works. If
something looks missing from the backlog, say so in an issue rather than adding
scope in a PR.

## Making a change

1. Branch from `main`. Branch names are free-form; `feat/…`, `fix/…`, `docs/…`
   are common here.
2. Write the change **and its tests**. For a bug, the regression test must
   **fail without the fix and pass with it** — a test that passes both ways
   proves nothing.
3. Run the checks below and make them green.
4. Open a PR against `main` describing what changed and why, and link the issue
   it closes. Conventional-commit prefixes (`feat:`, `fix:`, `docs:`, `chore:`)
   are expected in the title.

## Local checks

`make all` is the local gate; run it before you push. CI runs the same checks
split into jobs: a `build` job (with `cp-test-short` instead of `cp-test`), an
`e2e` job that runs `make e2e` with every tool installed, the SPA, and an
informational `govulncheck`.

```bash
make all           # fmt-check + vet + cp-vet + test + cp-test + skills-check
make test          # go test ./... -count=1   (root module)
make race          # go test ./... -race
make e2e           # the toolchain lane: needs gombit, atlas, Docker and node; any skip fails
make cover         # coverage summary
make golden        # rewrite the canonical-JSON golden file (deliberate only)
make skills-check  # the .claude and .cursor skill trees must match byte for byte
```

### The end-to-end gate

`TestM0EndToEnd` scaffolds a real Gombit app, migrates on Postgres, builds,
boots, and exercises CRUD + admin. The export, frontend and migration
end-to-ends and the gombit-app compile tests work the same way. They need
`gombit`, `atlas` and Docker on PATH (the frontend one also needs node), and
under `go test` they skip without them or under `-short`:

```bash
go test ./internal/compiler -run TestM0EndToEnd -v
```

`make e2e` is the lane that must not skip. It runs those tests, then the control
plane's whole suite, through `internal/toolchaintest/noskip`, which lists every
test that ran and fails on any skip. It also sets `FORGE_REQUIRE_TOOLCHAIN=1`,
which makes a missing tool fail the test with its name rather than skip it. CI's
`e2e` job runs it with the gombit `controlplane/go.mod` pins. Run it before you
move that pin.

In `make e2e` any skip fails, a bare `t.Skip` included: `noskip` prints the
skip's message and fails the step. `toolchaintest` carries the rule outside the
lane: with `FORGE_REQUIRE_TOOLCHAIN=1`, a hand run of `go test` on one package
fails on a missing tool instead of skipping. So a new test that needs a tool
should check for it with `toolchaintest.RequireBinaries` (or
`toolchaintest.Skipf`). The control plane's Postgres tests skip the plain way
(`internal/dbtest`); in the lane, `noskip` covers them.

### The control plane

The control plane is a separate nested module, so root `./...` never covers it:

```bash
make cp-vet cp-build cp-test
```

`make cp-test` applies the committed Atlas migration to a throwaway Postgres, so
it needs `atlas` and Docker. It skips under `-short` and when Docker is absent;
with Docker present but `atlas` missing it **fails** rather than skips — a green
run that silently skipped every integration test is worse than a loud one. CI's
`build` job runs `make cp-test-short`, which skips every Postgres-backed test;
its `e2e` job runs them all (`make e2e-controlplane`).

### The editor SPA

```bash
make web-check web-build web-test   # typecheck, build, unit-test controlplane/web
```

### The skills, and why they are duplicated

This repo ships three task workflows as skills — **`feature`**, **`bugfix`**, and
**`review`** (an adversarial merge-gate review that overrides the bundled
`/code-review`). They live under `.claude/skills/<name>/SKILL.md` for Claude Code
and `.cursor/skills/<name>/SKILL.md` for Cursor. **The two trees are duplicated,
not shared** — Cursor does not reliably follow symlinks — so `make skills-check`
(run in CI) diffs them and fails on any drift. Everything must match byte for
byte except each `review/SKILL.md`'s one-line pointer to its counterpart. If you
touch a skill: **edit both sides**, then run `make skills-check`.

## Code review

Before opening or merging a PR, review the diff as an adversarial senior
reviewer against the working agreement and the change's claimed contract. Treat
review findings as **claims, not facts**: reproduce a reported defect against the
current code before fixing it, and confirm the fix. This repo ships a review
skill for it — run `/review` in Claude Code or Cursor.

## Working agreement

A pull request is not done unless it satisfies these — the conventions reviews
enforce (full text in [`AGENTS.md`](AGENTS.md)):

- **Determinism is a contract, not a nicety.** The same compiler version and spec
  must produce byte-identical output — no map iteration in ordered output, no
  timestamps, no randomness. `internal/spec` pins canonical JSON in a golden
  file; regenerate it with `make golden`, never to make a test pass.
- **Authored order is preserved, not sorted** — it drives form-field and
  navigation order.
- **Three states stay separate** (ADR-001 §36): spec validity, ABI compatibility,
  build health. Don't collapse them.
- **Validation accumulates diagnostics** rather than failing on the first
  problem, each carrying a stable `Code` and the offending entity's stable ID.
- **The graph refuses to build over an invalid spec**, so generation stages carry
  no defensive nil checks — preserve that invariant.
- **Generated code consumes Gombit's public APIs and never duplicates Gombit
  infrastructure** (ADR-004 D3): no Forge-specific router, ORM, auth/admin
  implementation, or divergent response envelope, and never import Gombit
  *internal* packages.
- **Standard library first** — a dependency needs a reason.
- **Comments explain why** — a comment describing behaviour the code lacks is a
  bug, not a docs nit.
- **Verify before asserting**, and **say what's done, what's skipped, and what's
  uncertain** — report failures with their output.

## Locked decisions — do not re-litigate

Settled in DESIGN.md §33 and ADR-001; changing one needs a new ADR, not a PR:

- **D1 spec-first** — `ProjectSpec` is the source of truth; generated source is
  not an editable round-trip.
- **D2 compiler, not runtime** — generated apps are ordinary Gombit apps with no
  Forge runtime; a deployed app keeps working if Forge vanishes.
- **D4 PostgreSQL only** for managed hosting (export may target any driver);
  **D5 cookie/session auth**; **D6 structured pages, not a freeform canvas**;
  **D7 Forge itself uses Gombit**; **D10/D11 export is mandatory and one-way**;
  **D12 use Gombit's contracts** — never build a Forge-specific auth, migration,
  API, admin, or ORM.

## License

Forge is licensed under [**AGPL-3.0**](LICENSE). By contributing, you agree that
your contributions are licensed under the same terms.
