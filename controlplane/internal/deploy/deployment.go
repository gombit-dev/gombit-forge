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
// same revision. The two must agree; that invariant is the deploy path's to
// uphold (M6), not something this schema-only change enforces.
type Deployment struct {
	ID            uint `gorm:"primaryKey"`
	EnvironmentID uint `gorm:"not null;index"`
	RevisionID    uint `gorm:"not null;index"`
	BuildID       uint `gorm:"not null;index"`
	CreatedBy     uint `gorm:"not null"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
