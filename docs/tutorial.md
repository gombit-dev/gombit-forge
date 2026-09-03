# Tutorial: from a `ProjectSpec` to a running app

You'll take a declarative application model — a `spec.json` — and turn it into an
ordinary [Gombit](https://github.com/gombit-dev/gombit) application: a typed Go
API with CRUD, a `belongs_to` relationship and a decimal, server-side
search/filter/sort, a numeric aggregate, a migration on Postgres, a working
admin, and a generated React frontend (tables, forms, details, a dashboard) —
all with **no runtime dependency on Forge**. Delete the compiler afterwards and
the app keeps running.

**Time:** about 45 minutes.
**Prerequisites:** Go 1.25.7 (auto-resolves via `GOTOOLCHAIN`), the
[`gombit`](https://github.com/gombit-dev/gombit) CLI **≥ v0.1.12**
(`go install github.com/gombit-dev/gombit/cmd/gombit@v0.1.12`),
[Atlas](https://atlasgo.io), Docker, and — for the frontend chapters — Node 22+.

Every chapter ends with a **✅ Checkpoint**. If one fails, stop there — later
chapters build on it. The finished model is committed at
[`examples/acme-crm/spec.json`](../examples/acme-crm/spec.json), and the backend
half of this flow is what the go/no-go gate
[`TestM0EndToEnd`](../internal/compiler/e2e_test.go) runs automatically:

```bash
go test ./internal/compiler -run TestM0EndToEnd -v
```

| # | Chapter |
| --- | --- |
| 1 | [Describe the application model](#1-describe-the-application-model) |
| 2 | [Compile the model into source](#2-compile-the-model-into-source) |
| 3 | [Scaffold the Gombit shell](#3-scaffold-the-gombit-shell) |
| 4 | [Drop in the generated tree and own `main`](#4-drop-in-the-generated-tree-and-own-main) |
| 5 | [Generate and apply the migration](#5-generate-and-apply-the-migration) |
| 6 | [Build, boot, and log in](#6-build-boot-and-log-in) |
| 7 | [Exercise the API — CRUD, search, filter, sort, aggregate](#7-exercise-the-api) |
| 8 | [Run the generated frontend](#8-run-the-generated-frontend) |
| 9 | [Ship it](#9-ship-it) |
| 10 | [What you proved, and where next](#10-what-you-proved-and-where-next) |

---

## 1. Describe the application model

A `ProjectSpec` is the source of truth (decision **D1**). Its on-disk form is
**canonical JSON** — the same bytes the control plane stores as a revision and
hashes for lineage. There is no YAML; the JSON *is* the format. In the product
you author it through the visual editor, which mints IDs and writes the JSON for
you; here you'll start from the committed
[`examples/acme-crm/spec.json`](../examples/acme-crm/spec.json).

Copy it into a working directory:

```bash
mkdir acme && cd acme
curl -sSL https://raw.githubusercontent.com/gombit-dev/gombit-forge/main/examples/acme-crm/spec.json -o spec.json
```

It's a two-resource CRM. **Customer** — `name`, `email` (unique), `tier` (enum),
`active` (bool), `signed_up_at` (datetime); full CRUD, admin-visible, with
`name`/`email` searchable, `name`/`signed_up_at` sortable, `tier`/`active`
filterable. **Invoice** — `customer` (a `belongs_to`), `total` (decimal),
`status` (enum), `issued_at` (datetime); create/update only, hidden from the
admin, with `total` **aggregatable**. Plus four pages — a dashboard, a table,
and a form and detail per resource — and a navigation bar.

The shape, with the important cross-references highlighted:

```jsonc
{
  "spec_version": 1,
  "project": { "id": "prj_…", "name": "Acme CRM", "slug": "acme-crm" },
  "database": { "driver": "postgres" },
  "auth": { "mode": "cookie" },
  "resources": [
    {
      "id": "res_…C",                       // the Customer resource
      "code_name": "Customer",              // the frozen Go symbol
      "storage_name": "customers",          // the DB table name
      "fields": [
        { "id": "fld_…name", "label": "Name",  "type": "string", "code_name": "Name",  "storage_name": "name",  "required": true },
        { "id": "fld_…tier", "label": "Tier",  "type": "enum",   "code_name": "Tier",  "storage_name": "tier",
          "enum_values": [ { "value": "free" }, { "value": "pro" } ] }
        // …
      ],
      "behavior": {
        "create_enabled": true, "update_enabled": true, "delete_enabled": true, "admin_visible": true,
        "searchable_fields": ["fld_…name", "fld_…email"],   // → ?search=
        "sortable_fields":   ["fld_…name", "fld_…signed"],  // → ?ordering=
        "filterable_fields": ["fld_…tier", "fld_…active"]   // → ?tier=&active=
      }
    },
    {
      "id": "res_…I", "code_name": "Invoice", "storage_name": "invoices",
      "fields": [
        { "id": "fld_…cust",  "type": "belongs_to", "storage_name": "customer_id",
          "target": "res_…C" },                            // ← points at Customer by ID, never by name
        { "id": "fld_…total", "type": "decimal", "storage_name": "total", "required": true }
        // …
      ],
      "behavior": {
        "create_enabled": true, "update_enabled": true, "admin_visible": false,
        "aggregatable_fields": ["fld_…total"]              // → ?aggregate=sum:total
      }
    }
  ],
  "pages": [
    { "type": "dashboard", "slug": "home", "dashboard": {
        "count_cards":     [ { "label": "Customers",       "resource": "res_…C" } ],
        "recent_lists":    [ { "label": "Recent invoices", "resource": "res_…I", "limit": 5, "order_by": "fld_…issued" } ],
        "aggregate_cards": [ { "label": "Total invoiced",  "resource": "res_…I", "field": "fld_…total", "op": "sum" } ] } },
    { "type": "resource_table",  "slug": "customers", "resource": "res_…C",
      "table": { "columns": ["fld_…name","fld_…email","fld_…tier"], "search": true, "filters": ["fld_…tier"] } },
    { "type": "resource_form",   "slug": "edit-customer", "resource": "res_…C" },
    { "type": "resource_detail", "slug": "customer",      "resource": "res_…C" }
    // …invoices table / form / detail
  ],
  "navigation": [ { "label": "Home", "target": "page", "page": "pag_…home" }, … ]
}
```

Everything references everything else **by stable ID, never by name** (ADR-001):
a `belongs_to`'s `target`, a page's `resource`, a behavior's field lists, a
dashboard card's `field`, a nav entry's `page`. Relabelling "Customer" to
"Client" changes only that resource's `label`; every ID reference is untouched,
and no generated Go symbol moves. That is why hand-editing this file is awkward
and the editor exists — but the file is the contract, and it is the thing under
version control.

You can validate a spec on its own before compiling anything:

```go
data, _ := os.ReadFile("spec.json")
s, _ := spec.Unmarshal(data)                 // rejects unknown fields
if d := spec.Validate(s); d != nil {         // accumulates diagnostics, doesn't stop at the first
	log.Fatal(d.Error())
}
```

Each diagnostic carries a stable `Code` and the offending entity's ID — for
example, naming a field `created_at` fails with `reserved_name` because
`gorm.Model` already provides it.

**✅ Checkpoint** — `spec.json` is in your working directory and, run through
`spec.Unmarshal` + `spec.Validate`, reports no diagnostics.

---

## 2. Compile the model into source

Write a tiny program that loads the spec and compiles it. `compiler.Compile`
runs `spec.Validate` for you and refuses to build over an invalid spec, so the
generator stages never see a broken model:

```go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/gombit-dev/gombit-forge/internal/compiler"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

func main() {
	data, err := os.ReadFile("spec.json")
	if err != nil {
		log.Fatal(err)
	}
	s, err := spec.Unmarshal(data)
	if err != nil {
		log.Fatal(err)
	}

	// module is the *generated app's* Go module path — the same one you pass to
	// `gombit new` in chapter 3, so register.go's imports resolve.
	files, err := compiler.Compile(s, "example.com/app")
	if err != nil {
		log.Fatal(err)
	}
	for _, f := range files {
		fmt.Println(f.Path)
	}
}
```

`compiler.Compile` builds the resolved domain graph — deriving Customer's
`has_many` from Invoice's `belongs_to` — and runs every generator stage into one
deterministic, gofmt-clean file tree. From this spec it writes **23 files**:

```text
internal/forge_generated/customer/model.go        # GORM models
internal/forge_generated/invoice/model.go
internal/forge_generated/customer/view.go         # the backend extension ABI:
internal/forge_generated/invoice/view.go          #   typed, frozen accessors your
internal/forge_generated/customer/mutation.go     #   hand-written extensions call
internal/forge_generated/invoice/mutation.go      #   (ADR-001 §23) — never edited
internal/forge_generated/extension/extension.go
internal/forge_generated/customer/fields.go
internal/forge_generated/invoice/fields.go
internal/forge_generated/customer/handlers.go     # Huma handlers + list query
internal/forge_generated/customer/routes.go       # explicit route registration
internal/forge_generated/invoice/handlers.go
internal/forge_generated/invoice/routes.go
internal/forge_generated/customer/admin.go        # admin registration (Customer only)
frontend/src/forge_generated/customer/CustomersTablePage.tsx
frontend/src/forge_generated/customer/EditCustomerFormPage.tsx
frontend/src/forge_generated/customer/CustomerDetailPage.tsx
frontend/src/forge_generated/invoice/InvoicesTablePage.tsx
frontend/src/forge_generated/invoice/EditInvoiceFormPage.tsx
frontend/src/forge_generated/invoice/InvoiceDetailPage.tsx
frontend/src/forge_generated/dashboard/HomeDashboardPage.tsx
frontend/src/forge_generated/resources.tsx        # routes + nav + branding registry
internal/forge_generated/register.go              # RegisterAll composition root
```

Three things to read out of that tree:

- **The behavior toggles shaped it.** Invoice has **no** `admin.go` because it
  isn't admin-visible — the only file its toggles drop. It still gets a form
  page: a create-or-update resource needs one, and only a fully read-only
  resource drops it.
- **Generation is page-driven.** The frontend `.tsx` files exist because the
  spec declares pages, and each component is named from its page slug
  (`customers` → `CustomersTablePage`, `edit-customer` → `EditCustomerFormPage`).
  A spec with no pages generates no page components — just an (empty) registry.
- **Compiler-owned and hand-written code never mix** (ADR-001 D7). Everything
  lives under `internal/forge_generated/**` and `frontend/src/forge_generated/**`;
  your own extensions live elsewhere and are never overwritten.

`register.go` exposes `RegisterAll(app *framework.App) error`, which mounts every
resource's routes and admin declarations through Gombit's public entry points —
the thing that makes the tree a working app rather than unwired packages.

The output is **deterministic**: the same compiler version and spec produce
byte-identical files, every run.

**✅ Checkpoint** — running the program prints the 23 paths above.

---

## 3. Scaffold the Gombit shell

Forge generates *application* code; Gombit owns the *framework shell* — the
module, config loader, database plumbing, the React app skeleton, and the
management CLI. Scaffold it (this is exactly what `internal/gombit`'s
`CLI.Scaffold` shells out to):

```bash
gombit new app \
  --module example.com/app \
  --database postgres \
  --auth cookie \
  --ui minimal
cd app
```

Use the **same module path** you passed to `Compile`. `--database postgres` and
`--auth cookie` match decisions **D4** and **D5**; `--ui minimal` gives a
headless React skeleton (pass `--ui mui` for a Material UI shell — the generated
Forge pages are plain and work under either).

> `gombit new` writes a random `GOMBIT_JWT_SECRET` into `.env`, so the scaffold
> is **not** byte-reproducible. Forge's determinism contract covers only
> *compiler-owned* output, which is what chapter 2 produced.

**✅ Checkpoint**

```bash
go build ./...
```

Compiles with no output — `gombit new` pinned `go.mod` and ran `go mod tidy`.

---

## 4. Drop in the generated tree and own `main`

Chapter 2 only *printed* the paths. Now write the compiled bytes into the
scaffold — each `gen.File` carries its repo-relative `Path` and `Content`. This
is the loop the e2e harness uses:

```go
// dir is the scaffolded project root, e.g. "./app".
for _, f := range files {
	full := filepath.Join(dir, filepath.FromSlash(f.Path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(full, f.Content, 0o644); err != nil {
		log.Fatal(err)
	}
}
```

Then replace the scaffold's `cmd/server/main.go` with a Forge-owned composition
root that wires every resource through `RegisterAll` and — crucially — does
**not** call `AutoMigrate` (migrations are applied out of band, DESIGN.md §14):

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

Resolve imports, then prove the app no longer depends on Forge:

```bash
go mod tidy
! grep -rq gombit-forge . --include='*.go' --include=go.mod && echo "no Forge dependency ✓"
```

The app now depends on `github.com/gombit-dev/gombit`, `gorm`, and
`shopspring/decimal` — but **not** on `gombit-forge`. The generated code consumed
Gombit's public APIs and vanished the compiler from the dependency graph
(decision **D2**).

**✅ Checkpoint** — `go build ./...` succeeds and the `grep` prints
`no Forge dependency ✓`.

---

## 5. Generate and apply the migration

Forge does not diff schemas; Gombit does, via Atlas. Forge derives the model set
with `MigrationModelsForSpec` and hands it to `gombit db makemigrations` through
the `internal/gombit` boundary. Each model is passed as
`--model <import-path>.<Type>` so it enters the Atlas registry (the flag is
`--driver`, not `--database`) — the exact argv the boundary builds:

```bash
gombit db makemigrations initial --driver postgres \
  --model example.com/app/internal/forge_generated/customer.Customer \
  --model example.com/app/internal/forge_generated/invoice.Invoice
```

Without the `--model` flags the models never enter the registry and the
migration is empty. Start a throwaway Postgres and apply it:

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

**✅ Checkpoint** — `gombit db status` shows the `initial` migration applied, and
the `customers` and `invoices` tables now exist.

---

## 6. Build, boot, and log in

```bash
go build -o server ./cmd/server
export GOMBIT_HTTP_ADDR=127.0.0.1:8080
./server &
# wait for http://127.0.0.1:8080/livez to answer
```

Cookie mode gates writes with CSRF and the admin plane with a superuser. Create
one, then log in for a session cookie and a CSRF token:

```bash
gombit createsuperuser --no-input \
  --email admin@example.test --password 'Password123!'

curl -s -c jar.txt http://127.0.0.1:8080/api/v1/auth/csrf
CSRF=$(grep -i csrf jar.txt | awk '{print $7}')
curl -s -b jar.txt -c jar.txt -X POST http://127.0.0.1:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" \
  -d '{"email":"admin@example.test","password":"Password123!"}'
CSRF=$(grep -i csrf jar.txt | awk '{print $7}')
```

**✅ Checkpoint** — `/livez` answers and the login response carries a `data`
object rather than an error envelope.

---

## 7. Exercise the API

Responses use Gombit's envelope — success is `{"data": …, "meta"?: …}`, and the
generated handlers consume it rather than inventing their own (ADR-004 D3).

**Create a customer, then an invoice — the `belongs_to` and the decimal
round-trip:**

```bash
curl -s -b jar.txt -X POST http://127.0.0.1:8080/api/v1/customers \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" \
  -d '{"name":"Acme Co","email":"ap@acme.test","tier":"pro","active":true}'
# -> {"data":{"id":1,"name":"Acme Co", …}}

curl -s -b jar.txt -X POST http://127.0.0.1:8080/api/v1/invoices \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" \
  -d '{"customer_id":1,"total":"19.95","status":"sent","issued_at":"2026-01-15T00:00:00Z"}'
```

`total` comes back as the string `"19.95"` — a decimal, not a lossy float.

**The declared list query (gombit #260), straight from the spec's behavior
allowlists.** Customer declared `name`/`email` searchable, `tier`/`active`
filterable, `name`/`signed_up_at` sortable:

```bash
curl -s -b jar.txt 'http://127.0.0.1:8080/api/v1/customers?search=acme'       # LIKE over name/email
curl -s -b jar.txt 'http://127.0.0.1:8080/api/v1/customers?tier=pro&active=true'   # exact-match filters
curl -s -b jar.txt 'http://127.0.0.1:8080/api/v1/customers?ordering=-signed_up_at' # sort, - = descending
```

A field the resource didn't declare is rejected — `?ordering=email` returns a
422, because the allowlist is enforced server-side, not just hidden in the UI.

**The numeric aggregate (gombit #273).** Invoice declared `total`
aggregatable, so the list endpoint answers `?aggregate=<op>:<field>` over the
whole filtered set, in `meta.aggregates`, keyed `"<op>:<field>"`:

```bash
curl -s -b jar.txt 'http://127.0.0.1:8080/api/v1/invoices?aggregate=sum:total,max:total'
# -> { "data":[…], "meta":{ …, "aggregates":{"sum:total":"19.95","max:total":"19.95"} } }
```

The value is a decimal **string**, computed in SQL before pagination — never in
the browser over a partial page.

**Confirm the admin honored the visibility toggles:**

```bash
curl -s -b jar.txt http://127.0.0.1:8080/api/v1/admin/meta
```

`customers` appears in `models`; `invoices` does not, because it isn't
admin-visible — proof `RegisterAll` ran `RegisterAdmin` with the compiler's
declarations.

**✅ Checkpoint** — the aggregate call returns `meta.aggregates` with
`sum:total` = `"19.95"`, and `/admin/meta` lists `customers` but not `invoices`.

---

## 8. Run the generated frontend

The compiler already wrote the React app under
`frontend/src/forge_generated/**`: a `CustomersTablePage` (with the search box,
sortable headers and the tier filter the spec declared), edit forms, detail
pages, and a `HomeDashboardPage` with the count card, the recent-invoices list,
and the two aggregate cards. `resources.tsx` exports the registry the scaffold's
shell is built to consume:

```ts
export const generatedResourceRoutes: RouteObject[];  // one route per page
export const generatedResources: { slug; title; listPath }[];  // for the nav
export const generatedNavigation: { label; to; external }[];   // authored nav
export const generatedBranding: { appName; accentColor; appearance; … };
```

**First, generate the typed client.** The pages import an
[openapi-fetch](https://openapi-ts.dev/openapi-fetch/) client from
`../../api/generated`, produced from your live OpenAPI document. With the server
from chapter 6 still running:

```bash
gombit client generate            # reads /openapi.json, writes frontend/src/api/generated
```

**Then point the shell at the generated registry.** The scaffold's
`frontend/src/app/router.tsx` and `layouts/AppLayout.tsx` already import
`generatedResourceRoutes` and `generatedResources` from `../resources` — so make
that module re-export Forge's registry:

```bash
echo 'export * from "./forge_generated/resources";' > frontend/src/resources.tsx
```

The scaffold ships a demo `Product` page as its landing route. Since this app
has no product resource, remove that wiring so the generated dashboard is the
home page: in `frontend/src/app/router.tsx` drop the `ProductListPage` /
`ProductFormPage` imports and their two `<Route>`s and point the index at the
dashboard —

```tsx
<Route index element={<Navigate to="/home" replace />} />
```

— and in `layouts/AppLayout.tsx` remove the hardcoded "Products" links (the
`generatedResources.map(…)` beside them already renders the real navigation).

**Now run the API and Vite together:**

```bash
gombit dev
```

`gombit dev` serves the Go API and the Vite dev server, proxies `/api` and
`/openapi.json`, and regenerates the client whenever the spec changes. Open the
React app (it prints the URL, typically <http://127.0.0.1:5173>) and log in as
the superuser from chapter 6. You'll land on the dashboard — the customer count,
recent invoices ordered newest-first, and the SUM/MAX-of-total cards — and the
nav links reach the customers table (type in the search box, click a sortable
header, filter by tier), the create/edit forms, and the detail pages.

> **Integration note.** The backend flow (chapters 1–7) is what
> `TestM0EndToEnd` pins. The frontend wiring here follows the scaffold's own
> router/layout contract — they are built to consume `generatedResourceRoutes` /
> `generatedResources` — but Forge's frontend is **not yet exercised by an
> automated end-to-end test**, so treat this chapter as the intended integration
> rather than a byte-pinned one. `gombit dev` is the fast way to iterate on it.

**✅ Checkpoint** — the dashboard renders with a live customer count and the
`Total invoiced` card showing `19.95`, and the customers table lists your
customer with a working search box and tier filter.

---

## 9. Ship it

```bash
gombit build --embed
```

`--embed` runs the frontend build and compiles the SPA into the Go binary with
`go:embed`, giving you **one artifact** that serves the API, the app, and the
admin. Configuration stays environment-driven and typed (`gombit config show`
redacts secrets; `gombit doctor` flags weak settings). In production: set
`GOMBIT_ENV=production`, a strong out-of-band `GOMBIT_JWT_SECRET`, cookie
`Secure=true`, disable `/docs`, and apply migrations with `gombit db migrate` —
never AutoMigrate.

Because the app is an ordinary Gombit project with the spec and the generated
tree committed, you can also hand it to any Git host — Forge's GitHub export
does exactly this from a project revision.

**✅ Checkpoint** — the built binary serves the API, the app, and `/admin/` on
one port with no `node` on the box.

---

## 10. What you proved, and where next

From a declarative `spec.json`, Forge synthesized an ordinary Gombit application
that compiles, migrates on Postgres, boots, serves CRUD with a real
relationship, a decimal, declared search/filter/sort and a server-side
aggregate, catalogs its resources through the admin, and ships a React frontend
of tables, forms, details and a dashboard — all with **no runtime dependency on
Forge**. Delete the compiler and this app keeps running. That is the M0 gate plus
the M3 page builder, and it's the contract every later milestone builds on.

Clean up:

```bash
kill %1                          # the server / gombit dev
docker stop forge-tutorial-db    # --rm removes it
```

Where to go next:

- [`docs/DESIGN.md`](DESIGN.md) — the roadmap (M1–M7) and the locked decisions.
- [`docs/ADR-001.md`](ADR-001.md) — identity, symbol allocation, and file
  ownership, which govern how the generator stays stable across edits.
- [`docs/ADR-004.md`](ADR-004.md) — the ownership split that makes the generated
  code legitimate: Gombit owns framework primitives, Forge owns application
  synthesis.
- [`docs/ADR-005.md`](ADR-005.md) — the Forge / Gombit Cloud boundary: who owns
  build, deploy, and the managed runtime.

Something wrong or unclear here? That's a docs bug —
[open an issue](https://github.com/gombit-dev/gombit-forge/issues/new).
