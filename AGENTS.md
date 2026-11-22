# AGENTS.md

Gombit Forge is a visual application builder that compiles a declarative
`ProjectSpec` into an ordinary Gombit application. Module path
`github.com/gombit-dev/gombit-forge`; each module's `go` directive is
authoritative for its toolchain (the control plane follows Gombit's, which
v0.9.0 raised to 1.26.9).

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
`Compile(spec, module)` builds the graph and runs every generator stage — models,
handlers, routes, admin, and the React CRUD frontend — into one deterministic
file tree. `compiler.MigrationModels` derives the model set for migration
generation, which the `internal/gombit` boundary drives through
`gombit db makemigrations` (Forge does not diff schemas itself).

**Every milestone — F0 and M0–M7 — is complete** (every milestone issue
closed); what remains open is a batch of filed `bug` issues, several against the
M4–M6 Cloud build/deploy surface. Verify with `gh issue list` and `git log`
before claiming otherwise — this paragraph is the thing most likely to drift.

The **compiler** (root module) is done and gated end to end. From a
`ProjectSpec`, `compiler.Compile` builds the resolved graph and runs every
generator stage — GORM models, the backend extension ABI (view/mutation/fields
+ the `extension` package, ADR-001 §23), Huma handlers, route and admin
registration, and a **page-driven** React frontend — into one deterministic file
tree. Pages generate tables, forms, details and dashboards; list handlers carry
declared server-side search / filter / sort (gombit #260), detail pages embed
FK-filtered related records, and dashboards carry count cards, recent-record
lists and numeric aggregate cards (gombit #273). `TestM0EndToEnd` scaffolds a
real Gombit app, compiles the spec, applies the migration on Postgres, builds,
boots and serves CRUD + admin (and a live `?aggregate=` query) with **no Forge
runtime dependency**. `compiler.MigrationModels` derives the model set;
`internal/gombit` drives `gombit db makemigrations` (Forge does not diff schemas
itself). GitHub source export ships as an async `ExportJob` (M7): freeze a
revision → queue → a worker runs `BuildApplicationSource` and publishes the repo.

The **control plane** (`controlplane/`, a nested Go module) is an ordinary Gombit
application — Forge dogfoods Gombit (D7) — and is runnable: cookie/session auth
with the admin plane auto-mounted (#35); org tenancy in `internal/org`
(Organization, Member, Invitation with per-org roles owner/admin/member and a
capability matrix, plus the invite → hashed-token → accept flow, #36); Project
and the immutable, append-only Revision chain in `internal/project`, each
revision pinning the exact canonical `spec.Marshal` bytes and `spec.Hash` with
`parent_revision_id` lineage (#37); the `internal/audit` service (#40); and the
full **authoring API** (#39) — create-project plus resource/field/relationship/
behavior/page/navigation/branding edits, each minting stable IDs, validating,
classifying the ABI transition and committing a new revision (a breaking
transition is returned 409, never built, D8). A React editor SPA in
`controlplane/web` (Vite + React + TS) drives that API, including a Deploy tab
for the Cloud integration below. Identity and RBAC are
Gombit's (`auth.User`, cookie session); the org role is tenancy Gombit can't
express, not a second identity store (D12). `internal/project` imports the root
module's `internal/spec` for canonicalization and hashing, so
`controlplane/go.mod` requires the root module with a `replace … => ../`.

The control plane's schema **is committed**: `internal/platform.Models()` is the
model set (auth, org tenancy, Project/Revision, audit, the GitHub-connect and
export-job tables, the cloud-build job queue, plus the `Project.CloudProjectID`
Cloud linkage) and `controlplane/database/migrations` holds the Atlas migrations
that create it (`20260830074350_initial.sql` plus the github-connect,
export-job and cloud-build-job follow-ups), authored via `gombit db
makemigrations` per §14 — deployment migrates, never AutoMigrate. Tests apply
the same committed migrations (`internal/dbtest`), not AutoMigrate, because
the cyclic FKs and the `project_revisions` append-only trigger are beyond what
AutoMigrate can express. #101
(migration), #39 (project API) and #40 (audit) are all **closed**; do not
describe them as open.

The **runtime** — build, deploy, environments, managed database, secrets,
domains — is Gombit Cloud's, not Forge's (ADR-005 D2/D6). Forge compiles a
revision to an ordinary Gombit application and hands the source to Cloud;
`Project.CloudProjectID` (#38, PR #100) is the whole of Forge's knowledge of its
Cloud counterpart. **M4–M6 shipped as a Cloud *client*, not a PaaS**:
`internal/cloudclient` is a narrow HTTP client, one call per concrete Gombit
Cloud §51 endpoint (create build, upload source, build state/logs,
environments, deployments, rollback); `internal/cloudbuild` is the async
build-job queue — `POST /projects/{id}/deploy` freezes a revision and enqueues,
never builds (D8) — and `internal/buildworker` assembles the frozen revision's
source, submits it to Cloud as Forge's service principal and mirrors Cloud's
build state onto the job. The remaining routes (preview environments, deploy a
build to an environment, deployment status/logs including the migration hold,
rollback) are thin pass-throughs; Cloud owns the build, the deployment state
machine, health and the migration gate. The Deploy tab in `controlplane/web`
surfaces build history, live build logs, preview, deploy with the
destructive-migration approval, and an app-log viewer. Cloud is configured by
`GOMBIT_CLOUD_API_URL` + `GOMBIT_CLOUD_API_TOKEN`; without both, deploy is
disabled. Don't grow a Forge-side runtime model (Environment, Build,
Deployment) or mock a Cloud endpoint the §51 API doesn't have. Secrets are
Cloud's too (ADR-005), never a Forge store.

The export and Cloud-build queues are tables in the control plane's Postgres,
claimed under a lease (`internal/joblease`). A worker renews the lease while it
works a job, and a job whose lease lapsed is failed with a reason the user sees,
not re-run, because a half-done export or build has side effects outside the
database. Gombit's jobs system is not used: its only durable driver is Redis.
Account email goes out through `internal/mail` (SMTP, `FORGE_SMTP_ADDR` +
`FORGE_MAIL_FROM`), the `auth.Mailer` Gombit's account lifecycle asks for. Its
links open the editor's account pages (`controlplane/web/src/auth/AccountPages.tsx`):
`/forgot-password` (linked from sign-in), `/reset-password`, `/verify-email`
and `/confirm-email`, served signed in or out. The links exist only when the
control plane sets `GOMBIT_AUTH_PASSWORD_RESET_URL`,
`GOMBIT_AUTH_EMAIL_VERIFICATION_URL` and `GOMBIT_AUTH_EMAIL_CHANGE_URL` to
those pages (`.env.example`); unset, the email carries a bare token.

The control plane's API contract is committed as `controlplane/openapi.json`:
the document a fully configured server serves, every optional route set on.
`TestOpenAPIDocumentIsCommitted` (`cmd/server`) builds the app with `newApp`,
as `main` does, and holds the file to it. The editor calls the API through the
TypeScript client `gombit client generate` writes from it
(`controlplane/web/src/api/generated`), so a path, parameter, body or response
that drifts from the contract fails the editor's typecheck. After an API change
run `make contract`, which regenerates both; CI's `web` job reruns the generator
with the gombit the control plane pins and fails on any difference
(`make contract-check`).
- The typed client is `contract` in `controlplane/web/src/api/client.ts`, and
  `call()` unwraps the D10 envelope into the editor's `ApiError`.
- Its transport is that file's `send()`. It echoes Gombit's `gombit_csrf`
  cookie as `X-CSRF-Token` on every state-changing request (login included),
  and refreshes an expired session once.
- The editor's unit tests stub `fetch` with real `Response`s
  (`src/test/http.ts`), so a login against a real control plane is the check
  that the transport still works.

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

Field coverage is no longer the reason. As of gombit v0.9.0, `gombit make
resource` covers every Forge field type, `belongs_to` included (it was 6
scalars and no relationships at v0.1.5). It is still not Forge's integration
point. It writes a human-owned model under `internal/<name>/` (model-first,
ADR-016), not a spec-derived `internal/forge_generated/`; it wires the model
into AutoMigrate, which DESIGN.md §14 forbids in deployed apps; and it generates
only list/get/create, where Forge's resources also update and delete.

Forge's handler templates (`internal/compiler/gen/handler_templates.go`) do
mirror the code `gombit make resource` generates for the operations both have:
the `database` list-query calls, the id parse, and the stored-row response.
Field types follow Gombit's too: an optional `date` or `datetime` is a
`*time.Time`, so a blank one is NULL (Gombit refuses the zero time from v0.8),
while optional integers and booleans stay plain values. The Go type is extension
ABI, so making such a field required, or optional, is a breaking transition.
When a Gombit release changes that generated code, the templates must follow
it. Compare against a resource generated by the new toolchain, and run the e2e
suite (`go test ./internal/compiler/`, which needs `gombit`, atlas and Docker)
with it. CI has no `gombit` binary, so the e2e tests skip there; the v0.8.0
`database.Search` change broke every generated app with a searchable field,
and only a local e2e run with the new toolchain catches that.

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
in ordinary Gombit apps, add it to Gombit first.

### Version pinning

Pin **≥ v0.8.0** (ADR-004 D5), tracked in `internal/gombit.MinimumVersion`.
The module path was renamed at v0.1.2 from
`github.com/LAA-Software-Engineering/gombit` to `github.com/gombit-dev/gombit`
(the original floor), and current scaffolding emits the latter. The floor
advanced to v0.1.11, which added the declared server-side list
filtering/sort/search (gombit #260) the generated list handlers consume, then to
v0.1.12, which added the declared server-side numeric aggregate contract (gombit
#273) the generated dashboard aggregate cards consume, then to v0.8.0, whose
`database.Search` takes the request context and returns an error: the generated
list handlers call that signature. The `controlplane` module
pins its own Gombit version in `controlplane/go.mod`, bumped independently
— read the pin there rather than trusting a number in prose; the floor only
moves when the generated code needs a newer contract. Dependabot proposes those
bumps, but a go.mod bump alone is not the upgrade. Run `gombit upgrade --to
vX.Y.Z` in `controlplane/` and do what it lists: v0.9.0, for one, added auth
tables that `auth.Models()` returns, which need a committed migration
(`gombit db makemigrations … --model …`) before the control plane runs it,
and v0.10.0 added `refresh_tokens` columns that need one too (`gombit db
plan` shows the change, `gombit db makemigrations upgrade_…` writes it). Run
`go mod tidy` in `controlplane/` last: `gombit upgrade` and the `gombit db`
commands add `ariga.io/atlas-provider-gorm` and its dependencies to `go.mod`
for their schema loader, and the control plane commits the tidy module. Earlier notes describing the two module paths as an
unresolved question were an artifact of a stale v0.1.0 binary, not an open
decision.

`internal/gombit` is the boundary. It exposes project-level operations only:
`Version`, `Scaffold` and `MakeMigrations` (#11). `MakeMigrations` also
declares the framework models the scaffold's migration registry omits for the
detected toolchain (`frameworkSchemaModels`): `gombit new` v0.9.0 leaves its
five new auth tables out of `models.json` while its AutoMigrate lists them, and
a Forge app's schema is its migration alone. v0.10.0 records every framework
model there itself (gombit #675), so this covers v0.9.x only and goes once
`MinimumVersion` passes it. Outside production
`gombit createsuperuser` AutoMigrates the auth tables itself, which is why the
e2e asserts them right after `gombit db migrate`. OpenAPI and the TypeScript
client are not boundary operations — the generated app produces them with
Gombit's own toolchain (`gombit client generate` against its `/openapi.json`),
and the generated pages consume that client rather than a Forge-written one
(#9).
The versioned generation *protocol* ADR-001 §68 describes is not built — the
current transport is the CLI behind an injectable runner.

Besides replacing the scaffold's `cmd/server/main.go` with the composition
root, the assembled tree edits one scaffold file: `ApplyPlatformModels` adds
the generated models to the `AutoMigrate` call in
`internal/platform/database.go` (`BuildApplicationSource`, and the M0 e2e,
which assembles the tree by hand). Nothing calls that function in a Forge app
(§14). Gombit's tooling reads it as the app's model list:
- `gombit db check` compares it with `models.json`, and reported every
  generated model as drift before this edit.
- `gombit make resource` adds to it.

So an exported app passes `gombit db check` and takes `gombit make resource`
like any Gombit app. Both e2e suites run `gombit db check` on the migrated
database.

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
make all           # fmt-check + vet + cp-vet + test + cp-test + skills-check
make test          # go test ./... -count=1
make race          # go test ./... -race
make cp-test       # control-plane tests (needs atlas + Docker; cp-test-short skips boot)
make web           # editor SPA: typecheck + build + unit tests
make cover         # coverage summary
make golden        # rewrite the canonical-JSON golden file (deliberate only)
make skills-check  # .claude and .cursor skill trees must match byte for byte
```

CI does not run `make all` verbatim. On push to `main` and on every PR it runs
`fmt-check`, `vet`, `skills-check`, `test` and `race` for the root module;
`cp-vet`, `cp-build` and `cp-test-short` for the control plane; a `web` job
(`web-install`, `web-check`, `web-build`, `web-test`); and an informational
`govulncheck` job that does not gate the merge.

The `.cursor/skills/` tree duplicates `.claude/skills/` because Cursor does not
reliably follow symlinks. Edit both sides; `skills-check` fails the build if
they drift.

## Working agreement

- Verify before asserting. The design docs describe intent, which has drifted
  from the code in both directions — much of the intended system now exists
  (F0–M7), and some described pieces never will (superseded by ADRs). Read
  the code and check `gh issue list`; don't trust a doc's tense.
- Automated review findings are claims, not facts. Reproduce a reported defect
  against the current code before fixing it, and confirm the new test actually
  fails without the fix. A test that passes both ways proves nothing.
- Beware a panicking test: it aborts the binary, so later subtests silently
  never run and appear to pass.
- Don't commit, push, or open a PR unless asked. Never post to GitHub without
  being asked.
- Say what's done, what's skipped, and what's uncertain. Report failures with
  their output.
