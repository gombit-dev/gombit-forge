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
	return candidateResponse(ctx, result)
}

// candidateResponse maps a candidate outcome to the shared HTTP response,
// keeping spec validity and ABI compatibility as separate states (ADR-001 §36):
// committed → 201, invalid_spec → 422 with diagnostics, breaking → 409 with
// reasons. It is shared by the raw candidate submit and the resource operations.
func candidateResponse(ctx context.Context, result CandidateResult) (*submitCandidateOutput, error) {
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
		return nil, contract.WithContext(ctx, contract.Validation("candidate spec is invalid", diagnosticFields(result.Diagnostics)))
	case OutcomeBreaking:
		// The reasons ride in the message rather than a structured field because
		// the contract's structured `fields` slot is validation-only (422); reusing
		// it for an ABI conflict would collapse the spec-vs-ABI distinction §36
		// keeps separate.
		return nil, contract.WithContext(ctx, contract.Conflict(
			"candidate is ABI-breaking and requires compatibility validation: "+strings.Join(result.Reasons, "; ")))
	default:
		return nil, contract.WithContext(ctx, contract.Internal("candidate"))
	}
}

// --- resource operations ---------------------------------------------------

type resourceBody struct {
	Label       string `json:"label" minLength:"1" maxLength:"120" doc:"Human-readable singular label"`
	LabelPlural string `json:"label_plural,omitempty" maxLength:"120" doc:"Human-readable plural label"`
}

type addResourceInput struct {
	ProjectID string `path:"projectID" doc:"Project identifier"`
	Body      resourceBody
}

func (h *Handler) addResource(ctx context.Context, in *addResourceInput) (*submitCandidateOutput, error) {
	p, user, err := h.loadAuthorized(ctx, in.ProjectID, org.CapProjectEdit)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.AddResource(ctx, p.ID, in.Body.Label, in.Body.LabelPlural, user.ID)
	if err != nil {
		return nil, mapError(ctx, err, "add resource")
	}
	return candidateResponse(ctx, result)
}

type renameResourceInput struct {
	ProjectID  string `path:"projectID" doc:"Project identifier"`
	ResourceID string `path:"resourceID" doc:"Resource stable ID"`
	Body       resourceBody
}

func (h *Handler) renameResource(ctx context.Context, in *renameResourceInput) (*submitCandidateOutput, error) {
	p, user, err := h.loadAuthorized(ctx, in.ProjectID, org.CapProjectEdit)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.RenameResource(ctx, p.ID, spec.ID(in.ResourceID), in.Body.Label, in.Body.LabelPlural, user.ID)
	if err != nil {
		return nil, mapError(ctx, err, "rename resource")
	}
	return candidateResponse(ctx, result)
}

type deleteResourceInput struct {
	ProjectID  string `path:"projectID" doc:"Project identifier"`
	ResourceID string `path:"resourceID" doc:"Resource stable ID"`
}

type blockerData struct {
	Kind    string `json:"kind" doc:"relationship, page or dashboard_card"`
	Message string `json:"message" doc:"Why the delete is blocked"`
}

type deleteResourceResult struct {
	Committed    bool          `json:"committed" doc:"Whether the deletion was applied"`
	RevisionID   *uint         `json:"revision_id,omitempty" doc:"The revision recording the deletion, when committed"`
	HadExtension bool          `json:"had_extension" doc:"Whether the resource carried custom code archived at build time"`
	Blockers     []blockerData `json:"blockers,omitempty" doc:"Dependencies that must be resolved first"`
}

type deleteResourceOutput struct {
	Status int
	Body   contract.Data[deleteResourceResult]
}

func (h *Handler) deleteResource(ctx context.Context, in *deleteResourceInput) (*deleteResourceOutput, error) {
	p, user, err := h.loadAuthorized(ctx, in.ProjectID, org.CapProjectEdit)
	if err != nil {
		return nil, err
	}
	del, err := h.svc.DeleteResource(ctx, p.ID, spec.ID(in.ResourceID), user.ID)
	if err != nil {
		return nil, mapError(ctx, err, "delete resource")
	}

	if del.Committed {
		var revID *uint
		if del.Revision != nil {
			revID = &del.Revision.ID
		}
		return &deleteResourceOutput{Status: 200, Body: contract.Data[deleteResourceResult]{Data: deleteResourceResult{
			Committed: true, RevisionID: revID, HadExtension: del.HadExtension,
		}}}, nil
	}
	if len(del.Diagnostics) > 0 {
		return nil, contract.WithContext(ctx, contract.Validation("deletion produced an invalid spec", diagnosticFields(del.Diagnostics)))
	}
	// Blocked by dependencies (§45). This is an expected outcome the editor must
	// render (what still references the resource), not a client error, so it is a
	// 200 carrying committed:false and the concrete blockers rather than a 4xx
	// whose structured body the client would have to parse out of an error
	// envelope.
	blockers := make([]blockerData, 0, len(del.Blockers))
	for _, b := range del.Blockers {
		blockers = append(blockers, blockerData{Kind: b.Kind, Message: b.Message})
	}
	return &deleteResourceOutput{Status: 200, Body: contract.Data[deleteResourceResult]{Data: deleteResourceResult{
		Committed: false, HadExtension: del.HadExtension, Blockers: blockers,
	}}}, nil
}

// --- field operations ------------------------------------------------------

type enumValueBody struct {
	Value string `json:"value" doc:"Stored enum value"`
	Label string `json:"label,omitempty" doc:"Human-readable enum label"`
}

type fieldBody struct {
	Label      string          `json:"label" minLength:"1" maxLength:"120" doc:"Human-readable field label"`
	Type       string          `json:"type" enum:"string,text,integer,decimal,boolean,datetime,date,enum" doc:"MVP field type"`
	Required   bool            `json:"required,omitempty"`
	Unique     bool            `json:"unique,omitempty"`
	Default    *string         `json:"default,omitempty" doc:"Literal default, null for none"`
	EnumValues []enumValueBody `json:"enum_values,omitempty" doc:"Permitted values when type is enum"`
}

func fieldInputFrom(b fieldBody) FieldInput {
	values := make([]spec.EnumValue, 0, len(b.EnumValues))
	for _, e := range b.EnumValues {
		values = append(values, spec.EnumValue{Value: e.Value, Label: e.Label})
	}
	return FieldInput{
		Label:      b.Label,
		Type:       spec.FieldType(b.Type),
		Required:   b.Required,
		Unique:     b.Unique,
		Default:    b.Default,
		EnumValues: values,
	}
}

type addFieldInput struct {
	ProjectID  string `path:"projectID" doc:"Project identifier"`
	ResourceID string `path:"resourceID" doc:"Resource stable ID"`
	Body       fieldBody
}

func (h *Handler) addField(ctx context.Context, in *addFieldInput) (*submitCandidateOutput, error) {
	p, user, err := h.loadAuthorized(ctx, in.ProjectID, org.CapProjectEdit)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.AddField(ctx, p.ID, spec.ID(in.ResourceID), fieldInputFrom(in.Body), user.ID)
	if err != nil {
		return nil, mapError(ctx, err, "add field")
	}
	return candidateResponse(ctx, result)
}

type updateFieldInput struct {
	ProjectID  string `path:"projectID" doc:"Project identifier"`
	ResourceID string `path:"resourceID" doc:"Resource stable ID"`
	FieldID    string `path:"fieldID" doc:"Field stable ID"`
	Body       fieldBody
}

func (h *Handler) updateField(ctx context.Context, in *updateFieldInput) (*submitCandidateOutput, error) {
	p, user, err := h.loadAuthorized(ctx, in.ProjectID, org.CapProjectEdit)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.UpdateField(ctx, p.ID, spec.ID(in.ResourceID), spec.ID(in.FieldID), fieldInputFrom(in.Body), user.ID)
	if err != nil {
		return nil, mapError(ctx, err, "update field")
	}
	return candidateResponse(ctx, result)
}

type deleteFieldInput struct {
	ProjectID  string `path:"projectID" doc:"Project identifier"`
	ResourceID string `path:"resourceID" doc:"Resource stable ID"`
	FieldID    string `path:"fieldID" doc:"Field stable ID"`
}

func (h *Handler) deleteField(ctx context.Context, in *deleteFieldInput) (*submitCandidateOutput, error) {
	p, user, err := h.loadAuthorized(ctx, in.ProjectID, org.CapProjectEdit)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.DeleteField(ctx, p.ID, spec.ID(in.ResourceID), spec.ID(in.FieldID), user.ID)
	if err != nil {
		return nil, mapError(ctx, err, "delete field")
	}
	return candidateResponse(ctx, result)
}

// --- relationship operation ------------------------------------------------

type addRelationshipInput struct {
	ProjectID  string `path:"projectID" doc:"Project identifier"`
	ResourceID string `path:"resourceID" doc:"Owning resource stable ID"`
	Body       struct {
		Label        string `json:"label" minLength:"1" maxLength:"120" doc:"Relationship label on the owning resource"`
		Target       string `json:"target" minLength:"1" doc:"Target resource stable ID"`
		InverseLabel string `json:"inverse_label,omitempty" doc:"Derived has_many label on the target"`
		Required     bool   `json:"required,omitempty"`
	}
}

func (h *Handler) addRelationship(ctx context.Context, in *addRelationshipInput) (*submitCandidateOutput, error) {
	p, user, err := h.loadAuthorized(ctx, in.ProjectID, org.CapProjectEdit)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.AddRelationship(ctx, p.ID, spec.ID(in.ResourceID), RelationshipInput{
		Label:        in.Body.Label,
		Target:       spec.ID(in.Body.Target),
		InverseLabel: in.Body.InverseLabel,
		Required:     in.Body.Required,
	}, user.ID)
	if err != nil {
		return nil, mapError(ctx, err, "add relationship")
	}
	return candidateResponse(ctx, result)
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
	case errors.Is(err, ErrResourceNotFound):
		return contract.WithContext(ctx, contract.NotFound("resource not found"))
	case errors.Is(err, ErrFieldNotFound):
		return contract.WithContext(ctx, contract.NotFound("field not found"))
	case errors.Is(err, ErrRelationshipTarget):
		return contract.WithContext(ctx, contract.Validation("invalid relationship", map[string][]string{
			"target": {"must reference an existing resource"},
		}))
	case errors.Is(err, ErrInvalidFieldEdit):
		return contract.WithContext(ctx, contract.Validation("invalid field", map[string][]string{
			"type": {"must be a supported MVP field type"},
		}))
	case errors.Is(err, org.ErrNotMember), errors.Is(err, org.ErrForbidden):
		return contract.WithContext(ctx, contract.Authorization("not permitted"))
	case errors.Is(err, ErrInvalidResourceEdit):
		return contract.WithContext(ctx, contract.Validation("invalid resource edit", map[string][]string{
			"label": {"a resource label is required"},
		}))
	case errors.Is(err, ErrNoSpec):
		return contract.WithContext(ctx, contract.Conflict("project has no revisions yet"))
	case errors.Is(err, ErrInvalidSpec):
		return contract.WithContext(ctx, contract.Validation("invalid spec", nil))
	default:
		return contract.WithContext(ctx, contract.Internal(action))
	}
}
