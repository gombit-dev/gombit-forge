package project_test

// End-to-end HTTP test over a humatest API with the real cookie gate and a real
// DB: exercises the wiring the service tests bypass — session → handler →
// org authorization → RawBody spec decode → outcome-to-status mapping.
// Docker-gated, like the service tests.

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
	"github.com/gombit-dev/gombit-forge/controlplane/internal/project"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

type httpFixture struct {
	api     humatest.TestAPI
	authSvc *auth.Service
	orgSvc  *org.Service
	db      *gorm.DB
}

func newHTTPFixture(t *testing.T) *httpFixture {
	t.Helper()
	db := dbtest.DB(t)
	cfg := config.Config{Auth: config.AuthConfig{
		JWTSecret:       "project-routes-test-secret-please-change-01",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Mode:            config.AuthModeCookie,
	}}
	authSvc, err := auth.NewService(db, cfg)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}
	orgSvc := org.NewService(db)
	_, api := humatest.New(t)
	project.RegisterRoutes(api, "/api/v1",
		huma.Middlewares{authSvc.RequireCookieSession()}, project.NewService(db), orgSvc)
	return &httpFixture{api: api, authSvc: authSvc, orgSvc: orgSvc, db: db}
}

func (f *httpFixture) user(t *testing.T, email string) uint {
	t.Helper()
	u := auth.User{Email: email, PasswordHash: "x"}
	if err := f.db.Create(&u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u.ID
}

func (f *httpFixture) cookie(t *testing.T, userID uint) string {
	t.Helper()
	var u auth.User
	if err := f.db.First(&u, userID).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	pair, err := f.authSvc.IssueTokens(context.Background(), u)
	if err != nil {
		t.Fatalf("issue tokens: %v", err)
	}
	return "Cookie: " + auth.AccessCookieName + "=" + pair.AccessToken
}

// specBody re-encodes a spec as a generic JSON object so humatest sends it as the
// request body, which the handler reads back as its RawBody.
func specBody(t *testing.T, s *spec.ProjectSpec) map[string]any {
	t.Helper()
	data, err := spec.Marshal(s)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("to map: %v", err)
	}
	return body
}

// TestProjectAPIEndToEnd walks the whole authoring loop over HTTP: create a
// project, commit a first revision, and see a breaking candidate rejected with
// 409 while the head stays put.
func TestProjectAPIEndToEnd(t *testing.T) {
	f := newHTTPFixture(t)
	ctx := context.Background()

	ownerID := f.user(t, "owner@example.test")
	o, err := f.orgSvc.CreateOrganization(ctx, "Acme", "acme", ownerID)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	cookie := f.cookie(t, ownerID)
	orgPath := "/api/v1/organizations/" + strconv.FormatUint(uint64(o.ID), 10) + "/projects"

	// Create a project.
	resp := f.api.Post(orgPath, cookie, map[string]any{"name": "Acme CRM", "slug": "acme-crm"})
	if resp.Code != http.StatusCreated {
		t.Fatalf("create project = %d, want 201\n%s", resp.Code, resp.Body.String())
	}
	var created struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	revPath := "/api/v1/projects/" + strconv.FormatUint(uint64(created.Data.ID), 10) + "/revisions"

	// First candidate commits.
	base := validSpec(t, "V1", "v1")
	resp = f.api.Post(revPath, cookie, specBody(t, base))
	if resp.Code != http.StatusCreated {
		t.Fatalf("first revision = %d, want 201\n%s", resp.Code, resp.Body.String())
	}

	// A breaking candidate is rejected 409, not committed.
	breaking := cloneSpec(t, base)
	breaking.Resources[0].CodeName = "Client"
	resp = f.api.Post(revPath, cookie, specBody(t, breaking))
	if resp.Code != http.StatusConflict {
		t.Fatalf("breaking candidate = %d, want 409\n%s", resp.Code, resp.Body.String())
	}

	// An invalid candidate is a 422, separate from the ABI conflict above.
	invalid := cloneSpec(t, base)
	invalid.Resources[0].StorageName = "1_not_valid"
	resp = f.api.Post(revPath, cookie, specBody(t, invalid))
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid candidate = %d, want 422\n%s", resp.Code, resp.Body.String())
	}
}

// TestProjectAPIHidesOtherOrgProjects: a user who is not a member of a project's
// org gets 404, so project ids cannot be probed across the tenancy boundary.
func TestProjectAPIHidesOtherOrgProjects(t *testing.T) {
	f := newHTTPFixture(t)
	ctx := context.Background()

	ownerID := f.user(t, "owner2@example.test")
	o, err := f.orgSvc.CreateOrganization(ctx, "Acme", "acme", ownerID)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	p, err := project.NewService(f.db).CreateProject(ctx, o.ID, "Secret", "secret", ownerID)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	outsiderID := f.user(t, "outsider@example.test")
	getPath := "/api/v1/projects/" + strconv.FormatUint(uint64(p.ID), 10)
	resp := f.api.Get(getPath, f.cookie(t, outsiderID))
	if resp.Code != http.StatusNotFound {
		t.Errorf("outsider get = %d, want 404 (not 403, to avoid leaking existence)\n%s", resp.Code, resp.Body.String())
	}
}
