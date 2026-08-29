package platform

import (
	"github.com/gombit-dev/gombit/auth"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/audit"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/org"
)

// Models is the control plane's complete schema as GORM model values, in
// dependency-friendly order (identity first, then tenancy, then audit). It is
// the single source of truth for what tables the control plane owns.
//
// Deployment applies this schema through Atlas migrations, never AutoMigrate
// (DESIGN.md §14). Tests may AutoMigrate this set to stand up a scratch schema.
// Gombit's auth models are included because the control plane reuses Gombit's
// identity and RBAC rather than defining its own (D12), so those tables are
// part of the same managed schema.
//
// The set grows as M1 lands its models: Project and ProjectRevision (#37),
// Environment/Build/Deployment/Domain (#38). audit.Event is here now because
// the invitation flow (#36) records to it; #38 folds it into the same set and
// #40 builds the audit service over it.
func Models() []any {
	return []any{
		&auth.User{},
		&auth.Group{},
		&auth.Permission{},
		&auth.RefreshToken{},
		&org.Organization{},
		&org.Member{},
		&org.Invitation{},
		&audit.Event{},
	}
}
