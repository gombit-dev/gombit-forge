# AGENTS.md

Gombit Forge is a visual application builder that compiles a declarative
`ProjectSpec` into an ordinary Gombit application. Module path
`github.com/gombit-dev/gombit-forge`, Go 1.25.7.

The whole product fits in one sentence, and it is the test every feature must
pass:

> Forge edits a declarative application model; Gombit turns that model into
> ordinary software.

## Current state

Pre-alpha. Only the spec layer exists. `internal/spec` holds `ProjectSpec`,
stable IDs, canonical JSON and the semantic validator; `internal/compiler/graph`
holds the resolved domain graph. Nothing generates code yet.

M0 (the go/no-go gate) is the active milestone: issues #2, #3 and #5 shipped in
PR #86; #4, #6–#12 remain. There is no control plane, no editor, no build
pipeline, no deploy path. Don't describe those as existing.

Milestones: `F0` (identity + extension ABI, ADR-001) and `M0`–`M7`. Every issue
carries an area label and lives under exactly one milestone. Check `gh issue
list` and `git log` before claiming how anything works.

## Source of truth

- `docs/DESIGN.md` is authoritative for product scope, milestones and the
  locked MVP decisions (§33).
- `docs/ADR-001.md` is authoritative for identity, symbol allocation, file
  ownership and the backend extension ABI. Where it overlaps DESIGN.md on
  identity or naming, ADR-001 v2 wins — it supersedes v1 and is the newer
  document.
- `docs/ADR-002.md` is authoritative for generation ownership and amends
  DESIGN.md P4. Read it before writing any generator.
- GitHub issues are the unit of work. Epics are labelled `epic`; don't start a
  task whose epic describes an unmet prerequisite.

## Locked decisions (do not re-litigate)

From DESIGN.md §33:

- **D1 spec-first.** `ProjectSpec` is the source of truth. Generated source is
  not an editable round-trip representation.
- **D2 compiler, not runtime.** Generated apps are normal Gombit apps with no
  Forge runtime dependency. A deployed app must keep working if Forge vanishes.
- **D3 isolated per project.** No shared generated-app runtime.
- **D4 PostgreSQL only** for managed hosting. Export may target any driver.
- **D5 cookie/session auth** for managed apps.
- **D6 structured pages, not a freeform canvas.** Tables, forms, details,
  dashboard. No absolute positioning.
- **D7 Forge itself uses Gombit.** The control plane is a Gombit application;
  dogfooding is locked, not a preference.
- **D8 builds are asynchronous.** No HTTP request performs a build.
- **D9 build execution is isolated.** Generated source is untrusted.
- **D10 export is mandatory MVP functionality**, not a later feature.
- **D11 no round-trip after eject.**
- **D12 use Gombit's contracts.** Never build a Forge-specific auth,
  migration, API, admin or ORM system.

From ADR-001:

- **Identity is the stable ID, never the name** (D1/D2). `label`,
  `storage_name` and `code_name` are separate naming domains. A relabel must
  never become a source-symbol change.
- **Code symbols are minted once, collision-checked, and frozen** (D3/D5).
  Deleting an entity tombstones its symbol; symbols are never recycled (D4).
- **Compiler-owned and user-owned files never mix** (D7). Generated code lives
  under `internal/forge_generated/**`; user code under `internal/extensions/**`.
  No marker regions, no partial ownership.
- **Forge never rewrites user implementation code** (D8). No `go/ast` rewriting
  of extension bodies to repair an ABI change.
- **Validation is impact-aware** (D11/D12). ABI-neutral edits commit even while
  unrelated user code is broken; ABI-breaking edits require compatibility
  proof.
- **Safety fails closed** (D14). When identity lineage can't be established,
  Forge refuses to infer a rename rather than guessing.

## Gombit integration boundary

Forge drives Gombit through coarse, project-level operations (ADR-001 §68–69),
never a per-entity subprocess protocol.

Gombit owns what DESIGN.md P4 forbids Forge from reimplementing: migration
diffing, OpenAPI generation, TypeScript client generation, authentication,
admin, GORM abstractions, **resource scaffolding semantics**, and embedded
builds. Never reimplement those.

### The `make resource` CLI is not the integration point

Verified against gombit v0.1.5: `gombit make resource` supports 6 scalar types
(`string, text, int, int64, bool, uint`) against Forge's 9 MVP field types,
refuses the `references=` modifier so it cannot express a relationship at all,
writes `internal/<name>/` rather than `internal/forge_generated/`, and wires
AutoMigrate — which DESIGN.md §14 forbids in deployed apps.

Note the installed binary on a given machine may be older. Check
`gombit version` before trusting a limitation you observed from the CLI.

### Ownership split (ADR-002, settled)

> **Gombit owns framework primitives; Forge owns application synthesis.**

Forge generates resource-specific application code — models, handlers, routes,
admin declarations and the extension API — under `internal/forge_generated/**`.
ADR-002 amends the P4 bullet on *resource scaffolding semantics* to scope it to
hand-written applications (`gombit make resource`); every other P4 bullet is
unchanged and still binding.

The constraint that makes this legitimate, and the one to enforce in review:

> **Generated output consumes Gombit's public/stable APIs and never duplicates
> Gombit infrastructure** (ADR-002 D3).

So generated code imports `framework`, `contract`, `auth`, `database` and
`admin` and registers through their documented entry points. It must never
contain a Forge-specific router, ORM layer, auth/permission/admin
implementation, divergent response envelope, or vendored Gombit internals, and
must never import Gombit *internal* packages. If generated code can't express
something through a public Gombit API, extend Gombit — don't synthesize a
private replacement.

P4's closing preference still stands: if Forge needs behavior that also belongs
in ordinary Gombit apps, add it to Gombit first. Adding the missing field types
to `resourcegen` upstream is still worth doing on its own merits.

### Version pinning

Pin **≥ v0.1.2** (ADR-002 D5): the module path was renamed there from
`github.com/LAA-Software-Engineering/gombit` to `github.com/gombit-dev/gombit`,
and current scaffolding emits the latter. Earlier notes in this repo described
the two paths as an unresolved question — that was an artifact of a stale
v0.1.0 binary, not an open decision.

The remaining ADR-001 §68 work for issue #4 is the versioned generation
*protocol*, not the module path.

## Conventions

- **Determinism is a contract, not a nicety.** Same compiler version + same
  spec must produce byte-identical output. No map iteration in ordered output,
  no timestamps, no randomness in generated artifacts. `internal/spec` has a
  golden file pinning canonical JSON; regenerate deliberately with
  `make golden`, never to make a test pass.
- **Authored order is preserved, not sorted.** Order is meaningful: it drives
  form field order and navigation order.
- **Three states stay separate** (ADR-001 §36): spec validity, ABI
  compatibility, and build health. Don't collapse them.
- **Validation accumulates diagnostics** rather than failing on the first
  problem, and every diagnostic carries a stable `Code` plus the offending
  entity's stable ID.
- **The graph refuses to build over an invalid spec**, so generation stages
  don't carry defensive nil checks. Preserve that invariant; if you add a
  reference to the graph, resolve it to a pointer or don't claim it's resolved.
- Standard library first. A dependency needs a reason.
- Comments explain why. A comment that describes behavior the code doesn't have
  is a bug, not a docs nit.

## Commands

```bash
make all           # fmt-check + vet + test + skills-check — the CI gate
make test          # go test ./... -count=1
make race          # go test ./... -race
make cover         # coverage summary
make golden        # rewrite the canonical-JSON golden file (deliberate only)
make skills-check  # .claude and .cursor skill trees must match byte for byte
```

CI runs `fmt-check`, `vet`, `skills-check`, `test` and `race` on push and PR.

The `.cursor/skills/` tree duplicates `.claude/skills/` because Cursor does not
reliably follow symlinks. Edit both sides; `skills-check` fails the build if
they drift.

## Working agreement

- Verify before asserting. This repo's design docs describe a system that
  mostly doesn't exist yet; read the code.
- Automated review findings are claims, not facts. Reproduce a reported defect
  against the current code before fixing it, and confirm the new test actually
  fails without the fix. A test that passes both ways proves nothing.
- Beware a panicking test: it aborts the binary, so later subtests silently
  never run and appear to pass.
- Don't commit, push, or open a PR unless asked. Never post to GitHub without
  being asked.
- Say what's done, what's skipped, and what's uncertain. Report failures with
  their output.
