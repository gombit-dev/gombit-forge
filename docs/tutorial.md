# Tutorial: building an app with Forge

Forge is a **visual application builder**. You describe an app — its resources,
fields, relationships, and screens — and Forge compiles that description into an
ordinary [Gombit](https://github.com/gombit-dev/gombit) application: a Go API,
a database schema and migrations, an admin, and a React frontend. The generated
project is normal code you own and can export; it has **no runtime dependency on
Forge**.

You never write Go, and you never hand-write the model. Every change is a small
edit — "add a resource", "add a field", "add a page" — and Forge mints the stable
IDs, validates the result, and records it as an immutable revision. This page
walks that authoring loop.

> **Where this stands.** The control plane (`controlplane/`) is built and
> runnable: it's a Gombit app whose Postgres schema ships as committed Atlas
> migrations, and a React editor SPA (`controlplane/web`) drives the authoring
> API below. The compiler it feeds is proven end to end by the go/no-go test —
> the executable version of this whole loop:
>
> ```bash
> go test ./internal/compiler -run TestM0EndToEnd -v
> ```
>
> Running the app is Gombit Cloud's job, not Forge's. When the control plane is
> configured with `GOMBIT_CLOUD_API_URL` and `GOMBIT_CLOUD_API_TOKEN`, the
> editor's Deploy tab hands a revision to Cloud to build, preview, and deploy
> ([§7](#7-build-and-deploy)). Without Cloud, the loop ends at a compiled,
> exportable app. This page shows the loop through the HTTP API; the editor
> issues these same calls for you.

We'll build a tiny CRM — customers and invoices.

## 1. A project

The control plane uses Gombit's cookie session auth, which protects every
state-changing request with a double-submit CSRF token. Fetch a token first,
send it as `X-CSRF-Token` on every `POST`, `PATCH`, `PUT` and `DELETE`, and
fetch a fresh one after signing in:

```http
GET /api/v1/auth/csrf
POST /api/v1/auth/login
{ "email": "you@example.com", "password": "…" }
```

(Create the first account with `gombit createsuperuser` in `controlplane/`.)

Everything lives in a project, inside an organization you belong to. Create the
organization, then a project in it:

```http
POST /api/v1/organizations
{ "name": "Acme", "slug": "acme" }
```

```http
POST /api/v1/organizations/{orgID}/projects
{ "name": "Acme CRM", "slug": "acme-crm" }
```

A project starts empty. From here, every edit returns a **revision** — the new
head of an append-only history:

```json
{ "id": 7, "project_id": 3, "spec_hash": "9f2c…", "abi_class": "additive" }
```

`abi_class` is how Forge tells a safe change from a dangerous one: adding things
is `additive`, a pure relabel is `neutral`, and a change that would break code
generated against the old shape is refused (returned as a 409 with its reasons)
rather than silently applied.

## 2. Resources

Add a resource from a **label**. Forge mints its stable ID and its frozen Go
symbol — you never choose either:

```http
POST /api/v1/projects/{id}/resources
{ "label": "Customer", "label_plural": "Customers" }
```

Do the same for `Invoice`. The edit returns the new revision, not the minted
ID: read IDs back from the spec (`GET /api/v1/projects/{id}/spec`, see
[§6](#6-what-you-get)), where each resource and field carries its `id`. The
`{customerID}`-style placeholders below are those IDs. Renaming later (`PATCH …/resources/{id}`) changes only
the label; the ID and the generated code symbol never move. **Identity is the ID,
never the name** — that is what lets you relabel freely without breaking anything.

## 3. Fields

Add fields the same way — a label and a type, from the MVP set (`string`,
`text`, `integer`, `decimal`, `boolean`, `date`, `datetime`, `enum`):

```http
POST /api/v1/projects/{id}/resources/{customerID}/fields
{ "label": "Email", "type": "string", "required": true, "unique": true }
```

```http
POST /api/v1/projects/{id}/resources/{invoiceID}/fields
{ "label": "Total", "type": "decimal", "required": true }
```

Give the invoice a `decimal` total, an `enum` status, a `datetime` — whatever the
app needs. Each add returns a new revision.

## 4. Relationships

Point one resource at another. Forge derives the reverse side (`Customer` gets a
`has_many` of invoices) for you:

```http
POST /api/v1/projects/{id}/resources/{invoiceID}/relationships
{ "label": "Customer", "target": "{customerID}", "required": true }
```

## 5. Behavior and screens

Choose what each resource allows and exposes — CRUD toggles, admin visibility,
and which fields are searchable, filterable, sortable, or aggregatable
(`searchable_fields`, `filterable_fields`, `sortable_fields`,
`aggregatable_fields`, each a list of field IDs):

```http
PATCH /api/v1/projects/{id}/resources/{customerID}/behavior
{ "create_enabled": true, "update_enabled": true, "delete_enabled": true,
  "admin_visible": true,
  "searchable_fields": ["{emailID}"] }
```

Then add screens — structured pages, not a freeform canvas: a **table**, a
**form**, a **detail** page, and a **dashboard**:

```http
POST /api/v1/projects/{id}/pages
{ "type": "resource_table", "label": "Customers", "resource": "{customerID}" }
```

```http
POST /api/v1/projects/{id}/pages
{ "type": "dashboard", "label": "Home" }
```

A dashboard carries count cards, recent-record lists, and numeric aggregate cards
(a live SUM/AVG/MIN/MAX over a field). A table gets a search box, sortable
headers, and filters — drawn straight from the behavior you declared above.

## 6. What you get

You never edit the model as a file — but you can read it. `GET
/api/v1/projects/{id}/spec` returns the current head as canonical JSON: the
single source of truth Forge stores and hashes for each revision. It is an
output to inspect or export, not something you author by hand.

From that model, the compiler produces an ordinary Gombit application:

- **GORM models** and **versioned Atlas migrations** for your schema;
- **Huma-typed handlers, routes, and OpenAPI** for the API, with server-side
  search / filter / sort and numeric aggregates;
- a **Django-style admin** for the resources you marked admin-visible;
- a **React + TypeScript frontend** — the tables, forms, detail pages, and
  dashboard you added;
- and a **GitHub export** of the whole repository.

Crucially, the output imports Gombit, not Forge. Delete Forge and the app keeps
running (decision **D2**). Forge builds nothing Gombit already owns — routing,
ORM, migrations, auth, admin, the OpenAPI and TypeScript client are all Gombit's;
Forge only synthesizes the resource-specific code that consumes them
([ADR-004](ADR-004.md)).

## 7. Build and deploy

Forge doesn't run your app. Gombit Cloud does ([ADR-005](ADR-005.md)), and
Forge is its client. A project linked to its Cloud counterpart deploys in two
steps. First, build the current revision:

```http
POST /api/v1/projects/{id}/deploy
```

This returns `202` with a build job. No request performs a build: the job
freezes the head revision, and a worker compiles the source and submits it to
Cloud. Poll `GET /api/v1/build-jobs/{jobID}` until `status` is `succeeded`, or
read its logs at `…/build-jobs/{jobID}/logs`. Then deploy the Cloud build to an
environment:

```http
POST /api/v1/projects/{id}/environments/{envID}/deployments
{ "build_id": "{cloud_build_id}" }
```

A deployment that would run a destructive migration comes back
`blocked_pending_approval`, with a link where a human approves it in Cloud.
`POST /api/v1/projects/{id}/preview-environments` creates a short-lived preview
environment, and `…/environments/{envID}/rollback` restores the previous
deployment. The editor's Deploy tab drives all of this: build history, live
logs, preview, deploy, approval, and rollback.

## The whole idea

- You describe the app; the **compiler** turns that into ordinary software. You
  never operate the compiler yourself.
- **Identity is the stable ID**, minted for you — a relabel is never a code
  change.
- Every edit is **validated and versioned**; a build-breaking change is flagged,
  not silently shipped.
- The generated app is **yours** — normal code, no Forge runtime, exportable.

## Where next

- [`README`](../README.md) — what Forge is, and its current status at a glance.
- [`docs/DESIGN.md`](DESIGN.md) — product scope, milestones, and locked decisions.
- [`docs/ADR-001.md`](ADR-001.md) — identity and symbol allocation: why the ID,
  not the name, is the thing.
- [`docs/ADR-004.md`](ADR-004.md) — the ownership split: Gombit owns framework
  primitives, Forge owns application synthesis.
- [`docs/ADR-005.md`](ADR-005.md) — the Forge / Gombit Cloud boundary: who owns
  build, deploy, and the managed runtime.

Something wrong or unclear here? That's a docs bug —
[open an issue](https://github.com/gombit-dev/gombit-forge/issues/new).
