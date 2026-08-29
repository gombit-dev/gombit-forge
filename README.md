# Gombit Forge

![status: pre-alpha](https://img.shields.io/badge/status-pre--alpha-orange)
![Go 1.25.7](https://img.shields.io/badge/go-1.25.7-00ADD8)
![license: AGPL--3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)

**Build visually. Ship normally. Own the code.**

Gombit Forge is a visual application builder that compiles a declarative
`ProjectSpec` into an ordinary [Gombit](https://github.com/gombit-dev/gombit)
application — Go + GORM + Huma on the backend, React + TypeScript on the
frontend, Atlas migrations, cookie/session auth, and admin. The generated
project is a normal repository you can build, run, and export; it has **no
runtime dependency on Forge**.

> The whole product fits in one sentence, and it is the test every feature must
> pass:
>
> **Forge edits a declarative application model; Gombit turns that model into
> ordinary software.**

## Status

**Pre-alpha.** The **M0 compiler spike — the go/no-go gate — is cleared.** From
a `ProjectSpec`, Forge generates a Gombit application that compiles, migrates on
Postgres, boots, serves CRUD and `/admin/`, and carries no Forge dependency.
This is proven end to end by `TestM0EndToEnd`.

What exists today is the **compiler**, as a Go library:

| Package | Role |
|---|---|
| `internal/spec` | `ProjectSpec`, stable IDs, canonical JSON, the semantic validator |
| `internal/compiler/graph` | the resolved domain graph (`belongs_to` → derived `has_many`) |
| `internal/compiler/gen` | code generators — models, handlers, routes, admin, the React frontend, and the `RegisterAll` composition root |
| `internal/compiler` | `Compile(spec, module)` — the pipeline that runs every stage into one deterministic file tree |
| `internal/gombit` | the coarse, versioned boundary to the Gombit toolchain (scaffold, migrations) |

There is **no** control plane, visual editor, build pipeline, or deploy path
yet — those are milestones M1–M7. See [`docs/DESIGN.md`](docs/DESIGN.md) §32 for
the roadmap.

## How it works

```text
ProjectSpec ──► Forge compiler ──► internal/forge_generated/**   (compiler-owned)
                                    frontend/src/forge_generated/**
                                        │
                                        ▼
                        an ordinary Gombit app  ──►  build ──► deploy
```

Forge owns **application synthesis** (the resource-specific code); Gombit owns
**framework primitives** (routing, ORM, migrations, auth, admin, the OpenAPI
and TypeScript client). Generated code only ever *consumes* Gombit's public
APIs — it never reimplements them ([`docs/ADR-004.md`](docs/ADR-004.md)).

## Requirements

- **Go 1.25.7** (the toolchain auto-resolves via `GOTOOLCHAIN`).
- To generate and run a real application: the [`gombit`](https://github.com/gombit-dev/gombit)
  CLI (**≥ v0.1.2**), [`atlas`](https://atlasgo.io), and Docker (Atlas uses a
  throwaway Postgres for migration diffs). Forge drives these; it does not
  vendor them, and the library itself depends only on `gorm` and
  `shopspring/decimal`.

## Quickstart

```bash
git clone https://github.com/gombit-dev/gombit-forge
cd gombit-forge

# Fast unit tests (no external toolchain needed).
go test ./... -short

# The full gate: fmt-check, vet, tests, and the skill-tree check.
make all

# The go/no-go proof: scaffolds a real app, migrates on Postgres, builds,
# boots, and exercises CRUD + admin. Needs gombit, atlas, and docker; it
# skips automatically if any is missing.
go test ./internal/compiler -run TestM0EndToEnd -v
```

## Quick example

Forge compiles a `ProjectSpec` into a file tree. This program builds a
two-resource spec in memory and prints what the compiler would write:

```go
package main

import (
	"fmt"
	"log"

	"github.com/gombit-dev/gombit-forge/internal/compiler"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

func main() {
	customer := spec.MustNewID(spec.KindResource)

	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: spec.MustNewID(spec.KindProject), Name: "Acme CRM", Slug: "acme-crm"},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{
				ID: customer, Label: "Customer", LabelPlural: "Customers",
				CodeName: "Customer", StorageName: "customers",
				Behavior: spec.ResourceBehavior{
					CreateEnabled: true, UpdateEnabled: true, DeleteEnabled: true, AdminVisible: true,
				},
				Fields: []*spec.Field{
					{ID: spec.MustNewID(spec.KindField), Label: "Email", Type: spec.TypeString,
						CodeName: "Email", StorageName: "email", Required: true, Unique: true},
					{ID: spec.MustNewID(spec.KindField), Label: "Active", Type: spec.TypeBoolean,
						CodeName: "Active", StorageName: "active"},
				},
			},
			{
				ID: spec.MustNewID(spec.KindResource), Label: "Invoice", LabelPlural: "Invoices",
				CodeName: "Invoice", StorageName: "invoices",
				Behavior: spec.ResourceBehavior{CreateEnabled: true, AdminVisible: false},
				Fields: []*spec.Field{
					{ID: spec.MustNewID(spec.KindField), Label: "Customer", Type: spec.TypeBelongsTo,
						CodeName: "Customer", StorageName: "customer_id", Required: true, Target: customer},
					{ID: spec.MustNewID(spec.KindField), Label: "Total", Type: spec.TypeDecimal,
						CodeName: "Total", StorageName: "total", Required: true},
				},
			},
		},
	}

	// module is the generated application's Go module path.
	files, err := compiler.Compile(s, "example.com/acme")
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range files {
		fmt.Println(f.Path)
	}
}
```

Output — the compiler-owned tree, emitted stage by stage (models, handlers,
admin, frontend, then the composition root), ready to drop into a scaffolded
Gombit app:

```text
internal/forge_generated/customer/model.go
internal/forge_generated/invoice/model.go
internal/forge_generated/customer/handlers.go
internal/forge_generated/customer/routes.go
internal/forge_generated/invoice/handlers.go
internal/forge_generated/invoice/routes.go
internal/forge_generated/customer/admin.go
frontend/src/forge_generated/customer/CustomerListPage.tsx
frontend/src/forge_generated/customer/CustomerDetailPage.tsx
frontend/src/forge_generated/customer/CustomerFormPage.tsx
frontend/src/forge_generated/invoice/InvoiceListPage.tsx
frontend/src/forge_generated/invoice/InvoiceDetailPage.tsx
frontend/src/forge_generated/invoice/InvoiceFormPage.tsx
frontend/src/forge_generated/resources.tsx
internal/forge_generated/register.go
```

`invoice` has no `admin.go` and no form page because it is not admin-visible and
enables only create. The output is **deterministic**: the same compiler version
and spec produce byte-identical files.

To turn this into a running application — scaffold, migrate, build, and run —
follow the [tutorial](docs/tutorial.md).

## Documentation

- [**Tutorial**](docs/tutorial.md) — build and run a two-resource app end to end.
- [**Design**](docs/DESIGN.md) — product scope, milestones, and the locked MVP decisions.
- [**ADR-001**](docs/ADR-001.md) — identity, symbol allocation, file ownership, and the extension ABI.
- [**ADR-004**](docs/ADR-004.md) — generation ownership: framework primitives vs application synthesis.
- [**Contributing**](CONTRIBUTING.md) — how to build, test, and land a change.
- [**Code of Conduct**](CODE_OF_CONDUCT.md).

## Locked decisions

These are settled (DESIGN.md §33) and should not be re-litigated without an ADR:
the `ProjectSpec` is the source of truth (D1); generated apps are ordinary
Gombit apps with no Forge runtime (D2); managed hosting is PostgreSQL-only (D4)
with cookie/session auth (D5); pages are structured, not a freeform canvas (D6);
export is mandatory and one-way (D10/D11); and Forge uses Gombit's contracts
rather than building its own auth, migration, API, admin, or ORM (D12).

## License

Gombit Forge is licensed under the **GNU Affero General Public License v3.0**.
See [LICENSE](LICENSE).
