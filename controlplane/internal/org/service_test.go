package org_test

import (
	"context"
	"testing"

	"github.com/gombit-dev/gombit/auth"
	"gorm.io/gorm"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/audit"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/dbtest"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/org"
)

// seedUser inserts a bare auth user and returns its id. The invitation flow
// keys off the Gombit users table (identity is Gombit's), so tests need real
// user rows.
func seedUser(t *testing.T, db *gorm.DB, email string) uint {
	t.Helper()
	u := auth.User{Email: email, PasswordHash: "x"}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("seed user %s: %v", email, err)
	}
	return u.ID
}

func TestCreateOrganizationMakesCreatorOwner(t *testing.T) {
	db := dbtest.DB(t)
	svc := org.NewService(db)
	ctx := context.Background()

	creator := seedUser(t, db, "founder@example.test")
	o, err := svc.CreateOrganization(ctx, "Acme", "acme", creator)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	role, ok, err := svc.MemberRole(ctx, o.ID, creator)
	if err != nil || !ok {
		t.Fatalf("creator membership: role=%q ok=%v err=%v", role, ok, err)
	}
	if role != org.RoleOwner {
		t.Errorf("creator role = %q, want owner", role)
	}
}

func TestInviteEmitsAuditEventAndAcceptCreatesMember(t *testing.T) {
	db := dbtest.DB(t)
	svc := org.NewService(db)
	ctx := context.Background()

	owner := seedUser(t, db, "owner@example.test")
	o, err := svc.CreateOrganization(ctx, "Acme", "acme", owner)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	inv, token, err := svc.InviteMember(ctx, o.ID, owner, "invitee@example.test", org.RoleMember)
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	if token == "" {
		t.Fatal("invite must return a raw token")
	}

	// The raw token is not stored; only its hash is. So the persisted row's
	// TokenHash must differ from the raw token.
	var stored org.Invitation
	if err := db.First(&stored, inv.ID).Error; err != nil {
		t.Fatalf("load invitation: %v", err)
	}
	if stored.TokenHash == token {
		t.Error("raw token must not be stored; only its hash")
	}

	// A member.invited audit event was recorded, scoped to the org and actor.
	var ev audit.Event
	if err := db.Where("action = ?", audit.ActionMemberInvited).First(&ev).Error; err != nil {
		t.Fatalf("expected a member.invited audit event: %v", err)
	}
	if ev.OrganizationID == nil || *ev.OrganizationID != o.ID {
		t.Errorf("audit org = %v, want %d", ev.OrganizationID, o.ID)
	}
	if ev.ActorUserID == nil || *ev.ActorUserID != owner {
		t.Errorf("audit actor = %v, want %d", ev.ActorUserID, owner)
	}

	// Accepting the invitation creates the invitee's membership with the
	// invited role.
	invitee := seedUser(t, db, "invitee@example.test")
	m, err := svc.AcceptInvitation(ctx, token, invitee)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if m.Role != org.RoleMember || m.OrganizationID != o.ID || m.UserID != invitee {
		t.Errorf("membership = %+v, want org %d user %d role member", m, o.ID, invitee)
	}

	// The token is single-use: a second accept fails closed.
	if _, err := svc.AcceptInvitation(ctx, token, invitee); err != org.ErrInvitationInvalid {
		t.Errorf("second accept err = %v, want ErrInvitationInvalid", err)
	}
}

func TestInviteRequiresPermission(t *testing.T) {
	db := dbtest.DB(t)
	svc := org.NewService(db)
	ctx := context.Background()

	owner := seedUser(t, db, "owner@example.test")
	o, err := svc.CreateOrganization(ctx, "Acme", "acme", owner)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	// A plain member cannot invite (CapMembersInvite is owner/admin only).
	memberUser := seedUser(t, db, "member@example.test")
	inv, token, err := svc.InviteMember(ctx, o.ID, owner, "member@example.test", org.RoleMember)
	if err != nil {
		t.Fatalf("owner invite: %v", err)
	}
	if _, err := svc.AcceptInvitation(ctx, token, memberUser); err != nil {
		t.Fatalf("accept: %v", err)
	}
	_ = inv

	if _, _, err := svc.InviteMember(ctx, o.ID, memberUser, "someone@example.test", org.RoleMember); err != org.ErrForbidden {
		t.Errorf("member invite err = %v, want ErrForbidden", err)
	}

	// A non-member cannot invite either — ErrNotMember, not a leak.
	stranger := seedUser(t, db, "stranger@example.test")
	if _, _, err := svc.InviteMember(ctx, o.ID, stranger, "x@example.test", org.RoleMember); err != org.ErrNotMember {
		t.Errorf("stranger invite err = %v, want ErrNotMember", err)
	}
}

func TestInviteRejectsExistingMemberAndBadRole(t *testing.T) {
	db := dbtest.DB(t)
	svc := org.NewService(db)
	ctx := context.Background()

	owner := seedUser(t, db, "owner@example.test")
	o, err := svc.CreateOrganization(ctx, "Acme", "acme", owner)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}

	// Owner is already a member, so inviting the owner's email is a conflict.
	if _, _, err := svc.InviteMember(ctx, o.ID, owner, "owner@example.test", org.RoleMember); err != org.ErrAlreadyMember {
		t.Errorf("invite existing member err = %v, want ErrAlreadyMember", err)
	}

	// An unknown role is rejected before any write.
	if _, _, err := svc.InviteMember(ctx, o.ID, owner, "new@example.test", org.Role("superuser")); err != org.ErrInvalidRole {
		t.Errorf("invite bad role err = %v, want ErrInvalidRole", err)
	}
}
