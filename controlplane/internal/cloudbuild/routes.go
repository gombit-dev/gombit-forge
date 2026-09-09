package cloudbuild

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gombit-dev/gombit/auth"
	"github.com/gombit-dev/gombit/contract"
	"github.com/gombit-dev/gombit/framework"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/org"
	"github.com/gombit-dev/gombit-forge/controlplane/internal/project"
)

const cookieSecurityName = "cookieAuth"

// Projects is the subset of project.Service the deploy routes need: load a
// project (for its org and Cloud linkage, to authorize and target) and resolve
// its head revision (to freeze).
type Projects interface {
	GetProject(ctx context.Context, projectID uint) (project.Project, error)
	Head(ctx context.Context, projectID uint) (project.Revision, bool, error)
}

// Authorizer is org.Service.Authorize — the per-org capability check.
type Authorizer interface {
	Authorize(ctx context.Context, orgID, userID uint, capability org.Capability) error
}

// Register mounts the cloud-build (deploy) routes on the app behind the cookie
// gate.
func Register(app *framework.App, jobs *Service, projects Projects, authz Authorizer) error {
	authSvc, err := auth.NewService(app.DB(), app.Config())
	if err != nil {
		return err
	}
	RegisterRoutes(app.API(), app.Config().API.Prefix,
		huma.Middlewares{authSvc.RequireCookieSession()}, jobs, projects, authz)
	return nil
}

// RegisterRoutes wires the deploy/build-job operations onto api behind gate.
// Split from Register so the real routes + cookie gate are testable on a
// humatest API without a full framework.App.
func RegisterRoutes(api huma.API, prefix string, gate huma.Middlewares, jobs *Service, projects Projects, authz Authorizer) {
	h := &handler{jobs: jobs, projects: projects, authz: authz}
	security := []map[string][]string{{cookieSecurityName: {}}}
	tags := []string{"Deploy"}

	huma.Register(api, huma.Operation{
		OperationID:   "deploy-project",
		Method:        http.MethodPost,
		Path:          prefix + "/projects/{projectID}/deploy",
		Summary:       "Deploy the project's current revision to Gombit Cloud",
		Description:   "Freezes the project's head revision and enqueues an asynchronous Cloud build (source is assembled and submitted by a worker; no request performs a build). Returns 202 with a build-job id to poll.",
		Tags:          tags,
		Security:      security,
		Middlewares:   gate,
		DefaultStatus: http.StatusAccepted,
	}, h.deploy)

	huma.Register(api, huma.Operation{
		OperationID: "get-build-job",
		Method:      http.MethodGet,
		Path:        prefix + "/build-jobs/{jobID}",
		Summary:     "Get a Cloud build job's status",
		Tags:        tags,
		Security:    security,
		Middlewares: gate,
	}, h.get)

	huma.Register(api, huma.Operation{
		OperationID: "list-project-build-jobs",
		Method:      http.MethodGet,
		Path:        prefix + "/projects/{projectID}/build-jobs",
		Summary:     "List a project's Cloud build jobs (deploy history)",
		Tags:        tags,
		Security:    security,
		Middlewares: gate,
	}, h.list)
}

type handler struct {
	jobs     *Service
	projects Projects
	authz    Authorizer
}

// jobData is a build job as the API exposes it.
type jobData struct {
	ID             uint   `json:"id"`
	ProjectID      uint   `json:"project_id"`
	Status         string `json:"status"`
	CloudBuildID   string `json:"cloud_build_id,omitempty" doc:"The Gombit Cloud build id, once the worker has created it"`
	CloudStatus    string `json:"cloud_status,omitempty" doc:"The last Cloud build state observed (Cloud's own vocabulary)"`
	ArtifactDigest string `json:"artifact_digest,omitempty" doc:"The content-addressed artifact digest, once the build has succeeded"`
	Error          string `json:"error,omitempty" doc:"A sanitized failure reason, once the job has failed"`
}

func toJobData(j BuildJob) jobData {
	return jobData{
		ID: j.ID, ProjectID: j.ProjectID, Status: string(j.Status),
		CloudBuildID: j.CloudBuildID, CloudStatus: j.CloudStatus,
		ArtifactDigest: j.ArtifactDigest, Error: j.Error,
	}
}

type deployInput struct {
	ProjectID string `path:"projectID" doc:"Project identifier"`
}

type deployOutput struct {
	Status int
	Body   contract.Data[jobData]
}

func (h *handler) deploy(ctx context.Context, in *deployInput) (*deployOutput, error) {
	// Deploying builds and ships the project's source on Cloud — a write action,
	// gated on edit (not view), so a future read-only viewer can't trigger a
	// deploy. A non-member gets NotFound so cross-org project ids can't be probed.
	p, user, err := h.loadAuthorized(ctx, in.ProjectID, org.CapProjectEdit)
	if err != nil {
		return nil, err
	}

	head, ok, err := h.projects.Head(ctx, p.ID)
	if err != nil {
		return nil, contract.WithContext(ctx, contract.Internal("could not resolve the project's head revision"))
	}
	if !ok {
		return nil, contract.WithContext(ctx, contract.Validation("the project has no revision to deploy", map[string][]string{
			"project": {"create a revision before deploying"},
		}))
	}

	cloudProjectID := ""
	if p.CloudProjectID != nil {
		cloudProjectID = *p.CloudProjectID
	}
	job, err := h.jobs.Enqueue(ctx, p.ID, head.ID, user.ID, cloudProjectID)
	if err != nil {
		// A project not linked to Cloud is a precondition failure (422), not a
		// storage fault (500).
		if errors.Is(err, ErrNoCloudProject) {
			return nil, contract.WithContext(ctx, contract.Validation("the project is not linked to a Gombit Cloud project", map[string][]string{
				"project": {"link the project to Gombit Cloud before deploying"},
			}))
		}
		return nil, contract.WithContext(ctx, contract.Internal("could not enqueue the deploy"))
	}
	return &deployOutput{Status: http.StatusAccepted, Body: contract.Data[jobData]{Data: toJobData(job)}}, nil
}

type getInput struct {
	JobID string `path:"jobID" doc:"Build job identifier"`
}

type getOutput struct {
	Body contract.Data[jobData]
}

func (h *handler) get(ctx context.Context, in *getInput) (*getOutput, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	jobID, err := parseID(in.JobID)
	if err != nil {
		return nil, contract.WithContext(ctx, contract.NotFound("build job not found"))
	}
	job, ok, err := h.jobs.Get(ctx, jobID)
	if err != nil {
		return nil, contract.WithContext(ctx, contract.Internal("could not load the build job"))
	}
	// A job is owned by the user who initiated it. A non-owner (or a missing job)
	// gets the same NotFound — no IDOR, and existence isn't leaked across users.
	if !ok || job.UserID != user.ID {
		return nil, contract.WithContext(ctx, contract.NotFound("build job not found"))
	}
	return &getOutput{Body: contract.Data[jobData]{Data: toJobData(job)}}, nil
}

type listInput struct {
	ProjectID string `path:"projectID" doc:"Project identifier"`
}

type listOutput struct {
	Body contract.Data[[]jobData]
}

func (h *handler) list(ctx context.Context, in *listInput) (*listOutput, error) {
	// Listing a project's deploy history is a read within the project; gate on
	// view. Tenancy-safe: a non-member sees NotFound.
	p, _, err := h.loadAuthorized(ctx, in.ProjectID, org.CapProjectView)
	if err != nil {
		return nil, err
	}
	jobs, err := h.jobs.ListForProject(ctx, p.ID)
	if err != nil {
		return nil, contract.WithContext(ctx, contract.Internal("could not list the project's build jobs"))
	}
	out := make([]jobData, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, toJobData(j))
	}
	return &listOutput{Body: contract.Data[[]jobData]{Data: out}}, nil
}

// loadAuthorized resolves and authorizes the project for the caller, mapping a
// non-member and a missing/invalid project alike to NotFound (tenancy-safe).
func (h *handler) loadAuthorized(ctx context.Context, projectIDParam string, capability org.Capability) (project.Project, auth.User, error) {
	user, err := caller(ctx)
	if err != nil {
		return project.Project{}, auth.User{}, err
	}
	projectID, err := parseID(projectIDParam)
	if err != nil {
		return project.Project{}, auth.User{}, contract.WithContext(ctx, contract.NotFound("project not found"))
	}
	p, err := h.projects.GetProject(ctx, projectID)
	if err != nil {
		if errors.Is(err, project.ErrProjectNotFound) {
			return project.Project{}, auth.User{}, contract.WithContext(ctx, contract.NotFound("project not found"))
		}
		return project.Project{}, auth.User{}, contract.WithContext(ctx, contract.Internal("could not load the project"))
	}
	if err := h.authz.Authorize(ctx, p.OrganizationID, user.ID, capability); err != nil {
		if errors.Is(err, org.ErrNotMember) || errors.Is(err, org.ErrForbidden) {
			return project.Project{}, auth.User{}, contract.WithContext(ctx, contract.NotFound("project not found"))
		}
		return project.Project{}, auth.User{}, contract.WithContext(ctx, contract.Internal("could not authorize the project"))
	}
	return p, user, nil
}

func caller(ctx context.Context) (auth.User, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return auth.User{}, contract.WithContext(ctx, contract.Authentication("authentication required"))
	}
	return user, nil
}

func parseID(s string) (uint, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(n), nil
}
