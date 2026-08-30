// Package deploy holds the control plane's build-and-deploy lifecycle models
// (DESIGN.md §6, §11, §12): a project revision is compiled by a Build, and a
// Deployment places a succeeded build's revision into an Environment reachable
// at a Domain.
//
// This issue (#38) is the schema and the state vocabulary only. The build
// pipeline that drives these states lives in M4, and the deploy path that
// creates Deployments lives in M6; neither is built here. AuditEvent, the
// remaining core model, already exists in internal/audit (#36).
package deploy

import "time"

// BuildState is a build's position in the pipeline (DESIGN.md §11). The zero
// value is not a valid state; a build starts at BuildQueued.
type BuildState string

const (
	BuildQueued     BuildState = "queued"
	BuildGenerating BuildState = "generating"
	BuildTesting    BuildState = "testing"
	BuildBuilding   BuildState = "building"
	BuildPublishing BuildState = "publishing"
	BuildSucceeded  BuildState = "succeeded"
	BuildFailed     BuildState = "failed"
	BuildCancelled  BuildState = "cancelled"
)

// buildTransitions is the state machine: for each state, the states it may move
// to next. The happy path is a linear pipeline (queued → generating → testing →
// building → publishing → succeeded); failed and cancelled are reachable from
// every non-terminal state (a build can break or be cancelled at any stage).
// The three terminal states have no outgoing transitions — this is the model
// half of "builds must be immutable" (§11): once a build reaches a terminal
// state, its result cannot be walked back to a different one.
var buildTransitions = map[BuildState]map[BuildState]bool{
	BuildQueued:     {BuildGenerating: true, BuildFailed: true, BuildCancelled: true},
	BuildGenerating: {BuildTesting: true, BuildFailed: true, BuildCancelled: true},
	BuildTesting:    {BuildBuilding: true, BuildFailed: true, BuildCancelled: true},
	BuildBuilding:   {BuildPublishing: true, BuildFailed: true, BuildCancelled: true},
	BuildPublishing: {BuildSucceeded: true, BuildFailed: true, BuildCancelled: true},
	BuildSucceeded:  {},
	BuildFailed:     {},
	BuildCancelled:  {},
}

// Valid reports whether s is a known build state.
func (s BuildState) Valid() bool {
	_, ok := buildTransitions[s]
	return ok
}

// IsTerminal reports whether s is an end state — succeeded, failed or cancelled.
// A terminal build never transitions again.
func (s BuildState) IsTerminal() bool {
	return s.Valid() && len(buildTransitions[s]) == 0
}

// CanTransitionTo reports whether a build in state s may move to next. It fails
// closed: an unknown current or next state, and any transition not in the
// machine (including staying in place), is refused.
func (s BuildState) CanTransitionTo(next BuildState) bool {
	return buildTransitions[s][next]
}

// Build compiles one exact project revision into a deployable artifact
// (DESIGN.md §11). Its inputs — the revision and the toolchain versions — are
// what make a rebuild deterministic and are never changed after creation; the
// pipeline advances State through buildTransitions.
type Build struct {
	ID        uint `gorm:"primaryKey"`
	ProjectID uint `gorm:"not null;index"`
	// RevisionID is the exact revision built (project_revisions). Immutable input.
	RevisionID uint `gorm:"not null;index"`
	// ForgeVersion and GombitVersion pin the toolchain (§11 build inputs), so a
	// rebuild reproduces the artifact. Immutable inputs.
	ForgeVersion  string     `gorm:"size:64;not null"`
	GombitVersion string     `gorm:"size:64;not null"`
	State         BuildState `gorm:"size:20;not null;index"`
	// FailureReason is set only when State is BuildFailed; never a secret value
	// (§23).
	FailureReason string `gorm:"type:text"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
