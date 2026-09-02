package project

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/gombit-dev/gombit/auth"
	"github.com/gombit-dev/gombit/contract"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/org"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

// Handler serves the project HTTP operations. Every operation is mounted behind
// the cookie-session gate (see Register), so auth.UserFromContext always yields
// the caller; per-org authorization then happens inside each handler against the
// caller's role via the org service.
type Handler struct {
	svc   *Service
	authz *org.Service
}

// --- wire types ------------------------------------------------------------

type projectData struct {
	ID             uint   `json:"id" doc:"Project identifier"`
	OrganizationID uint   `json:"organization_id" doc:"Owning organization"`
	Name           string `json:"name" doc:"Human-readable project name"`
	Slug           string `json:"slug" doc:"URL-safe project key, unique within the org"`
	HeadRevisionID *uint  `json:"head_revision_id,omitempty" doc:"Current revision, null until the first is created"`
}

type revisionData struct {
	ID               uint   `json:"id" doc:"Revision identifier"`
	ProjectID        uint   `json:"project_id" doc:"Owning project"`
	SpecHash         string `json:"spec_hash" doc:"SHA-256 of the canonical spec"`
	ParentRevisionID *uint  `json:"parent_revision_id,omitempty" doc:"Revision this one descended from"`
	// Class is the ABI classification of the transition that produced this
	// revision ("neutral" or "additive"); empty for a project's first revision.
	Class string `json:"abi_class,omitempty" doc:"ABI classification of the accepted transition"`
}

func toProjectData(p Project) projectData {
	return projectData{
		ID: p.ID, OrganizationID: p.OrganizationID,
		Name: p.Name, Slug: p.Slug, HeadRevisionID: p.HeadRevisionID,
	}
}

// --- create project --------------------------------------------------------

type createProjectInput struct {
	OrgID string `path:"orgID" doc:"Organization identifier"`
	Body  struct {
		Name string `json:"name" minLength:"1" maxLength:"120" doc:"Human-readable project name"`
		Slug string `json:"slug" minLength:"1" maxLength:"120" pattern:"^[a-z0-9]+(-[a-z0-9]+)*$" doc:"URL-safe unique key within the org"`
	}
}

type createProjectOutput struct {
	Body contract.Data[projectData]
}

func (h *Handler) createProject(ctx context.Context, in *createProjectInput) (*createProjectOutput, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseID(in.OrgID)
	if err != nil {
		return nil, contract.WithContext(ctx, contract.NotFound("organization not found"))
	}
	// Org-scoped operations authorize by the explicit orgID and, for a
	// non-member, surface 403 ("not permitted") via mapError — deliberately, to
	// match the org package's own convention (listMembers does the same). Org
	// existence is not hidden here: org IDs already circulate through invitations
	// and membership. Project *existence* is what must not leak across a tenancy
	// boundary, which the project-scoped ops enforce with 404 (see loadAuthorized).
	if err := h.authz.Authorize(ctx, orgID, user.ID, org.CapProjectCreate); err != nil {
		return nil, mapError(ctx, err, "create project")
	}
	p, err := h.svc.CreateProject(ctx, orgID, in.Body.Name, in.Body.Slug, user.ID)
	if err != nil {
		return nil, mapError(ctx, err, "create project")
	}
	return &createProjectOutput{Body: contract.Data[projectData]{Data: toProjectData(p)}}, nil
}

// --- list projects ---------------------------------------------------------

type listProjectsInput struct {
	OrgID string `path:"orgID" doc:"Organization identifier"`
}

type listProjectsOutput struct {
	Body contract.Data[[]projectData]
}

func (h *Handler) listProjects(ctx context.Context, in *listProjectsInput) (*listProjectsOutput, error) {
	user, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	orgID, err := parseID(in.OrgID)
	if err != nil {
		return nil, contract.WithContext(ctx, contract.NotFound("organization not found"))
	}
	if err := h.authz.Authorize(ctx, orgID, user.ID, org.CapProjectView); err != nil {
		return nil, mapError(ctx, err, "list projects")
	}
	projects, err := h.svc.ListProjects(ctx, orgID)
	if err != nil {
		return nil, mapError(ctx, err, "list projects")
	}
	rows := make([]projectData, 0, len(projects))
	for _, p := range projects {
		rows = append(rows, toProjectData(p))
	}
	return &listProjectsOutput{Body: contract.Data[[]projectData]{Data: rows}}, nil
}

// --- get project -----------------------------------------------------------

type getProjectInput struct {
	ProjectID string `path:"projectID" doc:"Project identifier"`
}

type getProjectOutput struct {
	Body contract.Data[projectData]
}

func (h *Handler) getProject(ctx context.Context, in *getProjectInput) (*getProjectOutput, error) {
	p, _, err := h.loadAuthorized(ctx, in.ProjectID, org.CapProjectView)
	if err != nil {
		return nil, err
	}
	return &getProjectOutput{Body: contract.Data[projectData]{Data: toProjectData(p)}}, nil
}

// --- get head spec ---------------------------------------------------------

type getProjectSpecInput struct {
	ProjectID string `path:"projectID" doc:"Project identifier"`
}

type getProjectSpecOutput struct {
	// Body wraps the head revision's canonical spec as raw JSON, or null when the
	// project has no revisions yet. The editor loads this to populate the Data
	// area; it is the exact bytes the revision pinned, not a re-encoding.
	Body contract.Data[json.RawMessage]
}

func (h *Handler) getProjectSpec(ctx context.Context, in *getProjectSpecInput) (*getProjectSpecOutput, error) {
	p, _, err := h.loadAuthorized(ctx, in.ProjectID, org.CapProjectView)
	if err != nil {
		return nil, err
	}
	head, ok, err := h.svc.Head(ctx, p.ID)
	if err != nil {
		return nil, mapError(ctx, err, "load project spec")
	}
	spec := json.RawMessage("null")
	if ok {
		spec = json.RawMessage(head.SpecJSON)
	}
	return &getProjectSpecOutput{Body: contract.Data[json.RawMessage]{Data: spec}}, nil
}

// --- submit candidate ------------------------------------------------------

type submitCandidateInput struct {
	ProjectID string `path:"projectID" doc:"Project identifier"`
	// RawBody is the candidate ProjectSpec as canonical JSON. It is taken raw
	// rather than through a generated schema because the spec's own validator
	// (spec.Validate) is the authority on its shape, not OpenAPI.
	RawBody []byte
}

type submitCandidateOutput struct {
	Status int
	Body   contract.Data[revisionData]
}

func (h *Handler) submitCandidate(ctx context.Context, in *submitCandidateInput) (*submitCandidateOutput, error) {
	p, user, err := h.loadAuthorized(ctx, in.ProjectID, org.CapProjectEdit)
	if err != nil {
		return nil, err
	}
	candidate, err := spec.Unmarshal(in.RawBody)
	if err != nil {
		return nil, contract.WithContext(ctx, contract.Validation("candidate is not a decodable spec", map[string][]string{
			"body": {err.Error()},
		}))
	}

	result, err := h.svc.SubmitCandidate(ctx, p.ID, candidate, user.ID)
	if err != nil {
		return nil, mapError(ctx, err, "submit candidate")
	}

	switch result.Outcome {
	case OutcomeCommitted:
		return &submitCandidateOutput{Status: 201, Body: contract.Data[revisionData]{Data: revisionData{
			ID:               result.Revision.ID,
			ProjectID:        result.Revision.ProjectID,
			SpecHash:         result.Revision.SpecHash,
			ParentRevisionID: result.Revision.ParentRevisionID,
			Class:            result.Class,
		}}}, nil
	case OutcomeInvalidSpec:
		// Spec validity is its own state (ADR-001 §36): a 422 with the
		// diagnostics, keyed by the offending path.
		return nil, contract.WithContext(ctx, contract.Validation("candidate spec is invalid", diagnosticFields(result.Diagnostics)))
	case OutcomeBreaking:
		// ABI compatibility is a separate state from spec validity (§36): a
		// breaking candidate is a 409, not a 422, and it names why. Committing it
		// needs a compatibility build the request path does not run (D8).
		//
		// The reasons ride in the message rather than a structured field because
		// the contract's structured `fields` slot is validation-only (422); reusing
		// it for an ABI conflict would collapse the very spec-vs-ABI distinction
		// §36 keeps separate. A future structured surface for the reasons belongs
		// alongside the async compatibility-validation result, not on this 409.
		return nil, contract.WithContext(ctx, contract.Conflict(
			"candidate is ABI-breaking and requires compatibility validation: "+strings.Join(result.Reasons, "; ")))
	default:
		return nil, contract.WithContext(ctx, contract.Internal("submit candidate"))
	}
}

// --- helpers ---------------------------------------------------------------

// caller extracts the authenticated user the cookie gate guarantees.
func caller(ctx context.Context) (auth.User, error) {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return auth.User{}, contract.WithContext(ctx, contract.Authentication("authentication required"))
	}
	return user, nil
}

// loadAuthorized loads a project by its path id and authorizes the caller
// against the project's organization for capability. A missing project and an
// unauthorized caller both return NotFound, so a project's existence never leaks
// across an organization boundary the caller is not a member of.
func (h *Handler) loadAuthorized(ctx context.Context, projectIDParam string, capability org.Capability) (Project, auth.User, error) {
	user, err := caller(ctx)
	if err != nil {
		return Project{}, auth.User{}, err
	}
	projectID, err := parseID(projectIDParam)
	if err != nil {
		return Project{}, auth.User{}, contract.WithContext(ctx, contract.NotFound("project not found"))
	}
	p, err := h.svc.GetProject(ctx, projectID)
	if err != nil {
		if errors.Is(err, ErrProjectNotFound) {
			return Project{}, auth.User{}, contract.WithContext(ctx, contract.NotFound("project not found"))
		}
		return Project{}, auth.User{}, mapError(ctx, err, "load project")
	}
	if err := h.authz.Authorize(ctx, p.OrganizationID, user.ID, capability); err != nil {
		// Tenancy-safe: a non-member gets NotFound, not Forbidden, so cross-org
		// project ids cannot be probed for existence.
		if errors.Is(err, org.ErrNotMember) || errors.Is(err, org.ErrForbidden) {
			return Project{}, auth.User{}, contract.WithContext(ctx, contract.NotFound("project not found"))
		}
		return Project{}, auth.User{}, mapError(ctx, err, "authorize project")
	}
	return p, user, nil
}

func parseID(s string) (uint, error) {
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(n), nil
}

// diagnosticFields turns spec diagnostics into the validation field map, keyed
// by the offending path so the editor can focus the right control.
func diagnosticFields(ds spec.Diagnostics) map[string][]string {
	fields := make(map[string][]string, len(ds))
	for _, d := range ds {
		key := d.Path
		if key == "" {
			key = string(d.Code)
		}
		fields[key] = append(fields[key], d.Message)
	}
	return fields
}

// mapError translates a service or org error into the matching contract
// envelope. The default is a 500 that does not leak the underlying message.
func mapError(ctx context.Context, err error, action string) error {
	switch {
	case errors.Is(err, ErrProjectNotFound):
		return contract.WithContext(ctx, contract.NotFound("project not found"))
	case errors.Is(err, org.ErrNotMember), errors.Is(err, org.ErrForbidden):
		return contract.WithContext(ctx, contract.Authorization("not permitted"))
	case errors.Is(err, ErrInvalidSpec):
		return contract.WithContext(ctx, contract.Validation("invalid spec", nil))
	default:
		return contract.WithContext(ctx, contract.Internal(action))
	}
}
