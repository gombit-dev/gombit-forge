package platform

import (
	"github.com/gombit-dev/gombit/auth"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/audit"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/deploy"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/org"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/project"
)

// Models is the control plane's complete schema as GORM model values: Gombit's
// own auth tables plus Forge's control-plane tables. It is the single source of
// truth for what tables the control plane owns.
//
// The auth tables come from auth.Models() rather than a hand-copied list —
// that function is Gombit's public API for its schema (ADR-004 D3: consume
// Gombit's public APIs, never duplicate its infrastructure), so when Gombit
// adds a model this set grows with it instead of silently omitting a table.
// The control plane reuses Gombit's identity and RBAC rather than defining its
// own (D12), so those tables are part of the same managed schema.
//
// Deployment applies this schema through Atlas migrations, never AutoMigrate
// (DESIGN.md §14). Tests may AutoMigrate this set to stand up a scratch schema.
//
// audit.Event is here because the invitation flow (#36) records to it; #40
// builds the audit service over it. With #38 the core model set is complete;
// what remains for M1 is services and API, not schema.
func Models() []any {
	return append(auth.Models(),
		&org.Organization{},
		&org.Member{},
		&org.Invitation{},
		&project.Project{},
		&project.Revision{},
		&deploy.Environment{},
		&deploy.Build{},
		&deploy.Deployment{},
		&deploy.Domain{},
		&audit.Event{},
	)
}
