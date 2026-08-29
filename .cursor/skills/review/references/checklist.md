# Forge review checklist

Walk only the sections the diff touches. Each item is a contract to attack, not
a box to tick, and none of it substitutes for tracing the change end-to-end.

## Always

- Does every comment describe behavior the code actually has? A doc comment
  promising a fallback, an ordering, or an "empty for X" contract that the code
  doesn't implement is a bug, not a docs nit.
- Does the PR description claim something the diff doesn't do?
- Would a new test still pass against the pre-change code? If so it proves
  nothing. Verify by reverting the source and running it.
- Is a locked decision (AGENTS.md) being re-litigated without an ADR? That is
  **BLOCKING**.

## Identity and naming (ADR-001 D1–D6)

- Is identity ever inferred from a name? Stable ID is the only identity.
- Can a relabel change a `code_name`? It must not — that turns a cosmetic edit
  into a source ABI break.
- Is a code symbol minted anywhere without checking live symbols, tombstones
  and the reserved table? Collisions are resolved at mint time, never later.
- Can a deleted entity's symbol be reused? Tombstones are permanent.
- Are `label`, `storage_name` and `code_name` treated as separate domains, or
  has one rule leaked across them (e.g. rejecting Go keywords in a column name)?

## Spec and validation

- Does validation answer only "is the model valid", or has it absorbed ABI
  compatibility or build health? Those three states stay separate (§36).
- Does it accumulate diagnostics, or bail on the first?
- Can `Validate` panic on anything `Unmarshal` accepts? Null array entries are
  legal JSON.
- Does a new field admit states the type can represent but validation blesses?
  Prefer making invalid states unrepresentable.
- Is a default, enum, or literal checked against the type it will be emitted
  as? A value reaching the model and migration verbatim needs a real grammar,
  not `strconv` permissiveness.

## Determinism (DESIGN.md §9, §32)

- Any map iteration reaching ordered output?
- Any timestamp, hostname, absolute path, or random value in a generated
  artifact?
- Does authored order survive, or did something sort a user-meaningful order?
- If canonical JSON changed, was the golden file regenerated deliberately —
  or to make a failing test pass?
- Do two semantically identical specs hash identically? nil vs empty
  collections must not diverge; the digest is a lineage anchor (§60).

## Domain graph

- Does the graph claim to resolve a reference it leaves as a raw ID?
- Does a documented invariant ("every belongs_to has a Relationship") hold by
  construction, or does a skip-continue leave it fail-open?
- Does a view key off the discriminant (page `Type`), or off whichever optional
  block happens to be non-nil?

## Generated code and ownership (ADR-001 D7–D9, §15–19)

- Does compiler-owned output land only under `internal/forge_generated/**`?
- Does anything write, rewrite, or AST-edit `internal/extensions/**`? Forge may
  create a stub once and never touch it again.
- Are marker regions / mixed ownership being introduced? Rejected outright.
- Is a generated file missing its DO-NOT-EDIT banner?
- Does the extension surface leak GORM models or handler internals? The ABI is
  deliberately narrow.

## Gombit boundary (ADR-004, ADR-001 §68–69)

Gombit owns framework primitives; Forge owns application synthesis. ADR-004 D3
is a standing review obligation — no compiler enforces it, so drift toward a
private framework has to be caught here.

- Does generated output consume Gombit's **public** APIs (`framework`,
  `contract`, `auth`, `database`, `admin`), or has it grown its own?
  Specifically reject in generated code: a Forge-specific router, middleware
  chain or request lifecycle; an ORM layer over GORM; an auth, session,
  permission or admin implementation; a response envelope diverging from
  Gombit's D10 shape.
- Any import of a Gombit `internal/` package, or vendored/copied Gombit
  source? Both are forbidden outright.
- Is Forge reimplementing migrations, OpenAPI, TS client generation, auth,
  admin, or GORM abstractions? Still never — ADR-004 did not widen that.
- Is a per-resource or per-page Gombit subprocess being introduced? The
  protocol is coarse and project-level.
- Is Gombit pinned at ≥ v0.1.2, or is the integration floating?
- Could the need have been met by extending Gombit instead? P4's preference
  survived the amendment.

## Generated app independence (DESIGN.md D2, P5)

- Does the generated app acquire a runtime dependency on Forge? It must keep
  working if Forge disappears.
- Did AutoMigrate reach a deployed path? §14 forbids it.
