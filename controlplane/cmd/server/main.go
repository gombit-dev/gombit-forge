// Command server boots the Forge control plane.
//
// The control plane is itself a Gombit application (DESIGN.md §6, D7): Forge
// dogfoods Gombit rather than building a bespoke backend. It runs with
// cookie/session auth (DESIGN.md §20, D5) and is PostgreSQL-backed (D4).
//
// In cookie mode framework.New mounts the admin plane automatically (see
// gombit's admin.Mount), so /api/v1/admin/* is served and gated by the session
// cookie without any explicit wiring here. The admin catalog is empty until a
// model registers with it — the M1 model issues (User, Organization, Project,
// …) fill it in.
package main

import (
	"context"
	"log"

	"github.com/gombit-dev/gombit/config"
	"github.com/gombit-dev/gombit/framework"

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

	app.OnStop(func(context.Context) error { return db.Close() })

	if err := framework.Run(app); err != nil {
		log.Fatal(err)
	}
}
