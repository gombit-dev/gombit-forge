package cloudbuild_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/gombit-dev/gombit/auth"
	"github.com/gombit-dev/gombit/config"
	"gorm.io/gorm"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/cloudbuild"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/dbtest"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/org"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/project"
)

// fakeProjects resolves a fixed project + head revision — the routes' project
// dependency, so the handler logic (auth, head resolution, Cloud linkage) is
// tested without the full project service.
type fakeProjects struct {
	proj    project.Project
	head    project.Revision
	hasHead bool
}

func (f fakeProjects) GetProject(context.Context, uint) (project.Project, error) { return f.proj, nil }
func (f fakeProjects) Head(context.Context, uint) (project.Revision, bool, error) {
	return f.head, f.hasHead, nil
}

// fakeAuthz authorizes (or not) with a fixed verdict.
type fakeAuthz struct{ err error }

func (f fakeAuthz) Authorize(context.Context, uint, uint, org.Capability) error { return f.err }

type routesFixture struct {
	api     humatest.TestAPI
	authSvc *auth.Service
	jobs    *cloudbuild.Service
	db      *gorm.DB
}

func newRoutesFixture(t *testing.T, projects cloudbuild.Projects, authz cloudbuild.Authorizer) *routesFixture {
	t.Helper()
	db := dbtest.DB(t)
	cfg := config.Config{Auth: config.AuthConfig{
		JWTSecret:       "cloudbuild-routes-test-secret-please-change-01",
		AccessTokenTTL:  time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
		Mode:            config.AuthModeCookie,
	}}
	authSvc, err := auth.NewService(db, cfg)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}
	jobs := cloudbuild.NewService(db)
	_, api := humatest.New(t)
	cloudbuild.RegisterRoutes(api, "/api/v1", huma.Middlewares{authSvc.RequireCookieSession()}, jobs, projects, authz)
	return &routesFixture{api: api, authSvc: authSvc, jobs: jobs, db: db}
}

func (f *routesFixture) seedUser(t *testing.T, email string) uint {
	t.Helper()
	u := auth.User{Email: email, PasswordHash: "x"}
	if err := f.db.Create(&u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u.ID
}

func (f *routesFixture) cookie(t *testing.T, userID uint) string {
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

func cloudLinked(id string) project.Project {
	return project.Project{ID: 1, OrganizationID: 7, CloudProjectID: &id}
}

func TestDeployEnqueuesForLinkedProject(t *testing.T) {
	fx := newRoutesFixture(t,
		fakeProjects{proj: cloudLinked("prj_cloud"), head: project.Revision{ID: 42}, hasHead: true},
		fakeAuthz{})
	user := fx.seedUser(t, "dev@example.test")

	resp := fx.api.Post("/api/v1/projects/1/deploy", fx.cookie(t, user))
	if resp.Code != http.StatusAccepted {
		t.Fatalf("deploy → %d, want 202: %s", resp.Code, resp.Body.String())
	}

	// The job was persisted against the project, queued, frozen to the head.
	list, err := fx.jobs.ListForProject(context.Background(), 1)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list[0].RevisionID != 42 || list[0].UserID != user || list[0].CloudProjectID != "prj_cloud" ||
		list[0].Status != cloudbuild.StatusQueued {
		t.Fatalf("enqueued job = %+v", list[0])
	}
}

func TestDeployRejectsUnlinkedProject(t *testing.T) {
	// A project with no Cloud link (CloudProjectID nil) is a 422, not a 500.
	fx := newRoutesFixture(t,
		fakeProjects{proj: project.Project{ID: 1, OrganizationID: 7}, head: project.Revision{ID: 42}, hasHead: true},
		fakeAuthz{})
	user := fx.seedUser(t, "dev@example.test")

	resp := fx.api.Post("/api/v1/projects/1/deploy", fx.cookie(t, user))
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("deploy unlinked → %d, want 422: %s", resp.Code, resp.Body.String())
	}
}

func TestGetBuildJobIsProjectScoped(t *testing.T) {
	// Deploy history is project-scoped: a project viewer who did NOT initiate a
	// job can still poll it (the regression the get/list mismatch would cause).
	fx := newRoutesFixture(t, fakeProjects{proj: cloudLinked("prj_cloud")}, fakeAuthz{})
	viewer := fx.seedUser(t, "viewer@example.test")

	// A job initiated by a different user (id 999).
	job, err := fx.jobs.Enqueue(context.Background(), 1, 42, 999, "prj_cloud")
	if err != nil {
		t.Fatal(err)
	}

	resp := fx.api.Get("/api/v1/build-jobs/"+strconv.FormatUint(uint64(job.ID), 10), fx.cookie(t, viewer))
	if resp.Code != http.StatusOK {
		t.Fatalf("viewer (non-initiator) get → %d, want 200: %s", resp.Code, resp.Body.String())
	}
}

func TestDeployRequiresAuth(t *testing.T) {
	fx := newRoutesFixture(t,
		fakeProjects{proj: cloudLinked("prj_cloud"), head: project.Revision{ID: 42}, hasHead: true},
		fakeAuthz{})
	// No cookie → the gate rejects before the handler.
	resp := fx.api.Post("/api/v1/projects/1/deploy")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated deploy → %d, want 401", resp.Code)
	}
}
