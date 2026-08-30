package deploy

import "time"

// Deployment records that a specific build's revision was placed into an
// environment (DESIGN.md §12). It references an exact revision and the exact
// build that produced the artifact, so a deployment is fully reproducible and
// can be rolled back to by redeploying an earlier one.
//
// RevisionID is stored directly rather than only reached through BuildID: a
// deployment's identity is "this revision, in this environment", and denormal-
// izing it keeps that queryable and stable even though Build already carries the
// same revision.
//
// The copy must not be allowed to drift: a deployment claiming revision 7 while
// its build compiled revision 9 would make every rollback and audit answer
// derived from RevisionID confidently wrong. Denormalizing does not mean leaving
// it unchecked — the migration MUST make disagreement unrepresentable with a
// composite foreign key: a unique index on builds(id, revision_id) (free, id is
// already unique), then deployments(build_id, revision_id) → builds(id,
// revision_id). Recorded here for #101 the way project.Project recorded its
// ON DELETE rules; M6's deploy path then gets the invariant for free instead of
// a duty to uphold.
type Deployment struct {
	ID            uint `gorm:"primaryKey"`
	EnvironmentID uint `gorm:"not null;index"`
	RevisionID    uint `gorm:"not null;index"`
	BuildID       uint `gorm:"not null;index"`
	CreatedBy     uint `gorm:"not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
