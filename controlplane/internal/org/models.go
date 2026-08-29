// Package org is the control plane's Forge-tenancy feature: organizations, the
// members that belong to them, and the invitation flow that adds new members
// (DESIGN.md §22).
//
// Identity is Gombit's. A member is a Gombit auth.User (the cookie session
// establishes who the caller is); this package never stores credentials or a
// second user table. What it adds is the tenancy that Gombit has no opinion
// about — which user holds which Forge-level role in which organization — plus
// the invitation flow that grants it. Authorization over those roles is a thin
// capability matrix (see roles.go), not a parallel permission engine (D12).
package org

import "time"

// Organization is a Forge tenant. It owns projects (added in #37) and has
// members with Forge-level roles.
type Organization struct {
	ID        uint      `gorm:"primaryKey"`
	Name      string    `gorm:"size:120;not null"`
	Slug      string    `gorm:"uniqueIndex;size:120;not null"`
	CreatedAt time.Time `gorm:"index"`
	UpdatedAt time.Time
}

// Member links a Gombit user to an organization with a Forge-level role. The
// (organization, user) pair is unique: a user holds exactly one role per org.
type Member struct {
	ID             uint `gorm:"primaryKey"`
	OrganizationID uint `gorm:"not null;uniqueIndex:uidx_org_member,priority:1"`
	UserID         uint `gorm:"not null;uniqueIndex:uidx_org_member,priority:2"`
	Role           Role `gorm:"size:20;not null"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// TableName pins the table so renaming the Go type never migrates the data.
func (Member) TableName() string { return "organization_members" }

// Invitation is a pending grant of membership to an email address. The raw
// token is shown to the inviter once (to put in the invite link) and never
// stored; only its hash is persisted, the same discipline Gombit uses for
// refresh tokens. Accepting a still-valid, unaccepted, unexpired invitation
// creates the Member.
type Invitation struct {
	ID              uint       `gorm:"primaryKey"`
	OrganizationID  uint       `gorm:"not null;index"`
	Email           string     `gorm:"size:255;not null;index"`
	Role            Role       `gorm:"size:20;not null"`
	TokenHash       string     `gorm:"size:64;not null;uniqueIndex"`
	InvitedByUserID uint       `gorm:"not null"`
	ExpiresAt       time.Time  `gorm:"not null"`
	AcceptedAt      *time.Time `gorm:"index"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
