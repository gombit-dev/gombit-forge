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

	"github.com/gombit-dev/gombit-forge/controlplane/internal/cloudclient"
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

// BuildLogs reads a Cloud build's log lines (optionally only those after `since`,
// RFC3339, for tailing). *cloudclient.Client satisfies it. Forge relays Cloud's
// logs; it never stores them (ADR-005 §24).
type BuildLogs interface {
	GetBuildLogs(ctx context.Context, buildID, since string) ([]cloudclient.BuildLog, error)
}

// Register mounts the cloud-build (deploy) routes on the app behind the cookie
// gate.
func Register(app *framework.App, jobs *Service, projects Projects, authz Authorizer, logs BuildLogs) error {
	authSvc, err := auth.NewService(app.DB(), app.Config())
	if err != nil {
		return err
	}
	RegisterRoutes(app.API(), app.Config().API.Prefix,
		huma.Middlewares{authSvc.RequireCookieSession()}, jobs, projects, authz, logs)
	return nil
}

// RegisterRoutes wires the deploy/build-job operations onto api behind gate.
// Split from Register so the real routes + cookie gate are testable on a
// humatest API without a full framework.App.
func RegisterRoutes(api huma.API, prefix string, gate huma.Middlewares, jobs *Service, projects Projects, authz Authorizer, logs BuildLogs) {
	h := &handler{jobs: jobs, projects: projects, authz: authz, logs: logs}
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

	huma.Register(api, huma.Operation{
		OperationID: "get-build-job-logs",
		Method:      http.MethodGet,
		Path:        prefix + "/build-jobs/{jobID}/logs",
		Summary:     "Read a build job's Cloud build logs",
		Description: "Relays the Cloud build's log lines (read from Cloud, not stored by Forge). Pass ?since=<RFC3339> to tail only newer lines. Empty until the worker has created the Cloud build.",
		Tags:        tags,
		Security:    security,
		Middlewares: gate,
	}, h.logsHandler)
}

type handler struct {
	jobs     *Service
	projects Projects
	authz    Authorizer
	logs     BuildLogs
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
	if !ok {
		return nil, contract.WithContext(ctx, contract.NotFound("build job not found"))
	}
	// Deploy history is project-scoped, not initiator-private: any project viewer
	// can poll any of the project's jobs, consistent with the list endpoint (which
	// already exposes the same fields). A non-viewer, or a vanished project, maps
	// to the same build-job NotFound, so a job's existence isn't leaked.
	if _, err := h.resolveAuthorized(ctx, job.ProjectID, user.ID, org.CapProjectView); err != nil {
		return nil, mapAuthErr(ctx, err, "build job")
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

type logsInput struct {
	JobID string `path:"jobID" doc:"Build job identifier"`
	Since string `query:"since" doc:"Only return log lines after this RFC3339 timestamp (for tailing)"`
}

// logLine is a build log line as the API exposes it — Forge's own shape, not the
// Cloud client's type, so the transport boundary doesn't leak into the API.
type logLine struct {
	Timestamp string `json:"timestamp"`
	Stream    string `json:"stream,omitempty"`
	Message   string `json:"message"`
}

type logsOutput struct {
	Body contract.Data[[]logLine]
}

func (h *handler) logsHandler(ctx context.Context, in *logsInput) (*logsOutput, error) {
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
	if !ok {
		return nil, contract.WithContext(ctx, contract.NotFound("build job not found"))
	}
	// Project-scoped, like get/list: any project viewer can read the logs.
	if _, err := h.resolveAuthorized(ctx, job.ProjectID, user.ID, org.CapProjectView); err != nil {
		return nil, mapAuthErr(ctx, err, "build job")
	}
	// No Cloud build has been created yet (the worker hasn't run, or the job
	// failed before submitting) — there are simply no logs, not an error.
	if job.CloudBuildID == "" {
		return &logsOutput{Body: contract.Data[[]logLine]{Data: []logLine{}}}, nil
	}
	lines, err := h.logs.GetBuildLogs(ctx, job.CloudBuildID, in.Since)
	if err != nil {
		return nil, contract.WithContext(ctx, contract.Internal("could not read the build logs from Gombit Cloud"))
	}
	out := make([]logLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, logLine{Timestamp: l.Timestamp, Stream: l.Stream, Message: l.Message})
	}
	return &logsOutput{Body: contract.Data[[]logLine]{Data: out}}, nil
}

// loadAuthorized resolves and authorizes the project named in a path param for
// the caller, mapping a non-member and a missing/invalid project alike to
// NotFound (tenancy-safe).
func (h *handler) loadAuthorized(ctx context.Context, projectIDParam string, capability org.Capability) (project.Project, auth.User, error) {
	user, err := caller(ctx)
	if err != nil {
		return project.Project{}, auth.User{}, err
	}
	projectID, err := parseID(projectIDParam)
	if err != nil {
		return project.Project{}, auth.User{}, contract.WithContext(ctx, contract.NotFound("project not found"))
	}
	p, err := h.resolveAuthorized(ctx, projectID, user.ID, capability)
	if err != nil {
		return project.Project{}, auth.User{}, mapAuthErr(ctx, err, "project")
	}
	return p, user, nil
}

// resolveAuthorized loads the project and checks the capability, returning the
// *unmapped* domain error (project.ErrProjectNotFound / org.ErrNotMember /
// org.ErrForbidden) or a genuine fault — so each caller maps it to the right
// resource's NotFound (a project route says "project", the job route says "build
// job"), rather than leaking that a job's project is the thing that was missing.
func (h *handler) resolveAuthorized(ctx context.Context, projectID, userID uint, capability org.Capability) (project.Project, error) {
	p, err := h.projects.GetProject(ctx, projectID)
	if err != nil {
		return project.Project{}, err
	}
	if err := h.authz.Authorize(ctx, p.OrganizationID, userID, capability); err != nil {
		return project.Project{}, err
	}
	return p, nil
}

// mapAuthErr maps a resolveAuthorized error to a contract error: a missing
// project, a non-member, and a forbidden caller all collapse to NotFound on
// resource (tenancy-safe — no existence leak, no absent/forbidden distinction);
// anything else is a genuine fault (500).
func mapAuthErr(ctx context.Context, err error, resource string) error {
	if errors.Is(err, project.ErrProjectNotFound) || errors.Is(err, org.ErrNotMember) || errors.Is(err, org.ErrForbidden) {
		return contract.WithContext(ctx, contract.NotFound(resource+" not found"))
	}
	return contract.WithContext(ctx, contract.Internal("could not authorize the "+resource))
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
