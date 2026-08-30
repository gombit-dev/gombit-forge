// Package audit records important control-plane actions (DESIGN.md §23).
//
// This is the minimal seam introduced with the org/membership work (#36): the
// Event model and a Record function that persists exactly one row. It is
// deliberately small. The audit *service* — querying, filtering, retention, an
// HTTP surface — is #40, and #38 folds Event into the control plane's migration
// model set alongside the other core models. Both build on this; neither
// replaces it.
//
// One rule from §23 is load-bearing and enforced by construction here: secret
// values must never appear in audit data. Event has no free-form value field —
// only an action key and a typed target reference — so a caller cannot smuggle
// a secret into the log through this package.
package audit

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// Action is a stable audit action key. The constants below are the §23
// vocabulary; callers must use them rather than free strings so the set of
// recordable actions stays closed and greppable.
type Action string

const (
	ActionProjectCreated    Action = "project.created"
	ActionSpecRevised       Action = "spec.revision.created"
	ActionPreviewStarted    Action = "preview.started"
	ActionDeploymentStarted Action = "deployment.started"
	ActionDeploymentOK      Action = "deployment.succeeded"
	ActionDeploymentFailed  Action = "deployment.failed"
	ActionSourceExported    Action = "source.exported"
	ActionSecretChanged     Action = "secret.changed"
	ActionMemberInvited     Action = "member.invited"
)

// Event is one recorded control-plane action. It carries who did what to which
// target, and when — never the target's contents. OrganizationID and
// ActorUserID are pointers because some events are platform-level (no org) or
// system-initiated (no human actor).
type Event struct {
	ID             uint   `gorm:"primaryKey"`
	OrganizationID *uint  `gorm:"index"`
	ActorUserID    *uint  `gorm:"index"`
	Action         Action `gorm:"column:action;index;size:60;not null"`
	// TargetType and TargetID are a typed reference to the affected entity,
	// e.g. ("invitation", "42") or ("member", "user@example.com"). Never a
	// value, so §23's "no secret values in audit data" holds by construction.
	TargetType string    `gorm:"size:60"`
	TargetID   string    `gorm:"size:120"`
	CreatedAt  time.Time `gorm:"index"`
}

// TableName pins the table so a later rename of the Go type cannot silently
// migrate the audit table.
func (Event) TableName() string { return "audit_events" }

// Record persists a single audit event. CreatedAt is left for GORM to set, so
// the recorded time is the write time.
//
// It takes the db explicitly rather than holding one, so a caller can record
// inside an existing transaction (pass tx) and have the event commit or roll
// back atomically with the action it describes.
func Record(ctx context.Context, db *gorm.DB, event Event) error {
	return db.WithContext(ctx).Create(&event).Error
}
