package platform

import (
	"github.com/gombit-dev/gombit/auth"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/audit"
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
// The set is deliberately the authoring loop only — Organization/Member/
// Invitation (#36), Project/Revision (#37) and audit.Event (#36, service #40).
// The runtime models (Environment, Build, Deployment, Domain) are owned by
// Gombit Cloud, not the Forge control plane (ADR-005 D2/D6): Forge compiles a
// revision to an ordinary Gombit application and hands it to Cloud, which owns
// build, deployment, environments, databases, secrets and domains. The link to
// the Cloud side is Project.CloudProjectID, not a table here. #101 authors this
// (reduced) schema's initial Atlas migration.
func Models() []any {
	return append(auth.Models(),
		&org.Organization{},
		&org.Member{},
		&org.Invitation{},
		&project.Project{},
		&project.Revision{},
		&audit.Event{},
	)
}
