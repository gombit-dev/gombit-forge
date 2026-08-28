package spec

import (
	"fmt"
	"sort"
	"strings"
)

// Code identifies a class of validation failure. Codes are stable so the
// editor can map a diagnostic back to a specific control.
type Code string

const (
	CodeSpecVersion     Code = "spec_version_unsupported"
	CodeMissingID       Code = "missing_id"
	CodeMalformedID     Code = "malformed_id"
	CodeDuplicateID     Code = "duplicate_id"
	CodeInvalidSlug     Code = "invalid_slug"
	CodeDuplicateSlug   Code = "duplicate_slug"
	CodeInvalidStorage  Code = "invalid_storage_name"
	CodeDuplicateStore  Code = "duplicate_storage_name"
	CodeInvalidCodeName Code = "invalid_code_name"
	CodeDuplicateCode   Code = "duplicate_code_name"
	CodeMissingLabel    Code = "missing_label"
	CodeUnknownType     Code = "unsupported_field_type"
	CodeDanglingRef     Code = "dangling_reference"
	CodeInvalidEnum     Code = "invalid_enum"
	CodeInvalidDriver   Code = "invalid_database_driver"
	CodeInvalidAuth     Code = "invalid_auth_mode"
	CodeInvalidPage     Code = "invalid_page"
	CodeInvalidNav      Code = "invalid_navigation"
	CodeEmptyProject    Code = "empty_project"
)

// Diagnostic is one structured, machine-readable validation failure.
//
// Validation reports diagnostics rather than panicking or failing on the
// first problem, so the editor can surface every issue at once (ADR-001 §36).
type Diagnostic struct {
	Code    Code   `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
	// Entity is the stable ID of the offending entity when one is known, so
	// the editor can focus the right control even after a relabel.
	Entity ID `json:"entity,omitempty"`
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s: %s (%s)", d.Path, d.Message, d.Code)
}

// Diagnostics is an ordered collection of validation failures.
type Diagnostics []Diagnostic

// Error renders the diagnostics as a single error message.
func (ds Diagnostics) Error() string {
	if len(ds) == 0 {
		return "spec: valid"
	}
	parts := make([]string, 0, len(ds))
	for _, diagnostic := range ds {
		parts = append(parts, diagnostic.String())
	}
	return fmt.Sprintf("spec: %d validation error(s):\n  %s", len(ds), strings.Join(parts, "\n  "))
}

// Has reports whether any diagnostic carries the given code.
func (ds Diagnostics) Has(code Code) bool {
	for _, diagnostic := range ds {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

// Codes returns the distinct codes present, sorted for stable comparison.
func (ds Diagnostics) Codes() []Code {
	seen := map[Code]struct{}{}
	for _, diagnostic := range ds {
		seen[diagnostic.Code] = struct{}{}
	}
	codes := make([]Code, 0, len(seen))
	for code := range seen {
		codes = append(codes, code)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	return codes
}

// validator accumulates diagnostics while walking a spec.
type validator struct {
	spec *ProjectSpec
	out  Diagnostics

	// seenIDs enforces that a stable ID identifies exactly one entity
	// anywhere in the spec (ADR-001 §36 "duplicate stable ID").
	seenIDs map[ID]string
}

func (v *validator) report(code Code, path string, entity ID, format string, args ...any) {
	v.out = append(v.out, Diagnostic{
		Code:    code,
		Path:    path,
		Entity:  entity,
		Message: fmt.Sprintf(format, args...),
	})
}

// claimID records an ID and reports a duplicate if it was already used.
func (v *validator) claimID(id ID, kind Kind, path string) {
	if id == "" {
		v.report(CodeMissingID, path, "", "missing stable id")
		return
	}
	if !id.Valid(kind) {
		v.report(CodeMalformedID, path, id, "malformed %s id %q", kind, id)
		return
	}
	if previous, taken := v.seenIDs[id]; taken {
		v.report(CodeDuplicateID, path, id, "stable id %q already used at %s", id, previous)
		return
	}
	v.seenIDs[id] = path
}

// Validate checks that a ProjectSpec is semantically well-formed.
//
// This answers only the "spec validity" question. It is deliberately
// independent of extension-ABI compatibility and of whether the project
// currently builds, which are separate states (ADR-001 §36).
//
// It returns nil when the spec is valid.
func Validate(s *ProjectSpec) Diagnostics {
	if s == nil {
		return Diagnostics{{
			Code: CodeEmptyProject, Path: "$", Message: "nil spec",
		}}
	}

	v := &validator{spec: s, seenIDs: map[ID]string{}}

	if s.SpecVersion != SpecVersion {
		v.report(CodeSpecVersion, "$.spec_version", "",
			"unsupported spec_version %d (expected %d)", s.SpecVersion, SpecVersion)
	}

	v.validateProject()
	v.validateResources()
	v.validatePages()
	v.validateNavigation()

	if len(v.out) == 0 {
		return nil
	}
	return v.out
}

func (v *validator) validateProject() {
	project := v.spec.Project
	v.claimID(project.ID, KindProject, "$.project.id")

	if strings.TrimSpace(project.Name) == "" {
		v.report(CodeMissingLabel, "$.project.name", project.ID, "project name is required")
	}
	if !IsSlug(project.Slug) {
		v.report(CodeInvalidSlug, "$.project.slug", project.ID,
			"project slug %q must be lower kebab-case", project.Slug)
	}

	switch v.spec.Database.Driver {
	case DriverPostgres, DriverSQLite, DriverMySQL:
	default:
		v.report(CodeInvalidDriver, "$.database.driver", project.ID,
			"unsupported database driver %q", v.spec.Database.Driver)
	}

	switch v.spec.Auth.Mode {
	case AuthCookie, AuthJWT, AuthNone:
	default:
		v.report(CodeInvalidAuth, "$.auth.mode", project.ID,
			"unsupported auth mode %q", v.spec.Auth.Mode)
	}
}

func (v *validator) validateResources() {
	resourceCodeNames := map[string]ID{}
	resourceStorage := map[string]ID{}

	for resourceIndex, resource := range v.spec.Resources {
		path := fmt.Sprintf("$.resources[%d]", resourceIndex)
		if resource == nil {
			v.report(CodeEmptyProject, path, "", "nil resource entry")
			continue
		}

		v.claimID(resource.ID, KindResource, path+".id")

		if strings.TrimSpace(resource.Label) == "" {
			v.report(CodeMissingLabel, path+".label", resource.ID, "resource label is required")
		}

		if !IsExportedGoIdent(resource.CodeName) {
			v.report(CodeInvalidCodeName, path+".code_name", resource.ID,
				"code_name %q is not an exported Go identifier", resource.CodeName)
		} else if owner, taken := resourceCodeNames[resource.CodeName]; taken {
			v.report(CodeDuplicateCode, path+".code_name", resource.ID,
				"code_name %q already used by resource %s", resource.CodeName, owner)
		} else {
			resourceCodeNames[resource.CodeName] = resource.ID
		}

		if !IsStorageName(resource.StorageName) {
			v.report(CodeInvalidStorage, path+".storage_name", resource.ID,
				"storage_name %q must be lower_snake_case", resource.StorageName)
		} else if owner, taken := resourceStorage[resource.StorageName]; taken {
			v.report(CodeDuplicateStore, path+".storage_name", resource.ID,
				"storage_name %q already used by resource %s", resource.StorageName, owner)
		} else {
			resourceStorage[resource.StorageName] = resource.ID
		}

		v.validateFields(resource, path)
		v.validateBehavior(resource, path)
	}
}

func (v *validator) validateFields(resource *Resource, resourcePath string) {
	// Code symbols and storage names must be unique within the resource's own
	// namespace; two resources may each legitimately have Email (ADR-001 §7).
	fieldCodeNames := map[string]ID{}
	fieldStorage := map[string]ID{}

	for fieldIndex, field := range resource.Fields {
		path := fmt.Sprintf("%s.fields[%d]", resourcePath, fieldIndex)
		if field == nil {
			v.report(CodeEmptyProject, path, resource.ID, "nil field entry")
			continue
		}

		v.claimID(field.ID, KindField, path+".id")

		if strings.TrimSpace(field.Label) == "" {
			v.report(CodeMissingLabel, path+".label", field.ID, "field label is required")
		}

		if !field.Type.Valid() {
			v.report(CodeUnknownType, path+".type", field.ID,
				"unsupported field type %q", field.Type)
		}

		if !IsExportedGoIdent(field.CodeName) {
			v.report(CodeInvalidCodeName, path+".code_name", field.ID,
				"code_name %q is not an exported Go identifier", field.CodeName)
		} else if owner, taken := fieldCodeNames[field.CodeName]; taken {
			v.report(CodeDuplicateCode, path+".code_name", field.ID,
				"code_name %q already used by field %s on this resource", field.CodeName, owner)
		} else {
			fieldCodeNames[field.CodeName] = field.ID
		}

		if !IsStorageName(field.StorageName) {
			v.report(CodeInvalidStorage, path+".storage_name", field.ID,
				"storage_name %q must be lower_snake_case", field.StorageName)
		} else if owner, taken := fieldStorage[field.StorageName]; taken {
			v.report(CodeDuplicateStore, path+".storage_name", field.ID,
				"storage_name %q already used by field %s on this resource", field.StorageName, owner)
		} else {
			fieldStorage[field.StorageName] = field.ID
		}

		v.validateFieldType(field, path)
	}
}

func (v *validator) validateFieldType(field *Field, path string) {
	switch field.Type {
	case TypeEnum:
		if len(field.EnumValues) == 0 {
			v.report(CodeInvalidEnum, path+".enum_values", field.ID,
				"enum field must declare at least one value")
		}
		seen := map[string]struct{}{}
		for valueIndex, enumValue := range field.EnumValues {
			valuePath := fmt.Sprintf("%s.enum_values[%d]", path, valueIndex)
			if strings.TrimSpace(enumValue.Value) == "" {
				v.report(CodeInvalidEnum, valuePath, field.ID, "enum value must not be empty")
				continue
			}
			if _, duplicate := seen[enumValue.Value]; duplicate {
				v.report(CodeInvalidEnum, valuePath, field.ID,
					"duplicate enum value %q", enumValue.Value)
				continue
			}
			seen[enumValue.Value] = struct{}{}
		}

	case TypeBelongsTo:
		if field.Target == "" {
			v.report(CodeDanglingRef, path+".target", field.ID,
				"belongs_to field must name a target resource")
			return
		}
		// A relationship pointing at a resource that no longer exists is the
		// canonical invalid state called out in ADR-001 §36.
		if v.spec.FindResource(field.Target) == nil {
			v.report(CodeDanglingRef, path+".target", field.ID,
				"belongs_to target %q does not exist", field.Target)
		}

	default:
		if len(field.EnumValues) > 0 {
			v.report(CodeInvalidEnum, path+".enum_values", field.ID,
				"enum_values is only valid on an enum field, not %q", field.Type)
		}
		if field.Target != "" {
			v.report(CodeDanglingRef, path+".target", field.ID,
				"target is only valid on a belongs_to field, not %q", field.Type)
		}
	}
}

// validateBehavior checks that every field list references a field that
// actually belongs to this resource.
func (v *validator) validateBehavior(resource *Resource, resourcePath string) {
	lists := []struct {
		name string
		ids  []ID
	}{
		{"list_fields", resource.Behavior.ListFields},
		{"searchable_fields", resource.Behavior.SearchableFields},
		{"sortable_fields", resource.Behavior.SortableFields},
		{"filterable_fields", resource.Behavior.FilterableFields},
	}

	for _, list := range lists {
		for index, fieldID := range list.ids {
			path := fmt.Sprintf("%s.behavior.%s[%d]", resourcePath, list.name, index)
			if resource.FindField(fieldID) == nil {
				v.report(CodeDanglingRef, path, fieldID,
					"%s references field %q which is not on this resource", list.name, fieldID)
			}
		}
	}
}

func (v *validator) validatePages() {
	slugs := map[string]ID{}

	for pageIndex, page := range v.spec.Pages {
		path := fmt.Sprintf("$.pages[%d]", pageIndex)
		if page == nil {
			v.report(CodeEmptyProject, path, "", "nil page entry")
			continue
		}

		v.claimID(page.ID, KindPage, path+".id")

		if strings.TrimSpace(page.Label) == "" {
			v.report(CodeMissingLabel, path+".label", page.ID, "page label is required")
		}
		if !IsSlug(page.Slug) {
			v.report(CodeInvalidSlug, path+".slug", page.ID,
				"page slug %q must be lower kebab-case", page.Slug)
		} else if owner, taken := slugs[page.Slug]; taken {
			v.report(CodeDuplicateSlug, path+".slug", page.ID,
				"page slug %q already used by page %s", page.Slug, owner)
		} else {
			slugs[page.Slug] = page.ID
		}

		switch page.Type {
		case PageResourceTable, PageResourceForm, PageResourceDetail:
			v.validateResourcePage(page, path)
		case PageDashboard:
			v.validateDashboardPage(page, path)
		default:
			v.report(CodeInvalidPage, path+".type", page.ID,
				"unsupported page type %q", page.Type)
		}
	}
}

func (v *validator) validateResourcePage(page *Page, path string) {
	if page.Resource == "" {
		v.report(CodeDanglingRef, path+".resource", page.ID,
			"%s page must reference a resource", page.Type)
		return
	}

	resource := v.spec.FindResource(page.Resource)
	if resource == nil {
		v.report(CodeDanglingRef, path+".resource", page.ID,
			"page references resource %q which does not exist", page.Resource)
		return
	}

	// Column and form field references must belong to the page's resource.
	if page.Table != nil {
		for index, fieldID := range page.Table.Columns {
			if resource.FindField(fieldID) == nil {
				v.report(CodeDanglingRef, fmt.Sprintf("%s.table.columns[%d]", path, index), fieldID,
					"column references field %q which is not on resource %s", fieldID, resource.CodeName)
			}
		}
		if page.Table.PageSize < 0 {
			v.report(CodeInvalidPage, path+".table.page_size", page.ID,
				"page_size must not be negative")
		}
	}
	if page.Form != nil {
		for index, fieldID := range page.Form.Fields {
			if resource.FindField(fieldID) == nil {
				v.report(CodeDanglingRef, fmt.Sprintf("%s.form.fields[%d]", path, index), fieldID,
					"form references field %q which is not on resource %s", fieldID, resource.CodeName)
			}
		}
		switch page.Form.Layout {
		case "", "single_column", "two_column", "section_groups":
		default:
			v.report(CodeInvalidPage, path+".form.layout", page.ID,
				"unsupported form layout %q", page.Form.Layout)
		}
	}
}

func (v *validator) validateDashboardPage(page *Page, path string) {
	if page.Resource != "" {
		v.report(CodeInvalidPage, path+".resource", page.ID,
			"dashboard page must not reference a resource")
	}
	if page.Dashboard == nil {
		return
	}

	cards := []struct {
		name  string
		cards []DashboardCard
	}{
		{"count_cards", page.Dashboard.CountCards},
		{"recent_lists", page.Dashboard.RecentLists},
	}
	for _, group := range cards {
		for index, card := range group.cards {
			cardPath := fmt.Sprintf("%s.dashboard.%s[%d]", path, group.name, index)
			if v.spec.FindResource(card.Resource) == nil {
				v.report(CodeDanglingRef, cardPath+".resource", page.ID,
					"dashboard card references resource %q which does not exist", card.Resource)
			}
			if strings.TrimSpace(card.Label) == "" {
				v.report(CodeMissingLabel, cardPath+".label", page.ID, "dashboard card label is required")
			}
		}
	}
}

func (v *validator) validateNavigation() {
	for index, item := range v.spec.Navigation {
		path := fmt.Sprintf("$.navigation[%d]", index)
		if item == nil {
			v.report(CodeEmptyProject, path, "", "nil navigation entry")
			continue
		}

		v.claimID(item.ID, KindNav, path+".id")

		if strings.TrimSpace(item.Label) == "" {
			v.report(CodeMissingLabel, path+".label", item.ID, "navigation label is required")
		}

		switch item.Target {
		case NavPage:
			if item.URL != "" {
				v.report(CodeInvalidNav, path+".url", item.ID,
					"page navigation entry must not set url")
			}
			if item.Page == "" {
				v.report(CodeDanglingRef, path+".page", item.ID,
					"page navigation entry must reference a page")
			} else if v.spec.FindPage(item.Page) == nil {
				v.report(CodeDanglingRef, path+".page", item.ID,
					"navigation references page %q which does not exist", item.Page)
			}

		case NavExternal:
			if item.Page != "" {
				v.report(CodeInvalidNav, path+".page", item.ID,
					"external navigation entry must not reference a page")
			}
			if !strings.HasPrefix(item.URL, "http://") && !strings.HasPrefix(item.URL, "https://") {
				v.report(CodeInvalidNav, path+".url", item.ID,
					"external navigation url %q must be http(s)", item.URL)
			}

		default:
			v.report(CodeInvalidNav, path+".target", item.ID,
				"unsupported navigation target %q", item.Target)
		}
	}
}
