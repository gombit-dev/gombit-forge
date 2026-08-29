# Tutorial: from a `ProjectSpec` to a running app

This walks through the whole M0 pipeline by hand: define a two-resource
application model, compile it into a Gombit app, apply the migration on Postgres,
build it, boot it, and exercise the real HTTP contract — CRUD plus admin — on an
app that carries **no dependency on Forge**.

Every step here is exactly what the go/no-go gate,
[`TestM0EndToEnd`](../internal/compiler/e2e_test.go), does automatically. If you
just want to see it run, that test *is* the tutorial in executable form:

```bash
go test ./internal/compiler -run TestM0EndToEnd -v
```

Read on to do it yourself and understand each moving part.

## What we're building

A tiny CRM with two resources:

- **Customer** — `email` (string, required, unique) and `active` (boolean).
  Full CRUD, visible in the admin.
- **Invoice** — `customer` (a `belongs_to` reference to Customer, required) and
  `total` (decimal, required). Create-only, hidden from the admin.

The relationship, the decimal, and the two different behavior/visibility
settings are what make this a real exercise rather than a hello-world.

## Prerequisites

- **Go 1.25.7** (auto-resolves via `GOTOOLCHAIN`).
- The [`gombit`](https://github.com/gombit-dev/gombit) CLI, **≥ v0.1.2**. Check
  with `gombit version`.
- [`atlas`](https://atlasgo.io) and **Docker** — Gombit's migration diffing uses
  a throwaway Postgres, and we'll run the app's database in a container too.

Forge itself never imports Gombit; it drives the `gombit` CLI as a subprocess
through the [`internal/gombit`](../internal/gombit) boundary. The steps below
show that boundary explicitly.

## Step 1 — Describe the application model

A `ProjectSpec` is the source of truth (decision **D1**). Identity is the stable
ID, never the name; `label`, `code_name`, and `storage_name` are separate naming
domains (ADR-001). Here's the spec for our CRM:

```go
package main

import (
	"fmt"
	"log"

	"github.com/gombit-dev/gombit-forge/internal/compiler"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

func buildSpec() *spec.ProjectSpec {
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
	return s
}
```

A `belongs_to` field points at the target resource's stable ID via `Target`
(here, `customer`), not its name — a relabel of Customer never touches this
reference.

You can validate a spec on its own with `spec.Validate(s)`, which accumulates
diagnostics (each with a stable `Code` and the offending entity's ID) rather than
failing on the first problem. `compiler.Compile` runs this for you: the graph
refuses to build over an invalid spec.

## Step 2 — Compile the model into source

```go
func main() {
	s := buildSpec()

	// module is the *generated app's* Go module path.
	const module = "example.com/acme"

	files, err := compiler.Compile(s, module)
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range files {
		fmt.Println(f.Path)
	}
}
```

`compiler.Compile` builds the resolved domain graph (deriving Customer's
`has_many` from Invoice's `belongs_to`) and runs every generator stage into one
deterministic, gofmt-clean file tree. The output, stage by stage:

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

Note what the behavior toggles produced: Invoice has **no** `admin.go` (it's not
admin-visible) and **no** form page (it enables only create). Everything lives
under `internal/forge_generated/**` and `frontend/src/forge_generated/**` — the
compiler-owned roots. Forge never mixes generated and hand-written code
(ADR-001).

`register.go` is the composition root: it exposes `RegisterAll(app *framework.App) error`,
which mounts every resource's routes and admin declarations through Gombit's
public entry points. It's what makes the generated tree a working app instead of
a pile of unwired packages.

## Step 3 — Scaffold the Gombit shell

Forge generates *application* code; Gombit owns the *framework shell* — the
module, config loader, database plumbing, embedded frontend, and the management
CLI. Scaffold it with the `gombit` CLI (this is what `internal/gombit`'s
`CLI.Scaffold` shells out to):

```bash
gombit new app \
  --module example.com/app \
  --database postgres \
  --auth cookie \
  --ui minimal
cd app
```

Use the same module path you passed to `Compile`. `--database postgres` and
`--auth cookie` match decisions **D4** and **D5**.

> **Note:** `gombit new` writes a random `GOMBIT_JWT_SECRET` into `.env`, so the
> scaffold is not byte-reproducible. Forge's determinism contract covers only
> *compiler-owned* output, which is what Step 2 produced.

## Step 4 — Drop in the generated tree and own `main`

Write each file from Step 2 into the scaffolded project at its path, then replace
the scaffold's `cmd/server/main.go` with a Forge-owned composition root that
wires every resource through `RegisterAll` and — crucially — does **not** call
`AutoMigrate`. Migrations are applied out of band (DESIGN.md §14):

```go
package main

import (
	"context"
	"log"

	"github.com/gombit-dev/gombit/config"
	"github.com/gombit-dev/gombit/framework"

	forge "example.com/app/internal/forge_generated"
	"example.com/app/internal/platform"
	"example.com/app/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := platform.OpenDatabase(cfg.Database)
	if err != nil {
		log.Fatal(err)
	}
	app, err := framework.New(
		framework.WithConfig(cfg),
		framework.WithDatabase(db),
		framework.WithEmbeddedFrontend(web.FS()),
	)
	if err != nil {
		_ = db.Close()
		log.Fatal(err)
	}
	if err := forge.RegisterAll(app); err != nil {
		log.Fatal(err)
	}
	app.OnStop(func(context.Context) error { return db.Close() })
	if err := framework.Run(app); err != nil {
		log.Fatal(err)
	}
}
```

Then resolve imports:

```bash
go mod tidy
```

At this point the app depends on `github.com/gombit-dev/gombit`, `gorm`, and
`shopspring/decimal` — but **not** on `gombit-forge`. The generated code consumed
Gombit's public APIs and vanished the compiler from the dependency graph
(decision **D2**). You can prove it:

```bash
! grep -rq gombit-forge . --include='*.go' --include=go.mod && echo "no Forge dependency ✓"
```

## Step 5 — Generate and apply the migration

Forge does not diff schemas; Gombit does (via Atlas). Forge derives the model set
and hands it to `gombit db makemigrations` through the `internal/gombit` boundary
(`MigrationModelsForSpec` → `CLI.MakeMigrations`). By hand:

```bash
gombit db makemigrations initial --database postgres
```

Start a throwaway Postgres and point the app at it:

```bash
docker run -d --rm --name forge-tutorial-db \
  -e POSTGRES_PASSWORD=forge -e POSTGRES_DB=app \
  -p 127.0.0.1:5432:5432 postgres:15

export GOMBIT_DATABASE_DRIVER=postgres
export GOMBIT_DATABASE_DSN='postgres://postgres:forge@127.0.0.1:5432/app?sslmode=disable'
export GOMBIT_JWT_SECRET='tutorial-secret-please-change-0001'
export GOMBIT_AUTH_MODE=cookie
export GOMBIT_COOKIE_SECURE=false     # we speak plain HTTP locally
export GOMBIT_COOKIE_SAMESITE=Lax

gombit db migrate
```

The `customers` and `invoices` tables now exist.

## Step 6 — Build, boot, and log in

```bash
go build -o server ./cmd/server
export GOMBIT_HTTP_ADDR=127.0.0.1:8080
./server &
# wait for http://127.0.0.1:8080/livez to answer
```

Cookie mode gates writes with CSRF and the admin plane with a superuser. Create
one, then log in to get a session cookie and a CSRF token:

```bash
gombit createsuperuser --no-input \
  --email admin@example.test --password 'Password123!'

# CSRF token (unauthenticated), then log in, then a fresh CSRF token for the session.
curl -s -c jar.txt http://127.0.0.1:8080/api/v1/auth/csrf
curl -s -b jar.txt -c jar.txt -X POST http://127.0.0.1:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' -H 'X-CSRF-Token: <token>' \
  -d '{"email":"admin@example.test","password":"Password123!"}'
```

(The e2e test automates the cookie-jar/CSRF dance; the shape above is what it
sends.)

## Step 7 — Exercise the real contract

**Create a customer and read it back.** Responses use Gombit's `{data: ...}`
envelope (decision **D10**):

```bash
curl -s -b jar.txt -X POST http://127.0.0.1:8080/api/v1/customers \
  -H 'Content-Type: application/json' -H 'X-CSRF-Token: <token>' \
  -d '{"email":"e2e@example.test","active":true}'
# -> {"data":{"id":1,"email":"e2e@example.test","active":true, ...}}

curl -s -b jar.txt http://127.0.0.1:8080/api/v1/customers/1
```

**Create an invoice — the `belongs_to` and the decimal round-trip:**

```bash
curl -s -b jar.txt -X POST http://127.0.0.1:8080/api/v1/invoices \
  -H 'Content-Type: application/json' -H 'X-CSRF-Token: <token>' \
  -d '{"customer_id":1,"total":"19.95"}'
```

`total` comes back as the string `"19.95"` — a decimal, not a lossy float.

**Confirm the admin catalog honored the visibility toggles:**

```bash
curl -s -b jar.txt http://127.0.0.1:8080/api/v1/admin/meta
```

`customers` appears in `models`; `invoices` does not, because it isn't
admin-visible. That's proof `RegisterAll` actually ran `RegisterAdmin` with the
declarations the compiler emitted.

## Step 8 — Clean up

```bash
kill %1                          # the server
docker stop forge-tutorial-db    # --rm removes it
```

## What you just proved

From a declarative `ProjectSpec`, Forge synthesized an ordinary Gombit
application that compiles, migrates on Postgres, boots, serves CRUD with a real
relationship and a decimal, and exposes an admin — all with **no runtime
dependency on Forge**. Delete the compiler and this app keeps running. That is
the M0 go/no-go gate, and it's the contract every later milestone builds on.

## Where to go next

- [`docs/DESIGN.md`](DESIGN.md) — the roadmap (M1–M7) and the locked decisions.
- [`docs/ADR-001.md`](ADR-001.md) — identity, symbol allocation, and file
  ownership, which govern how the generator stays stable across edits.
- [`docs/ADR-004.md`](ADR-004.md) — the ownership split that makes the generated
  code legitimate: Gombit owns framework primitives, Forge owns application
  synthesis.
