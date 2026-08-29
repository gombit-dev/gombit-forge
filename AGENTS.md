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

**`gombit make resource` is not the integration point.** As of gombit v0.1.0 it
supports 4 of Forge's 9 MVP field types, rejects relationships outright
(`references=` is refused), writes `internal/<name>/` rather than
`internal/forge_generated/`, and wires AutoMigrate — which DESIGN.md §14
forbids in deployed apps. Forge generates models, handlers, routes and the
admin registry itself.

Gombit still owns everything DESIGN.md P4 lists: app scaffolding (`gombit new`),
Atlas migrations, OpenAPI, the TypeScript client, auth, admin, GORM and
`gombit build --embed`. Never reimplement those.

Unresolved: the installed binary generates apps requiring
`github.com/LAA-Software-Engineering/gombit v0.1.0` while the checkout's module
path is `github.com/gombit-dev/gombit`. ADR-001 §68 requires a pinned version;
settle this before writing generator imports (issue #4).

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
make all        # fmt-check + vet + test — the CI gate
make test       # go test ./... -count=1
make race       # go test ./... -race
make cover      # coverage summary
make golden     # rewrite the canonical-JSON golden file (deliberate only)
```

CI runs `fmt-check`, `vet`, `test` and `race` on push and PR.

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
