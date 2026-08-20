# Gombit Forge — MVP Design Document v1

**Status:** Design-ready
**Product:** Gombit Forge
**Purpose:** No-code/low-code SaaS application builder powered by Gombit
**MVP goal:** Let a non-Go developer visually define a CRUD-oriented web application, preview it, deploy it, and export the resulting ordinary Gombit source code.

---

## 1. Product thesis

Gombit Forge is a visual application builder that compiles a declarative project specification into a real Gombit application.

The core promise is:

> **Build visually. Ship normally. Own the code.**

Unlike conventional no-code platforms, Forge does not require applications to remain inside a proprietary runtime. A Forge project can be exported as a normal Go + React repository using Gombit, Gin, Huma, GORM, Atlas, OpenAPI, TypeScript, and React.

Forge is therefore best understood as:

> **A visual compiler and managed deployment platform for Gombit applications.**

The MVP is intentionally focused on structured business applications: internal tools, CRUD SaaS products, portals, inventory systems, lightweight CRMs, admin systems, and similar data-driven applications.

Forge is not initially a replacement for Webflow, Bubble, Retool, or a general-purpose visual programming environment.

---

# 2. Product principles

## P1 — The project specification is the source of truth

A Forge application is represented by a versioned declarative `ProjectSpec`.

The visual editor edits this specification.

Generation is:

```text
ProjectSpec
    ↓
Forge compiler
    ↓
Gombit application
    ↓
build
    ↓
deployable artifact
```

The generated source tree is not treated as an editable round-trip representation while the project remains managed by Forge.

---

## P2 — No code captivity

A user may export the generated application at any time.

Export produces an ordinary Gombit project containing:

* Go source
* GORM models
* Huma handlers
* React frontend
* TypeScript client
* Atlas migrations
* authentication configuration
* admin registration
* deployment configuration

Once exported and modified outside Forge, Forge does not promise to reverse-engineer arbitrary source changes back into the visual model.

This boundary is intentional.

---

## P3 — Structured before arbitrary

The MVP supports structured application building:

* resources
* fields
* relationships
* CRUD pages
* tables
* forms
* authentication
* permissions
* navigation
* basic dashboards
* branding

The MVP does not provide arbitrary pixel-positioned page design.

Most business applications do not need a freeform canvas to become useful.

---

## P4 — Use Gombit instead of reimplementing Gombit

Forge must invoke or reuse existing Gombit capabilities wherever possible.

Forge must not independently implement:

* migration diffing
* OpenAPI generation
* TypeScript client generation
* authentication
* admin
* GORM database abstractions
* resource scaffolding semantics
* embedded builds

If Forge needs behavior that also belongs in normal Gombit applications, prefer adding the capability to Gombit first.

---

## P5 — Generated applications remain boring

Exported code should look like code a Go developer could reasonably maintain without Forge.

Avoid:

* giant generated interpreters
* proprietary runtime DSLs
* opaque binary blobs
* runtime dependency on Forge
* remote Forge APIs required for normal application operation

A deployed application should continue working if Forge disappeared.

---

# 3. Target user

Primary MVP user:

> A technical founder, backend developer, consultant, agency, operations team, or power user who needs a conventional database-backed application quickly and wants the ability to own the result.

Forge initially targets users comfortable with concepts such as:

* tables/resources
* fields
* relationships
* forms
* permissions
* deployments

It does not require knowledge of Go.

---

# 4. MVP user journey

## 4.1 Create project

User clicks:

**New Project**

Inputs:

* project name
* slug
* database
* authentication mode
* UI preset

MVP defaults:

```text
Database: PostgreSQL
Auth: cookie/session
UI: Gombit minimal
Admin: enabled
```

Forge creates an empty `ProjectSpec`.

---

## 4.2 Define resources

User adds:

```text
Customer
Invoice
Product
Project
Ticket
```

For each resource the user configures fields.

Example:

```text
Customer
├── name        string     required
├── email       string     required unique
├── active      bool
└── created_at  datetime
```

Supported MVP field types:

```text
string
text
integer
decimal
boolean
datetime
date
enum
belongs_to
```

`has_many` is derived from `belongs_to`.

Many-to-many is deferred.

---

## 4.3 Configure resource behavior

For each resource:

* fields displayed in list
* searchable fields
* sortable fields
* filterable fields
* create enabled
* update enabled
* delete enabled
* admin visibility
* human-readable singular/plural labels

---

## 4.4 Create pages

MVP page types:

### Resource table

Displays:

* rows
* pagination
* sorting
* filters
* search
* create action
* row actions

### Resource form

Supports:

* create
* edit
* validation
* relationship selection

### Resource detail

Displays record fields and related records.

### Dashboard

Simple configurable dashboard supporting:

* count cards
* recent-record lists
* basic numeric aggregates

No arbitrary chart designer in MVP.

---

## 4.5 Configure navigation

User creates a navigation structure:

```text
Dashboard
Customers
Invoices
Products
Settings
```

Navigation entries may point to:

* dashboard
* resource list
* custom external URL

---

## 4.6 Configure access

Forge exposes Gombit's users/groups/permissions system visually.

User can define roles such as:

```text
Administrator
Sales
Support
Viewer
```

Permissions map onto Gombit's normal permission model.

Example:

```text
customers.view
customers.create
customers.update
customers.delete
```

Forge must not create a second authorization system.

---

## 4.7 Preview

Clicking **Preview** creates or updates a temporary Forge environment.

The preview runs the actual generated application.

The browser receives a URL such as:

```text
https://preview-abc123.forge.example
```

Preview is not a fake renderer.

It is the application.

---

## 4.8 Publish

User clicks:

**Deploy**

Forge:

```text
validate ProjectSpec
        ↓
generate application
        ↓
generate migrations
        ↓
generate OpenAPI
        ↓
generate TS client
        ↓
build frontend
        ↓
gombit build --embed
        ↓
produce container image
        ↓
run migrations
        ↓
deploy
```

The application receives a stable environment URL.

---

## 4.9 Export

User clicks:

**Export Source**

MVP export targets:

1. ZIP download
2. GitHub repository

GitHub export may be deferred to the second MVP milestone if OAuth work delays the core product.

Exported projects contain no requirement to call Forge.

---

# 5. High-level architecture

```text
┌───────────────────────────────┐
│          Forge Web UI         │
│ React / TypeScript            │
└──────────────┬────────────────┘
               │
               ▼
┌───────────────────────────────┐
│       Forge Control Plane     │
│            Gombit             │
│                               │
│ Projects                      │
│ Specs                         │
│ Users                         │
│ Organizations                 │
│ Builds                        │
│ Deployments                   │
│ Environments                  │
└──────────────┬────────────────┘
               │
               ▼
┌───────────────────────────────┐
│        Forge Compiler         │
│ ProjectSpec → Gombit project  │
└──────────────┬────────────────┘
               │
               ▼
┌───────────────────────────────┐
│         Build Worker          │
│                               │
│ gombit generation             │
│ Atlas migrations              │
│ TypeScript generation         │
│ Vite build                    │
│ Go build                      │
│ container image               │
└──────────────┬────────────────┘
               │
               ▼
┌───────────────────────────────┐
│       Runtime Platform        │
│                               │
│ customer app container        │
│ PostgreSQL                    │
│ HTTPS routing                 │
└───────────────────────────────┘
```

---

# 6. Forge itself

Forge should dogfood Gombit.

The Forge control plane should itself be implemented as a Gombit application.

Core models:

```text
User
Organization
OrganizationMember
Project
ProjectRevision
Environment
Build
Deployment
Domain
AuditEvent
```

Possible later models:

```text
Subscription
Invoice
Secret
Plugin
Integration
```

---

# 7. Project specification

The `ProjectSpec` is the most important contract in the system.

It must be versioned independently from generated source code.

Example:

```yaml
version: 1

project:
  name: Acme CRM
  slug: acme-crm

database:
  driver: postgres

auth:
  mode: cookie

resources:
  customer:
    label: Customer
    fields:
      name:
        type: string
        required: true

      email:
        type: string
        required: true
        unique: true

      active:
        type: boolean
        default: true

  invoice:
    label: Invoice
    fields:
      customer:
        type: belongs_to
        resource: customer
        required: true

      total:
        type: decimal
        required: true

      paid:
        type: boolean
        default: false

pages:
  dashboard:
    type: dashboard

  customers:
    type: resource_table
    resource: customer

  invoices:
    type: resource_table
    resource: invoice

navigation:
  - page: dashboard
    label: Dashboard

  - page: customers
    label: Customers

  - page: invoices
    label: Invoices
```

The canonical storage form should be JSON.

YAML is allowed for export/readability.

---

# 8. Project revisions

Every successful visual edit batch creates a new immutable `ProjectRevision`.

```text
Project
  ├── Revision 1
  ├── Revision 2
  ├── Revision 3
  └── Revision 4 ← current
```

A revision contains:

```text
id
project_id
spec_version
spec_json
created_by
created_at
```

Deployments reference an exact revision.

This enables:

* deterministic rebuilds
* rollback
* audit history
* diff visualization
* safe asynchronous builds

---

# 9. Forge compiler

The compiler converts:

```text
ProjectSpec → generated Gombit repository
```

It should be deterministic:

```text
same compiler version
+ same ProjectSpec
= same generated application
```

Generation stages:

```text
1. validate spec
2. construct domain graph
3. generate models
4. generate route registrations
5. generate handlers
6. generate admin registry
7. generate frontend configuration
8. generate page components
9. generate app navigation
10. generate project configuration
11. run formatter
12. run Gombit contract generation
13. run migration generation
14. run tests/build
```

The compiler may reuse Gombit's generator packages directly rather than spawning CLI commands where practical.

CLI execution remains useful as an end-to-end validation path.

---

# 10. Generated project ownership

Generated source falls into two classes.

## Compiler-owned

Forge may regenerate these files completely:

```text
internal/forge_generated/**
frontend/src/forge_generated/**
```

These directories carry explicit generated-file banners.

---

## Stable application shell

A Forge-generated repository also contains stable framework wiring.

The user owns all code after export.

Forge managed mode does not attempt to preserve arbitrary edits to generated files.

This avoids complex bidirectional reconciliation.

---

# 11. Build system

Each build executes in an isolated worker.

Input:

```text
ProjectRevision
Forge compiler version
Gombit version
```

Output:

```text
generated source archive
build logs
container image
migration set
application binary
build metadata
```

Build states:

```text
queued
generating
testing
building
publishing
succeeded
failed
cancelled
```

Builds must be immutable.

---

# 12. Deployment model

## MVP decision

Each deployed Forge project runs as an isolated application container.

Do not implement a shared multi-tenant application runtime.

This provides:

* strong isolation
* ordinary Gombit semantics
* independent scaling
* straightforward export parity
* simpler debugging

---

## Runtime

Each environment consists of:

```text
Application container
PostgreSQL database/schema
Environment variables
HTTPS endpoint
```

MVP environments:

```text
preview
production
```

Staging is deferred.

---

# 13. Database model

MVP managed applications use PostgreSQL.

Although Gombit supports SQLite, PostgreSQL, and MySQL, Forge managed hosting should initially support **PostgreSQL only**.

Reasons:

* simpler operational surface
* reliable concurrency
* consistent production behavior
* fewer migration/deployment branches
* easier backup implementation

Exported applications may use any database Gombit supports.

---

# 14. Migration policy

Schema changes must use Gombit's Atlas-backed migration system.

Forge never uses GORM AutoMigrate in deployed applications.

Publish flow:

```text
old ProjectSpec
      ↓
new ProjectSpec
      ↓
generated GORM model changes
      ↓
Atlas diff
      ↓
versioned SQL migration
      ↓
review/validation
      ↓
deploy
```

Migrations become build artifacts.

A deployment must fail rather than silently apply an invalid migration.

---

# 15. Deployment safety

MVP publishing flow:

```text
build new artifact
       ↓
migration preflight
       ↓
apply migration
       ↓
start new application
       ↓
health check
       ↓
route traffic
```

If the application fails health checks, deployment is marked failed.

Automatic destructive schema rollback is not guaranteed.

Application rollback and database rollback are separate operations.

---

# 16. Preview behavior

Preview prioritizes speed over persistence guarantees.

For each project:

```text
save spec
   ↓
debounce
   ↓
new preview build
   ↓
replace preview environment
```

Do not compile on every keystroke.

Trigger preview builds on explicit:

**Preview changes**

or after meaningful saved changes.

---

# 17. Visual editor

The editor has four main areas:

```text
Data
Pages
Access
Deploy
```

## Data

Resource tree + field editor.

## Pages

Page list + structured page properties.

## Access

Users, groups and permission configuration.

## Deploy

Preview, build history, production deployment and logs.

---

# 18. Page builder

MVP page building is schema-driven rather than pixel-driven.

A resource table supports:

```text
title
columns
search
filters
sorting
pagination
primary action
row actions
```

A form supports:

```text
field ordering
field visibility
labels
help text
required state
relationship selectors
```

Layout options:

```text
single column
two column
section groups
```

No absolute positioning.

---

# 19. Branding

MVP branding:

```text
application name
logo
primary accent color
light/dark/system appearance
```

Advanced theme editing is deferred.

---

# 20. Authentication

Managed Forge projects default to:

```text
--auth cookie
```

Reasons:

* browser-first product
* Gombit admin compatibility
* HttpOnly session security
* built-in CSRF model

JWT remains available for exported/API-oriented applications later.

---

# 21. Secrets

Secrets never live in `ProjectSpec`.

Use separate encrypted environment configuration.

Example:

```text
DATABASE_URL
GOMBIT_JWT_SECRET
SMTP_PASSWORD
EXTERNAL_API_KEY
```

MVP secret UI supports:

```text
name
value
environment
```

Values are write-only after creation.

---

# 22. Multi-tenancy

There are two unrelated kinds of tenancy.

## Forge tenancy

Forge itself is multi-tenant:

```text
Organization
 └── Projects
```

Organization members receive Forge-level roles.

## Generated application tenancy

Not supported in MVP.

A generated application's business-level multi-tenancy is explicitly deferred.

---

# 23. Audit log

Forge records important control-plane actions:

```text
project created
spec revision created
preview started
deployment started
deployment succeeded
deployment failed
source exported
secret changed
member invited
```

Secret values must never appear in audit data.

---

# 24. Observability

Every application environment must provide:

```text
logs
health state
deployment history
build history
```

MVP does not provide a full Datadog-style observability UI.

Log viewing should support:

```text
timestamp
level
message
request_id
```

---

# 25. Infrastructure

## MVP preferred deployment stack

Initial cloud implementation:

```text
AWS
```

Suggested services:

```text
Control plane       ECS/Fargate
Generated apps      ECS/Fargate
Container registry  ECR
PostgreSQL          RDS
Artifacts           S3
Build workers        ECS tasks
DNS                  Route 53
TLS                  ACM
Ingress              ALB
Secrets              Secrets Manager
Queue                 SQS
```

The deployment interface should still live behind internal abstractions so another runtime may be added later.

Do not build Kubernetes infrastructure for the MVP unless operational requirements make ECS insufficient.

---

# 26. Build execution

Build workers consume jobs from a queue.

```text
Forge API
   ↓
SQS
   ↓
Build Worker
   ↓
Compiler
   ↓
Gombit
   ↓
ECR/S3
```

Control-plane HTTP requests must never synchronously perform builds.

---

# 27. Security boundaries

Each build runs in an isolated disposable environment.

Build workers must assume generated source is untrusted.

Restrictions:

* no access to Forge production credentials
* scoped artifact credentials
* network access restricted where practical
* CPU/memory/time limits
* ephemeral filesystem
* container destroyed after build

---

# 28. Export

Export is a first-class product feature.

ZIP structure:

```text
project/
├── cmd/
├── internal/
├── frontend/
├── database/
├── config/
├── docs/
├── gombit.yaml
├── go.mod
└── README.md
```

Exported README should include:

```bash
gombit dev
gombit db migrate
gombit build --embed
```

The generated repository should build outside Forge.

CI must test that condition.

---

# 29. Forge version metadata

Each exported project records:

```yaml
forge:
  spec_version: 1
  compiler_version: 0.1.0
  gombit_version: 0.x.y
```

This is informational after export.

The application does not depend on Forge at runtime.

---

# 30. MVP non-goals

Explicitly excluded:

* freeform drag-and-drop canvas
* arbitrary JavaScript workflow editor
* custom code blocks
* plugin marketplace
* background jobs
* email builder
* storage/file-management builder
* mobile applications
* native applications
* AI application generation
* real-time collaborative editing
* custom domains
* automatic multi-region deployment
* Kubernetes
* application-level multi-tenancy
* Git import / reverse engineering
* source-code ↔ visual-editor round trip
* arbitrary third-party API connectors
* visual SQL editor
* complex chart builder
* billing automation for generated apps

These may become later products.

---

# 31. MVP success criterion

A new user must be able to build this application without writing code:

```text
Customer Portal

Resources:
  Customer
  Project
  Ticket

Authentication:
  users log in

Permissions:
  Admin
  Support

Pages:
  Dashboard
  Customers
  Projects
  Tickets

Admin:
  enabled

Database:
  PostgreSQL
```

They must then be able to:

```text
Preview
Deploy
Create a user
Use the application
Use /admin/
Export the source
Run the exported source locally
```

If this loop works reliably, the MVP is successful.

---

# 32. Milestones

## M0 — Forge compiler spike

Goal: prove the central architectural assumption.

Implement:

```text
ProjectSpec
   ↓
2 resources
   ↓
generated Gombit project
   ↓
build
   ↓
working application
```

Acceptance criteria:

* deterministic generation
* exported app compiles
* migration works
* React CRUD works
* admin works
* no Forge runtime dependency

**Go/no-go gate.**

---

## M1 — Forge control plane

Implement:

* organizations
* projects
* project revisions
* environments
* builds
* deployments
* audit events

Forge itself runs on Gombit.

---

## M2 — Data editor

Implement visual:

* resources
* fields
* validation
* relationships
* CRUD behavior
* admin settings

Produces valid `ProjectSpec`.

---

## M3 — Structured page builder

Implement:

* resource table
* resource form
* detail page
* dashboard
* navigation
* branding

---

## M4 — Build pipeline

Implement:

* build queue
* isolated worker
* compiler invocation
* migration generation
* tests
* embedded binary
* container build
* artifact storage
* logs

---

## M5 — Preview

Implement:

* preview environment provisioning
* preview URL
* rebuild
* build state
* application logs

---

## M6 — Production deploy

Implement:

* production database
* secrets
* migration apply
* container rollout
* health checks
* stable URL
* rollback to previous application image

---

## M7 — Export

Implement:

* source ZIP
* generated README
* reproducibility metadata
* CI test proving exported project works outside Forge

GitHub export is optional for MVP release.

---

# 33. Locked MVP decisions

The following should be considered locked unless implementation proves them invalid.

### D1 — Forge is spec-first

`ProjectSpec` is the source of truth.

### D2 — Forge is a compiler, not a proprietary application runtime

Generated applications are normal Gombit applications.

### D3 — Managed applications are isolated per project

No shared generated-app runtime.

### D4 — Managed hosting supports PostgreSQL only in MVP

Other Gombit databases remain export options.

### D5 — Managed applications default to cookie/session auth

Required for the browser-first product and Gombit admin.

### D6 — Structured pages, not freeform canvas

Tables/forms/details/dashboard only.

### D7 — Forge itself uses Gombit

Dogfood the framework.

### D8 — Builds are asynchronous

No HTTP request directly builds an application.

### D9 — Build execution is isolated

Generated code is treated as untrusted.

### D10 — Export is mandatory MVP functionality

No-code without code captivity is part of the product, not a later marketing feature.

### D11 — No round-trip after eject

Export is a one-way ownership boundary.

### D12 — Use Gombit's normal contracts

Do not create Forge-specific auth, migration, API, admin or ORM systems where Gombit already provides them.

---

# 34. Post-MVP direction

Once the core compiler/deploy/export loop is proven, the most valuable expansions are likely:

```text
background jobs
email
file storage
webhooks
external API connectors
workflow automation
custom domains
GitHub synchronization
application templates
AI-assisted ProjectSpec generation
custom components
custom code escape hatches
```

AI should operate primarily on `ProjectSpec`, not directly mutate generated source.

Example:

> “Create a lightweight CRM with customers, contacts, deals and salespeople.”

AI produces a proposed spec.

The user reviews it visually.

Forge compiles it normally.

That keeps the same architecture intact.

---

# 35. Final architecture rule

The entire product should remain understandable through one sentence:

> **Forge edits a declarative application model; Gombit turns that model into ordinary software.**

If a future feature violates that model by requiring applications to depend permanently on Forge internals, it needs strong justification before being accepted.
