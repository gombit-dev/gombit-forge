// Package graph builds the compiler's in-memory domain graph from a
// validated ProjectSpec (DESIGN.md §9 stage 2).
//
// The graph resolves every ID reference exactly once so later generation
// stages can walk typed pointers instead of re-scanning the spec. It is
// deterministic: every collection is ordered by the spec's authored order and
// never by map iteration, which is what lets generation be reproducible
// (DESIGN.md §9, §32).
package graph

import (
	"fmt"

	"github.com/gombit-dev/gombit-forge/internal/spec"
)

// Graph is the resolved domain model for one ProjectSpec.
type Graph struct {
	Spec *spec.ProjectSpec

	// Resources is in authored order.
	Resources []*Resource
	// Pages is in authored order.
	Pages []*Page
	// Navigation is in authored order.
	Navigation []*NavEntry

	byResourceID map[spec.ID]*Resource
	byPageID     map[spec.ID]*Page
	byFieldID    map[spec.ID]*Field
}

// Resource is a resolved resource with its fields and relationships.
type Resource struct {
	Spec *spec.Resource

	// Fields is in authored order and includes belongs_to fields.
	Fields []*Field
	// BelongsTo lists the outgoing relationships of this resource.
	BelongsTo []*Relationship
	// HasMany lists the inbound relationships pointing at this resource.
	//
	// These are derived from other resources' belongs_to fields and are never
	// authored directly (DESIGN.md §4.2).
	HasMany []*Relationship

	byFieldID map[spec.ID]*Field
}

// Field is a resolved field.
type Field struct {
	Spec  *spec.Field
	Owner *Resource

	// Relationship is set when the field is a belongs_to.
	Relationship *Relationship
}

// Relationship is one resolved belongs_to edge, viewed from both ends.
type Relationship struct {
	// Field is the belongs_to field carrying the reference.
	Field *Field
	// From is the resource declaring the belongs_to.
	From *Resource
	// To is the referenced resource.
	To *Resource
}

// Page is a resolved page.
type Page struct {
	Spec *spec.Page
	// Resource is nil for dashboard pages.
	Resource *Resource
	// Columns is the resolved table column order, empty for non-table pages.
	Columns []*Field
	// FormFields is the resolved form field order, empty for non-form pages.
	FormFields []*Field
}

// NavEntry is a resolved navigation entry.
type NavEntry struct {
	Spec *spec.NavItem
	// Page is nil for external entries.
	Page *Page
}

// Build resolves a spec into a domain graph.
//
// The spec must already be valid: Build validates first and refuses to
// construct a graph over a broken spec, so later stages never have to defend
// against dangling references.
func Build(s *spec.ProjectSpec) (*Graph, error) {
	if diagnostics := spec.Validate(s); diagnostics != nil {
		return nil, fmt.Errorf("graph: refusing to build over an invalid spec: %w", diagnostics)
	}

	g := &Graph{
		Spec:         s,
		byResourceID: make(map[spec.ID]*Resource, len(s.Resources)),
		byPageID:     make(map[spec.ID]*Page, len(s.Pages)),
		byFieldID:    map[spec.ID]*Field{},
	}

	g.buildResources()
	g.linkRelationships()
	g.buildPages()
	g.buildNavigation()

	return g, nil
}

func (g *Graph) buildResources() {
	for _, resourceSpec := range g.Spec.Resources {
		resource := &Resource{
			Spec:      resourceSpec,
			Fields:    make([]*Field, 0, len(resourceSpec.Fields)),
			byFieldID: make(map[spec.ID]*Field, len(resourceSpec.Fields)),
		}

		for _, fieldSpec := range resourceSpec.Fields {
			field := &Field{Spec: fieldSpec, Owner: resource}
			resource.Fields = append(resource.Fields, field)
			resource.byFieldID[fieldSpec.ID] = field
			g.byFieldID[fieldSpec.ID] = field
		}

		g.Resources = append(g.Resources, resource)
		g.byResourceID[resourceSpec.ID] = resource
	}
}

// linkRelationships wires belongs_to edges and derives the has_many inverse.
//
// Both sides are appended in resource-then-field authored order, so HasMany
// ordering is deterministic rather than dependent on map iteration.
func (g *Graph) linkRelationships() {
	for _, from := range g.Resources {
		for _, field := range from.Fields {
			if field.Spec.Type != spec.TypeBelongsTo {
				continue
			}

			to := g.byResourceID[field.Spec.Target]
			if to == nil {
				// Validation guarantees the target exists; skip defensively
				// rather than panicking if that ever regresses.
				continue
			}

			relationship := &Relationship{Field: field, From: from, To: to}
			field.Relationship = relationship
			from.BelongsTo = append(from.BelongsTo, relationship)
			to.HasMany = append(to.HasMany, relationship)
		}
	}
}

func (g *Graph) buildPages() {
	for _, pageSpec := range g.Spec.Pages {
		page := &Page{Spec: pageSpec}

		if pageSpec.Resource != "" {
			page.Resource = g.byResourceID[pageSpec.Resource]
		}

		if pageSpec.Table != nil && page.Resource != nil {
			for _, columnID := range pageSpec.Table.Columns {
				if field := page.Resource.byFieldID[columnID]; field != nil {
					page.Columns = append(page.Columns, field)
				}
			}
		}
		if pageSpec.Form != nil && page.Resource != nil {
			for _, fieldID := range pageSpec.Form.Fields {
				if field := page.Resource.byFieldID[fieldID]; field != nil {
					page.FormFields = append(page.FormFields, field)
				}
			}
		}

		g.Pages = append(g.Pages, page)
		g.byPageID[pageSpec.ID] = page
	}
}

func (g *Graph) buildNavigation() {
	for _, itemSpec := range g.Spec.Navigation {
		entry := &NavEntry{Spec: itemSpec}
		if itemSpec.Target == spec.NavPage {
			entry.Page = g.byPageID[itemSpec.Page]
		}
		g.Navigation = append(g.Navigation, entry)
	}
}

// Resource returns the resolved resource for an ID, or nil.
func (g *Graph) Resource(id spec.ID) *Resource { return g.byResourceID[id] }

// Page returns the resolved page for an ID, or nil.
func (g *Graph) Page(id spec.ID) *Page { return g.byPageID[id] }

// Field returns the resolved field for an ID, or nil.
func (g *Graph) Field(id spec.ID) *Field { return g.byFieldID[id] }

// ScalarFields returns the resource's non-relationship fields in authored order.
func (r *Resource) ScalarFields() []*Field {
	fields := make([]*Field, 0, len(r.Fields))
	for _, field := range r.Fields {
		if field.Spec.Type.Scalar() {
			fields = append(fields, field)
		}
	}
	return fields
}

// Field returns a field owned by this resource, or nil.
func (r *Resource) Field(id spec.ID) *Field { return r.byFieldID[id] }

// CodeName is the frozen Go symbol for the resource (ADR-001 D3).
func (r *Resource) CodeName() string { return r.Spec.CodeName }

// CodeName is the frozen Go symbol for the field (ADR-001 D3).
func (f *Field) CodeName() string { return f.Spec.CodeName }
