package org_test

// HTTP-layer tests: exercise the real org operations over a humatest API with
// the real cookie-session gate, so the wiring the service tests bypass — the
// gate attachment and auth.UserFromContext(ctx).Email — is actually covered.
//
// Docker-gated (needs a users table to mint and validate sessions), like the
// service tests. It still pins the single regression most worth pinning on an
// RBAC feature: that every operation is behind the gate. Delete Middlewares
// from any route in RegisterRoutes and TestRoutesEnforceGate fails.

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/gombit-dev/gombit/auth"
	"github.com/gombit-dev/gombit/config"
	"gorm.io/gorm"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/dbtest"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/org"
)

// httpFixture is a humatest API wired with the real gate over a real DB, plus
// helpers to mint session cookies for seeded users.
type httpFixture struct {
	api     humatest.TestAPI
	authSvc *auth.Service
	svc     *org.Service
	db      *gorm.DB
}

func newHTTPFixture(t *testing.T) *httpFixture {
	t.Helper()
	db := dbtest.DB(t)
	cfg := config.Config{Auth: config.AuthConfig{
		JWTSecret:       "org-routes-test-secret-please-change-0001",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Mode:            config.AuthModeCookie,
	}}
	authSvc, err := auth.NewService(db, cfg)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}
	svc := org.NewService(db)
	_, api := humatest.New(t)
	org.RegisterRoutes(api, "/api/v1", huma.Middlewares{authSvc.RequireCookieSession()}, svc)
	return &httpFixture{api: api, authSvc: authSvc, svc: svc, db: db}
}

// sessionCookie mints a valid cookie-mode access token for the user, formatted
// as the "Cookie:" header humatest sends verbatim.
func (f *httpFixture) sessionCookie(t *testing.T, userID uint) string {
	t.Helper()
	var u auth.User
	if err := f.db.First(&u, userID).Error; err != nil {
		t.Fatalf("load user %d: %v", userID, err)
	}
	pair, err := f.authSvc.IssueTokens(context.Background(), u)
	if err != nil {
		t.Fatalf("issue tokens: %v", err)
	}
	return "Cookie: " + auth.AccessCookieName + "=" + pair.AccessToken
}

// TestRoutesEnforceGate: every operation rejects an unauthenticated request
// with 401 — this pins "the gate is attached". It fails if Middlewares is
// dropped from any route. orgID 1 need not exist: the gate runs before the
// handler, so an unauthenticated call never reaches it.
func TestRoutesEnforceGate(t *testing.T) {
	f := newHTTPFixture(t)

	ops := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/organizations"},
		{http.MethodGet, "/api/v1/organizations/1/members"},
		{http.MethodPost, "/api/v1/organizations/1/invitations"},
		{http.MethodPost, "/api/v1/invitations/accept"},
	}
	for _, op := range ops {
		resp := f.api.Do(op.method, op.path)
		if resp.Code != http.StatusUnauthorized {
			t.Errorf("%s %s unauthenticated = %d, want 401 (gate not attached?)", op.method, op.path, resp.Code)
		}
	}
}

// TestInviteRouteAuthorizes: with a real session, an owner may invite and a
// plain member may not — exercising the handler's auth.UserFromContext wiring
// end to end, not the service in isolation.
func TestInviteRouteAuthorizes(t *testing.T) {
	f := newHTTPFixture(t)
	ctx := context.Background()

	ownerID := seedUser(t, f.db, "owner@example.test")
	o, err := f.svc.CreateOrganization(ctx, "Acme", "acme", ownerID)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	invitePath := "/api/v1/organizations/" + strconv.FormatUint(uint64(o.ID), 10) + "/invitations"

	// Owner can invite; the response carries the one-time token, proving the
	// handler ran and the session email flowed through UserFromContext into the
	// authorized path.
	resp := f.api.Post(invitePath, f.sessionCookie(t, ownerID),
		map[string]any{"email": "invitee@example.test", "role": "member"})
	if resp.Code != http.StatusOK {
		t.Fatalf("owner invite = %d, want 200\n%s", resp.Code, resp.Body.String())
	}
	var body struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}
	if body.Data.Token == "" {
		t.Error("invite response missing token")
	}

	// A plain member cannot invite. Make one directly, then try over HTTP.
	memberID := seedUser(t, f.db, "member@example.test")
	if err := f.db.Create(&org.Member{OrganizationID: o.ID, UserID: memberID, Role: org.RoleMember}).Error; err != nil {
		t.Fatalf("make member: %v", err)
	}
	resp = f.api.Post(invitePath, f.sessionCookie(t, memberID),
		map[string]any{"email": "another@example.test", "role": "member"})
	if resp.Code != http.StatusForbidden {
		t.Errorf("member invite = %d, want 403\n%s", resp.Code, resp.Body.String())
	}
}
