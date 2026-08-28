package graph_test

import (
	"strings"
	"testing"

	"github.com/gombit-dev/gombit-forge/internal/compiler/graph"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

// buildSpec assembles a two-resource spec equivalent to the M0 target, using
// minted IDs so the graph is exercised against realistic identifiers.
func buildSpec(t *testing.T) (*spec.ProjectSpec, map[string]spec.ID) {
	t.Helper()

	ids := map[string]spec.ID{
		"project":  spec.MustNewID(spec.KindProject),
		"customer": spec.MustNewID(spec.KindResource),
		"invoice":  spec.MustNewID(spec.KindResource),
		"cName":    spec.MustNewID(spec.KindField),
		"cEmail":   spec.MustNewID(spec.KindField),
		"iCust":    spec.MustNewID(spec.KindField),
		"iTotal":   spec.MustNewID(spec.KindField),
		"pageCust": spec.MustNewID(spec.KindPage),
		"pageDash": spec.MustNewID(spec.KindPage),
		"navCust":  spec.MustNewID(spec.KindNav),
		"navDocs":  spec.MustNewID(spec.KindNav),
	}

	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: ids["project"], Name: "Portal", Slug: "portal"},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{
				ID: ids["customer"], Label: "Customer",
				CodeName: "Customer", StorageName: "customers",
				Fields: []*spec.Field{
					{
						ID: ids["cName"], Label: "Name", Type: spec.TypeString,
						CodeName: "Name", StorageName: "name", Required: true,
					},
					{
						ID: ids["cEmail"], Label: "Email", Type: spec.TypeString,
						CodeName: "Email", StorageName: "email",
					},
				},
			},
			{
				ID: ids["invoice"], Label: "Invoice",
				CodeName: "Invoice", StorageName: "invoices",
				Fields: []*spec.Field{
					{
						ID: ids["iCust"], Label: "Customer", Type: spec.TypeBelongsTo,
						CodeName: "Customer", StorageName: "customer_id",
						Target: ids["customer"], InverseLabel: "Invoices",
					},
					{
						ID: ids["iTotal"], Label: "Total", Type: spec.TypeDecimal,
						CodeName: "Total", StorageName: "total",
					},
				},
			},
		},
		Pages: []*spec.Page{
			{
				ID: ids["pageCust"], Slug: "customers", Label: "Customers",
				Type: spec.PageResourceTable, Resource: ids["customer"],
				Table: &spec.TableConfig{Columns: []spec.ID{ids["cEmail"], ids["cName"]}},
			},
			{
				ID: ids["pageDash"], Slug: "dashboard", Label: "Dashboard",
				Type: spec.PageDashboard,
			},
		},
		Navigation: []*spec.NavItem{
			{ID: ids["navCust"], Label: "Customers", Target: spec.NavPage, Page: ids["pageCust"]},
			{ID: ids["navDocs"], Label: "Docs", Target: spec.NavExternal, URL: "https://example.com"},
		},
	}

	if diagnostics := spec.Validate(s); diagnostics != nil {
		t.Fatalf("fixture spec is invalid:\n%s", diagnostics.Error())
	}
	return s, ids
}

// TestBuildDerivesHasManyFromBelongsTo is the relationship rule from
// DESIGN.md §4.2: has_many is never authored, only derived.
func TestBuildDerivesHasManyFromBelongsTo(t *testing.T) {
	s, ids := buildSpec(t)

	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	invoice := g.Resource(ids["invoice"])
	customer := g.Resource(ids["customer"])
	if invoice == nil || customer == nil {
		t.Fatal("resources not resolved")
	}

	if len(invoice.BelongsTo) != 1 {
		t.Fatalf("invoice.BelongsTo: got %d want 1", len(invoice.BelongsTo))
	}
	if got := invoice.BelongsTo[0].To; got != customer {
		t.Errorf("belongs_to points at %v, want customer", got)
	}

	// The inverse edge exists on the target without being authored there.
	if len(customer.HasMany) != 1 {
		t.Fatalf("customer.HasMany: got %d want 1", len(customer.HasMany))
	}
	inverse := customer.HasMany[0]
	if inverse.From != invoice || inverse.To != customer {
		t.Errorf("inverse edge wired wrong: from=%v to=%v", inverse.From, inverse.To)
	}
	if inverse.Field.Spec.ID != ids["iCust"] {
		t.Errorf("inverse edge carries wrong field: %s", inverse.Field.Spec.ID)
	}

	// Customer declares no relationships of its own.
	if len(customer.BelongsTo) != 0 {
		t.Errorf("customer.BelongsTo: got %d want 0", len(customer.BelongsTo))
	}
}

func TestBuildResolvesFieldAndPageReferences(t *testing.T) {
	s, ids := buildSpec(t)

	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	page := g.Page(ids["pageCust"])
	if page == nil {
		t.Fatal("page not resolved")
	}
	if page.Resource != g.Resource(ids["customer"]) {
		t.Error("page resource not resolved")
	}

	// Column order follows the page's authored order, not field order.
	wantColumns := []spec.ID{ids["cEmail"], ids["cName"]}
	if len(page.Columns) != len(wantColumns) {
		t.Fatalf("columns: got %d want %d", len(page.Columns), len(wantColumns))
	}
	for i, want := range wantColumns {
		if page.Columns[i].Spec.ID != want {
			t.Errorf("column %d: got %s want %s", i, page.Columns[i].Spec.ID, want)
		}
	}

	dashboard := g.Page(ids["pageDash"])
	if dashboard == nil || dashboard.Resource != nil {
		t.Error("dashboard page should resolve with no resource")
	}
}

func TestBuildResolvesNavigation(t *testing.T) {
	s, ids := buildSpec(t)

	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if len(g.Navigation) != 2 {
		t.Fatalf("navigation: got %d want 2", len(g.Navigation))
	}
	if g.Navigation[0].Page != g.Page(ids["pageCust"]) {
		t.Error("page nav entry not resolved")
	}
	if g.Navigation[1].Page != nil {
		t.Error("external nav entry must not resolve a page")
	}
}

// TestBuildIsDeterministic guards the property that makes generation
// reproducible: repeated builds must produce identical orderings.
func TestBuildIsDeterministic(t *testing.T) {
	s, _ := buildSpec(t)

	fingerprint := func() string {
		g, err := graph.Build(s)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		var out strings.Builder
		for _, resource := range g.Resources {
			out.WriteString(resource.CodeName())
			for _, field := range resource.Fields {
				out.WriteString("|f:" + field.CodeName())
			}
			for _, rel := range resource.HasMany {
				out.WriteString("|hm:" + rel.From.CodeName() + "." + rel.Field.CodeName())
			}
			for _, rel := range resource.BelongsTo {
				out.WriteString("|bt:" + rel.To.CodeName())
			}
			out.WriteString(";")
		}
		return out.String()
	}

	first := fingerprint()
	for range 50 {
		if got := fingerprint(); got != first {
			t.Fatalf("graph ordering unstable:\nfirst: %s\ngot:   %s", first, got)
		}
	}
}

func TestScalarFieldsExcludesRelationships(t *testing.T) {
	s, ids := buildSpec(t)

	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	scalars := g.Resource(ids["invoice"]).ScalarFields()
	if len(scalars) != 1 {
		t.Fatalf("scalar fields: got %d want 1", len(scalars))
	}
	if scalars[0].Spec.ID != ids["iTotal"] {
		t.Errorf("got %s, want the decimal field", scalars[0].Spec.ID)
	}
}

// TestBuildRefusesInvalidSpec keeps later generation stages free of
// defensive nil checks: a broken spec never becomes a graph.
func TestBuildRefusesInvalidSpec(t *testing.T) {
	s, _ := buildSpec(t)
	s.Resources[1].Fields[0].Target = spec.MustNewID(spec.KindResource) // dangling

	if _, err := graph.Build(s); err == nil {
		t.Fatal("expected build to refuse an invalid spec")
	}
}

// TestEveryBelongsToHasARelationship is the invariant generators rely on when
// they dereference Field.Relationship without a nil check.
func TestEveryBelongsToHasARelationship(t *testing.T) {
	s, _ := buildSpec(t)

	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	for _, resource := range g.Resources {
		for _, field := range resource.Fields {
			isBelongsTo := field.Spec.Type == spec.TypeBelongsTo
			hasRelationship := field.Relationship != nil
			if isBelongsTo != hasRelationship {
				t.Errorf("field %s (%s): belongs_to=%v but relationship=%v",
					field.CodeName(), field.Spec.Type, isBelongsTo, hasRelationship)
			}
		}
	}
}

// TestPageViewsAreKeyedOffType is the contract the Page doc comments state:
// each page type populates exactly one view and leaves the others empty, so a
// generator can switch on Type alone.
func TestPageViewsAreKeyedOffType(t *testing.T) {
	s, ids := buildSpec(t)

	// Give the table page a form page sibling so every type is represented.
	formPageID := spec.MustNewID(spec.KindPage)
	detailPageID := spec.MustNewID(spec.KindPage)
	s.Pages = append(s.Pages,
		&spec.Page{
			ID: formPageID, Slug: "customer-form", Label: "Customer form",
			Type: spec.PageResourceForm, Resource: ids["customer"],
			Form: &spec.FormConfig{Fields: []spec.ID{ids["cName"], ids["cEmail"]}},
		},
		&spec.Page{
			ID: detailPageID, Slug: "customer-detail", Label: "Customer detail",
			Type: spec.PageResourceDetail, Resource: ids["customer"],
		},
	)
	s.Pages[1].Dashboard = &spec.DashboardConfig{
		CountCards: []spec.DashboardCard{{Label: "Customers", Resource: ids["customer"]}},
	}

	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	tests := []struct {
		name                             string
		page                             *graph.Page
		wantColumns, wantForm, wantCards bool
	}{
		{"resource_table", g.Page(ids["pageCust"]), true, false, false},
		{"resource_form", g.Page(formPageID), false, true, false},
		{"resource_detail", g.Page(detailPageID), false, false, false},
		{"dashboard", g.Page(ids["pageDash"]), false, false, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := len(test.page.Columns) > 0; got != test.wantColumns {
				t.Errorf("Columns populated=%v, want %v", got, test.wantColumns)
			}
			if got := len(test.page.FormFields) > 0; got != test.wantForm {
				t.Errorf("FormFields populated=%v, want %v", got, test.wantForm)
			}
			if got := len(test.page.CountCards) > 0; got != test.wantCards {
				t.Errorf("CountCards populated=%v, want %v", got, test.wantCards)
			}
		})
	}
}

// TestDashboardPageProjectsOnlyCards pins that a dashboard yields resolved
// cards and no table/form views.
func TestDashboardPageProjectsOnlyCards(t *testing.T) {
	s, ids := buildSpec(t)
	s.Pages[1].Dashboard = &spec.DashboardConfig{
		CountCards:  []spec.DashboardCard{{Label: "Customers", Resource: ids["customer"]}},
		RecentLists: []spec.DashboardCard{{Label: "Recent", Resource: ids["invoice"], Limit: 5}},
	}

	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	dashboard := g.Page(ids["pageDash"])
	if len(dashboard.Columns) != 0 || len(dashboard.FormFields) != 0 {
		t.Error("dashboard page must project neither columns nor form fields")
	}

	// Cards are resolved to resource pointers, not left as raw IDs.
	if len(dashboard.CountCards) != 1 {
		t.Fatalf("count cards: got %d want 1", len(dashboard.CountCards))
	}
	if dashboard.CountCards[0].Resource != g.Resource(ids["customer"]) {
		t.Error("count card resource not resolved to a pointer")
	}
	if len(dashboard.RecentLists) != 1 {
		t.Fatalf("recent lists: got %d want 1", len(dashboard.RecentLists))
	}
	if dashboard.RecentLists[0].Resource != g.Resource(ids["invoice"]) {
		t.Error("recent list resource not resolved to a pointer")
	}

	// A table page must not gain card views.
	if len(g.Page(ids["pageCust"]).CountCards) != 0 {
		t.Error("a resource_table page must not project dashboard cards")
	}
}

// TestBehaviorFieldListsAreResolved covers the other half of "resolves every
// ID reference exactly once": behavior lists become pointers.
func TestBehaviorFieldListsAreResolved(t *testing.T) {
	s, ids := buildSpec(t)
	s.Resources[0].Behavior = spec.ResourceBehavior{
		CreateEnabled:    true,
		ListFields:       []spec.ID{ids["cEmail"], ids["cName"]},
		SearchableFields: []spec.ID{ids["cEmail"]},
		SortableFields:   []spec.ID{ids["cName"]},
		FilterableFields: []spec.ID{ids["cEmail"]},
	}

	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	customer := g.Resource(ids["customer"])
	behavior := customer.Behavior

	// Order follows the behavior list, not field declaration order.
	if len(behavior.List) != 2 ||
		behavior.List[0].Spec.ID != ids["cEmail"] ||
		behavior.List[1].Spec.ID != ids["cName"] {
		t.Errorf("list fields not resolved in order: %+v", behavior.List)
	}
	if len(behavior.Searchable) != 1 || behavior.Searchable[0].Spec.ID != ids["cEmail"] {
		t.Errorf("searchable fields not resolved: %+v", behavior.Searchable)
	}
	if len(behavior.Sortable) != 1 || behavior.Sortable[0].Spec.ID != ids["cName"] {
		t.Errorf("sortable fields not resolved: %+v", behavior.Sortable)
	}
	if len(behavior.Filterable) != 1 || behavior.Filterable[0].Spec.ID != ids["cEmail"] {
		t.Errorf("filterable fields not resolved: %+v", behavior.Filterable)
	}

	// Resolved pointers must be the graph's own field objects, not copies.
	if behavior.List[0] != customer.Field(ids["cEmail"]) {
		t.Error("behavior field is not the graph's field instance")
	}
	if behavior.Spec != &s.Resources[0].Behavior {
		t.Error("behavior spec pointer not carried through")
	}
}
