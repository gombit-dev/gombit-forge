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

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

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
// The three terminal states have no outgoing transitions, so a pipeline that
// consults CanTransitionTo cannot walk a finished build back to a different
// result (§11).
//
// The stored Build.State is bound to this table only at the ends: BeforeCreate
// (and BeforeUpdate, for a state the instance carries) rejects a state the
// machine does not know — no "banana" build from a struct write. The transition
// guard proper — refusing a legal-state-but-illegal-move write — is M4's
// worker, and should be a compare-and-swap (UPDATE … WHERE id = ? AND state = ?
// with the expected prior state) so two racing workers cannot lose an update; a
// CHECK constraint reinforces the valid-state half at the database (#101).
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
// what make a rebuild deterministic; only State and FailureReason advance after
// creation, which BeforeUpdate enforces. The pipeline advances State through
// buildTransitions.
type Build struct {
	ID        uint `gorm:"primaryKey"`
	ProjectID uint `gorm:"not null;index"`
	// RevisionID is the exact revision built (project_revisions). Input, frozen
	// after creation by BeforeUpdate.
	RevisionID uint `gorm:"not null;index"`
	// ForgeVersion and GombitVersion pin the toolchain (§11 build inputs), so a
	// rebuild reproduces the artifact. Inputs, frozen after creation.
	ForgeVersion  string     `gorm:"size:64;not null"`
	GombitVersion string     `gorm:"size:64;not null"`
	State         BuildState `gorm:"size:20;not null;index"`
	// FailureReason carries a build's failure explanation. It is populated from
	// build output, so §23's "no secret values" does NOT hold here by
	// construction the way it does for audit.Event — build logs are the most
	// likely place a DSN or key leaks. Redacting against the known secret set is
	// M4's job (and #41's secret store is what makes it possible); this field is
	// unredacted until then. The state/reason pairing is likewise a convention,
	// not a constraint, until #101 adds a CHECK.
	FailureReason string `gorm:"type:text"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// BeforeCreate rejects a build state the machine does not know. A build is
// always born with a state, so the whole-instance check belongs here rather
// than on BeforeSave — the latter also fires on updates, where a column-level
// Update carries a zero-valued model whose State is legitimately "". This is the
// cheap, in-process half of binding State to the machine; #101's CHECK
// constraint is the durable guarantee, and the transition guard proper is M4's
// (see buildTransitions).
func (b *Build) BeforeCreate(*gorm.DB) error {
	if !b.State.Valid() {
		return fmt.Errorf("deploy: invalid build state %q", b.State)
	}
	return nil
}

// BeforeUpdate freezes the build's inputs — a rebuild is only reproducible if
// the revision and toolchain versions cannot be repointed by a later
// Updates/Save — and validates a state the instance actually carries. A
// column-level Update("state", …) (the compare-and-swap buildTransitions
// prescribes for M4) has no instance state and passes through here; #101's
// CHECK owns that path. Like project.Revision's hook this guards GORM's model
// API, not a raw Exec or SkipHooks.
func (b *Build) BeforeUpdate(tx *gorm.DB) error {
	for _, col := range []string{"project_id", "revision_id", "forge_version", "gombit_version"} {
		if tx.Statement.Changed(col) {
			return fmt.Errorf("deploy: build %s is immutable after creation", col)
		}
	}
	if b.State != "" && !b.State.Valid() {
		return fmt.Errorf("deploy: invalid build state %q", b.State)
	}
	return nil
}
