# AGENTS.md

Gombit Forge is a visual application builder that compiles a declarative
`ProjectSpec` into an ordinary Gombit application. Module path
`github.com/gombit-dev/gombit-forge`, Go 1.25.7.

The whole product fits in one sentence, and it is the test every feature must
pass:

> Forge edits a declarative application model; Gombit turns that model into
> ordinary software.

## Current state

Pre-alpha. `internal/spec` holds `ProjectSpec`, stable IDs, canonical JSON and
the semantic validator; `internal/compiler/graph` holds the resolved domain
graph; `internal/gombit` is the project-level toolchain boundary and can
scaffold an application shell; `internal/compiler/gen` generates GORM models,
Huma handlers, route registration and admin registration into
`internal/forge_generated/<resource>/`; `internal/compiler` is the pipeline —
`Compile(spec)` builds the graph and runs every generator stage — models,
handlers, routes, admin, and the React CRUD frontend — into one deterministic
file tree. `compiler.MigrationModels` derives the model set for migration
generation, which the `internal/gombit` boundary drives through
`gombit db makemigrations` (Forge does not diff schemas itself).

M0 (the go/no-go gate) is **cleared**: issues #2–#12 shipped (PRs #86–#94 plus
the e2e harness). The end-to-end test in `internal/compiler` scaffolds a real
Gombit app, compiles the spec, applies the migration on Postgres, builds, boots
and serves customers/invoices/admin with no Forge runtime dependency.

M1 (control plane) is **in progress**. `controlplane/` is a nested Go module
holding the control plane as an ordinary Gombit application (Forge dogfoods
Gombit, D7) — cookie/session auth, Postgres-backed, admin plane auto-mounted by
`framework.New` in cookie mode (#35). Forge tenancy exists (#36): `internal/org`
holds Organization, Member and Invitation with per-org Forge roles (owner/admin/
member) and a capability matrix, plus the invitation flow (invite → hashed
token → accept) over cookie-gated Huma routes; `internal/audit` is the minimal
audit seam (§23) the invitation flow records to (the audit *service* is #40).
Identity and RBAC are Gombit's (`auth.User`, cookie session) — the org role is
tenancy Gombit can't express, not a second identity store (D12). Projects exist
too (#37): `internal/project` holds Project and the immutable, append-only
Revision chain — each revision pins the exact canonical `spec.Marshal` bytes and
`spec.Hash`, with `parent_revision_id` lineage (DESIGN.md §8, ADR-001 §60). This
is the first place the control plane imports the compiler: `internal/project`
uses the root module's `internal/spec` for canonicalization and hashing (the
single source of truth for spec bytes), so `controlplane/go.mod` now requires
the root module with a `replace … => ../`. Project also carries a
`CloudProjectID` linkage (ADR-005 D6) — the whole of Forge's knowledge of its
Gombit Cloud counterpart. The runtime models (Environment, Build, Deployment,
Domain) are **not** in the Forge control plane: they are owned by Gombit Cloud
(ADR-005 D2). Forge compiles a revision to an ordinary Gombit application and
hands it to Cloud, which owns build, deployment, environments, databases,
secrets and domains. #38 was originally a set of `internal/deploy` models here;
ADR-005 reduced PR #100 to the `CloudProjectID` linkage instead.

`internal/platform.Models()` is the control plane's schema of record; tests
AutoMigrate it, but **no Atlas migration is committed yet** — deployment can't
create these tables until one is (§14 forbids AutoMigrate in the deployed app).
The model set is the authoring loop only — org tenancy, Project/Revision, audit,
plus the `CloudProjectID` linkage — and #101 authors its initial migration
(org/project FKs, `head_revision_id` ON DELETE SET NULL, revision immutability).
Still open for M1: that migration (#101, now scoped to the reduced set), the
project/revision API (#39, which also adds the authorized project-create path),
the audit service (#40), and — as Cloud-integration client work, not a Forge
PaaS — the build/preview/deploy paths (M4–M6, re-scoped by ADR-005). Secrets
(#41) moved to Gombit Cloud. Don't describe any of those as existing.

The repo is **two Go modules**. The root module
(`github.com/gombit-dev/gombit-forge`) is the compiler and stays gombit-free —
it drives Gombit only through the CLI. The `controlplane/` module
(`…/controlplane`) is the Gombit app and *does* import Gombit; it is where the
runtime dependency on Gombit is allowed to live. It also imports the root
module's `internal/spec` (via `replace … => ../`) — allowed because the two
modules share the `gombit-forge/` path prefix, and `internal/spec` is itself
gombit-free, so this does not drag Gombit into the compiler. Root `./...` never
reaches into the nested module, so `make test`/`make vet` stay gombit-free; the
control plane
has its own `cp-build`/`cp-vet`/`cp-test` targets that `cd controlplane` first,
folded into `make all` and mirrored in CI. A `go.work` at the repo root is a
local-dev convenience for editing both modules at once — create it with `go work
init . ./controlplane`. It is **gitignored, not committed** (the explicit
per-module targets are what CI relies on), so don't assume it exists in a fresh
checkout. Never run `go work sync`: it rewrites the root module's go.sum to the
workspace-wide versions, which reintroduces a Gombit-derived dependency graph
into the gombit-free compiler module and breaks its module-mode build.

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
- `docs/ADR-004.md` is authoritative for generation ownership and amends
  DESIGN.md P4. Read it before writing any generator.
- `docs/ADR-005.md` is authoritative for the Forge/Cloud boundary: Forge owns
  the authoring loop, Gombit Cloud owns the runtime primitives (build, deploy,
  environments, managed DB, secrets, domains, logs, health, rollback). It
  supersedes the runtime-platform parts of DESIGN.md (§5, §11–13, §15, §21,
  §24–27) and re-scopes M4–M6 to "integrate Cloud," not "build a PaaS." Read it
  before touching the control-plane model set, the build pipeline, or deploy.
- GitHub issues are the unit of work. Epics are labelled `epic`; don't start a
  task whose epic describes an unmet prerequisite.

## Locked decisions (do not re-litigate)

From DESIGN.md §33:

- **D1 spec-first.** `ProjectSpec` is the source of truth. Generated source is
  not an editable round-trip representation.
- **D2 compiler, not runtime.** Generated apps are normal Gombit apps with no
  Forge runtime dependency. A deployed app must keep working if Forge vanishes.
- **D3 isolated per project.** No shared generated-app runtime. *(Realized in
  Gombit Cloud, ADR-005 — Cloud's runtime enforces the isolation, not Forge.)*
- **D4 PostgreSQL only** for managed hosting. Export may target any driver.
  *(Managed hosting is Cloud's scope, ADR-005.)*
- **D5 cookie/session auth** for managed apps.
- **D6 structured pages, not a freeform canvas.** Tables, forms, details,
  dashboard. No absolute positioning.
- **D7 Forge itself uses Gombit.** The control plane is a Gombit application;
  dogfooding is locked, not a preference.
- **D8 builds are asynchronous.** No HTTP request performs a build. *(Cloud's
  build queue, ADR-005 D2 — Forge submits source and observes; it does not
  build.)*
- **D9 build execution is isolated.** Generated source is untrusted. *(Enforced
  by Cloud's disposable workers, ADR-005.)*
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

### Ownership split (ADR-004, settled)

> **Gombit owns framework primitives; Forge owns application synthesis.**

Forge generates resource-specific application code — models, handlers, routes,
admin declarations and the extension API — under `internal/forge_generated/**`.
ADR-004 amends the P4 bullet on *resource scaffolding semantics* to scope it to
hand-written applications (`gombit make resource`); every other P4 bullet is
unchanged and still binding.

The constraint that makes this legitimate, and the one to enforce in review:

> **Generated output consumes Gombit's public/stable APIs and never duplicates
> Gombit infrastructure** (ADR-004 D3).

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

Pin **≥ v0.1.11** (ADR-004 D5), tracked in `internal/gombit.MinimumVersion`.
The module path was renamed at v0.1.2 from
`github.com/LAA-Software-Engineering/gombit` to `github.com/gombit-dev/gombit`
(the original floor), and current scaffolding emits the latter. The floor
advanced to v0.1.11, which added the declared server-side list
filtering/sort/search (gombit #260) the generated list handlers consume; the
`controlplane` module requires v0.1.11 to match. Earlier notes describing the
two module paths as an unresolved question were an artifact of a stale v0.1.0
binary, not an open decision.

`internal/gombit` is the boundary. It exposes project-level operations only;
migrations, OpenAPI and TypeScript client generation join it with #11 and #9.
The versioned generation *protocol* ADR-001 §68 describes is not built — the
current transport is the CLI behind an injectable runner.

### Known hazard: the scaffold is not byte-reproducible

`gombit new` writes a random `GOMBIT_JWT_SECRET` into `.env`, so scaffolding
the same project twice does not produce identical trees. Forge's determinism
contract covers **compiler-owned output** (`internal/forge_generated/**`,
`frontend/src/forge_generated/**`), which is generated from the spec and is
reproducible.

M0's determinism criterion is exactly that compiler-owned-output reproducibility,
which the unit tests pin; the M0 gate does **not** claim a byte-identical *whole
app* build, and `gombit new`'s random secret is why. A future claim of
byte-identical full builds must decide explicitly what it covers — exclude
environment files from the comparison, or have a later stage own secret material
— and not quietly narrow the claim to whatever happens to pass.

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
