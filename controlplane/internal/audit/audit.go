// Package audit records important control-plane actions (DESIGN.md §23) and
// reads them back (#40).
//
// It began as a minimal seam with the org/membership work (#36) — the Event
// model and Record, which persists exactly one row. #40 adds the read side
// (List) and settles the vocabulary; the write primitive is unchanged, so the
// invitation flow that already records through Record keeps working.
//
// Scope is Forge's own actions (ADR-005). Forge records what a user does in the
// builder — creating a project, revising its spec, exporting source, inviting a
// member, and triggering a preview or a deploy. It does not record the runtime
// lifecycle of a build or deployment, or secret changes: those happen in Gombit
// Cloud and belong to Cloud's audit trail (gombit-cloud RFC §62). "Deploy
// triggered from Forge" is a Forge action; "deployment started/succeeded/failed"
// is Cloud's, and this package deliberately has no key for it.
//
// One rule from §23 is load-bearing and enforced by construction here: secret
// values must never appear in audit data. Event has no free-form value field —
// only an action key and a typed target reference — so a caller cannot smuggle
// a secret into the log through this package, and there is no secret.changed
// action to invite the attempt.
package audit

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// Action is a stable audit action key. The constants below are the closed §23
// vocabulary, scoped to Forge's own actions (ADR-005); callers must use them
// rather than free strings so the recordable set stays closed and greppable.
type Action string

const (
	ActionProjectCreated Action = "project.created"
	ActionSpecRevised    Action = "spec.revision.created"
	ActionSourceExported Action = "source.exported"
	ActionMemberInvited  Action = "member.invited"
	// Forge triggers previews and deploys; Cloud runs them. These record the
	// trigger — the user's action in the builder — not the runtime lifecycle,
	// which is Cloud's audit trail (ADR-005; gombit-cloud RFC §62).
	ActionPreviewTriggered Action = "preview.triggered"
	ActionDeployTriggered  Action = "deploy.triggered"
)

// Actions is the closed set of Forge audit actions, in vocabulary order. It
// exists so a test can assert the set does not drift into Cloud's runtime
// events (deployment.*, secret.changed), which ADR-005 forbids Forge from
// fabricating.
var Actions = []Action{
	ActionProjectCreated,
	ActionSpecRevised,
	ActionSourceExported,
	ActionMemberInvited,
	ActionPreviewTriggered,
	ActionDeployTriggered,
}

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

// Paging bounds for List.
const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// Filter selects and pages audit events for reading. OrganizationID scopes the
// query to one tenant's events; it is required, because the audit log is viewed
// per organization and an unscoped read would let one tenant see another's
// trail. Action and ActorUserID are optional narrowing filters (their zero
// values match anything).
type Filter struct {
	OrganizationID uint
	Action         Action
	ActorUserID    *uint
	Limit          int // <= 0 uses defaultListLimit; larger than maxListLimit is capped
	Offset         int
}

// List returns one organization's audit events, newest first — ordered by time
// and then id so events sharing a timestamp keep a stable, deterministic order.
// It applies the optional Action and ActorUserID filters and limit/offset
// paging. Platform-level events (no organization) are intentionally not part of
// a tenant's audit view and are not returned.
func List(ctx context.Context, db *gorm.DB, f Filter) ([]Event, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	q := db.WithContext(ctx).
		Where("organization_id = ?", f.OrganizationID).
		Order("created_at DESC, id DESC").
		Limit(limit)
	if f.Offset > 0 {
		q = q.Offset(f.Offset)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.ActorUserID != nil {
		q = q.Where("actor_user_id = ?", *f.ActorUserID)
	}

	var events []Event
	if err := q.Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}
