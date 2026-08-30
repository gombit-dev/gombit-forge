package org

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/audit"
)

// Sentinel errors the HTTP layer maps to status codes. Returning typed errors
// keeps the service transport-agnostic.
var (
	// ErrForbidden means the caller's role does not permit the action.
	ErrForbidden = errors.New("org: forbidden")
	// ErrNotMember means the caller has no membership in the organization.
	ErrNotMember = errors.New("org: not a member")
	// ErrInvitationInvalid means the token matched no pending, unexpired
	// invitation.
	ErrInvitationInvalid = errors.New("org: invitation invalid or expired")
	// ErrAlreadyMember means the invitee already belongs to the organization.
	ErrAlreadyMember = errors.New("org: already a member")
	// ErrInvalidRole means a caller-supplied role is not a known Role.
	ErrInvalidRole = errors.New("org: invalid role")
	// ErrRoleExceedsGranter means the inviter tried to grant a role above its
	// own standing (e.g. an admin inviting an owner).
	ErrRoleExceedsGranter = errors.New("org: cannot grant a role above your own")
)

// DefaultInvitationTTL is how long an invitation stays acceptable.
const DefaultInvitationTTL = 7 * 24 * time.Hour

// Service holds the org tenancy operations. The clock and token source are
// injectable so tests are deterministic.
type Service struct {
	db            *gorm.DB
	now           func() time.Time
	newToken      func() (string, error)
	invitationTTL time.Duration
}

// NewService builds a Service over db with production defaults: the system
// clock, cryptographically random invitation tokens, and DefaultInvitationTTL.
func NewService(db *gorm.DB) *Service {
	return &Service{
		db:            db,
		now:           time.Now,
		newToken:      randomToken,
		invitationTTL: DefaultInvitationTTL,
	}
}

// CreateOrganization creates an organization and makes creator its first owner,
// atomically. A half-created org with no owner would be unreachable, so both
// rows commit together or not at all.
func (s *Service) CreateOrganization(ctx context.Context, name, slug string, creatorUserID uint) (Organization, error) {
	o := Organization{Name: name, Slug: slug}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&o).Error; err != nil {
			return err
		}
		owner := Member{OrganizationID: o.ID, UserID: creatorUserID, Role: RoleOwner}
		return tx.Create(&owner).Error
	})
	if err != nil {
		return Organization{}, fmt.Errorf("create organization: %w", err)
	}
	return o, nil
}

// MemberRole returns the caller's role in the organization. The bool is false
// (with a nil error) when the user is not a member.
func (s *Service) MemberRole(ctx context.Context, orgID, userID uint) (Role, bool, error) {
	var m Member
	err := s.db.WithContext(ctx).
		Where("organization_id = ? AND user_id = ?", orgID, userID).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("lookup membership: %w", err)
	}
	return m.Role, true, nil
}

// Authorize resolves the user's role in the org and checks it against the
// capability. It returns ErrNotMember when there is no membership and
// ErrForbidden when the role lacks the capability, so callers can fail closed
// without duplicating the lookup.
func (s *Service) Authorize(ctx context.Context, orgID, userID uint, capability Capability) error {
	role, ok, err := s.MemberRole(ctx, orgID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotMember
	}
	if !Can(role, capability) {
		return ErrForbidden
	}
	return nil
}

// InviteMember creates a pending invitation for email to join the org with the
// given role, authorizing the inviter first, and records a member.invited audit
// event in the same transaction as the invitation — so the event exists iff the
// invitation does. It returns the invitation and the raw token, which is the
// only time the token is available in the clear; only its hash is stored.
func (s *Service) InviteMember(ctx context.Context, orgID, inviterUserID uint, email string, role Role) (Invitation, string, error) {
	if !role.Valid() {
		return Invitation{}, "", ErrInvalidRole
	}
	// Resolve the inviter's own role: they must both be permitted to invite and
	// not be granting above their own standing. Checking CapMembersInvite alone
	// would let an admin mint an owner.
	inviterRole, ok, err := s.MemberRole(ctx, orgID, inviterUserID)
	if err != nil {
		return Invitation{}, "", err
	}
	if !ok {
		return Invitation{}, "", ErrNotMember
	}
	if !Can(inviterRole, CapMembersInvite) {
		return Invitation{}, "", ErrForbidden
	}
	if !CanGrant(inviterRole, role) {
		return Invitation{}, "", ErrRoleExceedsGranter
	}

	// A person who is already a member cannot be invited again.
	invited, err := s.userByEmailIsMember(ctx, orgID, email)
	if err != nil {
		return Invitation{}, "", err
	}
	if invited {
		return Invitation{}, "", ErrAlreadyMember
	}

	raw, err := s.newToken()
	if err != nil {
		return Invitation{}, "", fmt.Errorf("mint invitation token: %w", err)
	}
	inv := Invitation{
		OrganizationID:  orgID,
		Email:           email,
		Role:            role,
		TokenHash:       hashToken(raw),
		InvitedByUserID: inviterUserID,
		ExpiresAt:       s.now().Add(s.invitationTTL),
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&inv).Error; err != nil {
			return err
		}
		org := orgID
		actor := inviterUserID
		return audit.Record(ctx, tx, audit.Event{
			OrganizationID: &org,
			ActorUserID:    &actor,
			Action:         audit.ActionMemberInvited,
			TargetType:     "invitation",
			TargetID:       strconv.FormatUint(uint64(inv.ID), 10),
		})
	})
	if err != nil {
		return Invitation{}, "", fmt.Errorf("invite member: %w", err)
	}
	return inv, raw, nil
}

// AcceptInvitation redeems a raw invitation token for the accepting user,
// creating their membership and marking the invitation accepted, atomically.
//
// Invitations are addressed to a specific email (§22): the accepting user's
// email must match the invitation's, so possessing a leaked token is not enough
// to join as someone else. It fails closed — an unknown, expired,
// already-accepted, or wrong-recipient token is all ErrInvitationInvalid, so a
// caller cannot even distinguish "valid token, not for you" from "no such
// token". userEmail is the authenticated caller's email (from the session), not
// caller-supplied.
func (s *Service) AcceptInvitation(ctx context.Context, rawToken, userEmail string, userID uint) (Member, error) {
	var member Member
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var inv Invitation
		lookup := tx.Where("token_hash = ? AND accepted_at IS NULL AND expires_at > ?",
			hashToken(rawToken), s.now()).First(&inv)
		if errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			return ErrInvitationInvalid
		}
		if lookup.Error != nil {
			return lookup.Error
		}
		// Bind acceptance to the invited identity. Email is case-insensitive.
		if !strings.EqualFold(userEmail, inv.Email) {
			return ErrInvitationInvalid
		}

		member = Member{OrganizationID: inv.OrganizationID, UserID: userID, Role: inv.Role}
		if err := tx.Create(&member).Error; err != nil {
			return err
		}
		acceptedAt := s.now()
		return tx.Model(&inv).Update("accepted_at", &acceptedAt).Error
	})
	if err != nil {
		if errors.Is(err, ErrInvitationInvalid) {
			return Member{}, err
		}
		return Member{}, fmt.Errorf("accept invitation: %w", err)
	}
	return member, nil
}

// Members lists an organization's members in a stable order (oldest first).
func (s *Service) Members(ctx context.Context, orgID uint) ([]Member, error) {
	var members []Member
	err := s.db.WithContext(ctx).
		Where("organization_id = ?", orgID).
		Order("id").
		Find(&members).Error
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return members, nil
}

// userByEmailIsMember reports whether the email belongs to a Gombit user who is
// already a member of the org. It joins on the auth users table by email rather
// than importing the auth model, so org has no compile coupling to auth's
// schema beyond the well-known table and columns.
func (s *Service) userByEmailIsMember(ctx context.Context, orgID uint, email string) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Table("organization_members AS m").
		Joins("JOIN users AS u ON u.id = m.user_id").
		Where("m.organization_id = ? AND u.email = ?", orgID, email).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("check existing membership: %w", err)
	}
	return count > 0, nil
}

// randomToken returns a 256-bit URL-safe random token.
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken is the at-rest representation of an invitation token: a hex SHA-256,
// so the database never holds a usable credential.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
