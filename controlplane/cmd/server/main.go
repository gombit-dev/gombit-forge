// Command server boots the Forge control plane.
//
// The control plane is itself a Gombit application (DESIGN.md §6, D7): Forge
// dogfoods Gombit rather than building a bespoke backend. It runs with
// cookie/session auth (DESIGN.md §20, D5) and is PostgreSQL-backed (D4).
//
// In cookie mode framework.New mounts the admin surface automatically, with no
// explicit wiring here: admin.Mount serves the gated data plane at
// /api/v1/admin/*, and the framework-owned admin SPA (gombit's internal/adminui)
// is served at /admin/. The admin catalog is empty until a model registers with
// it — the M1 model issues (User, Organization, Project, …) fill it in.
package main

import (
	"context"
	"log"

	"github.com/gombit-dev/gombit/config"
	"github.com/gombit-dev/gombit/framework"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/org"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/platform"
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
	)
	if err != nil {
		_ = db.Close()
		log.Fatal(err)
	}

	// Feature packages register explicitly (Gombit does not discover them by
	// reflection). Tenancy is the first; projects, environments and the rest
	// follow with their issues.
	if err := org.Register(app); err != nil {
		_ = db.Close()
		log.Fatal(err)
	}

	app.OnStop(func(context.Context) error { return db.Close() })

	if err := framework.Run(app); err != nil {
		log.Fatal(err)
	}
}
