package gen

import (
	"strings"
	"testing"

	"github.com/gombit-dev/gombit-forge/internal/compiler/graph"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

// TestFrontendTableFiltersConstructorStorageName: a filterable field whose
// storage name is "constructor" must be wired like any other filter, and the
// filters map must stay prototype-free. A plain {} reads
// Object.prototype.constructor as the filter value, so the table sends the
// function's source on first load and comes up empty (#305).
func TestFrontendTableFiltersConstructorStorageName(t *testing.T) {
	id := func(k spec.Kind) spec.ID { return spec.MustNewID(k) }
	res := id(spec.KindResource)
	ctor := id(spec.KindField)
	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: id(spec.KindProject), Name: "Acme", Slug: "acme"},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{
				ID: res, Label: "Widget", CodeName: "Widget", StorageName: "widgets",
				Behavior: spec.ResourceBehavior{FilterableFields: []spec.ID{ctor}},
				Fields: []*spec.Field{
					{ID: ctor, Label: "Constructor", Type: spec.TypeString, CodeName: "Constructor", StorageName: "constructor"},
				},
			},
		},
		Pages: []*spec.Page{
			{ID: id(spec.KindPage), Slug: "widgets", Label: "Widgets", Type: spec.PageResourceTable, Resource: res,
				Table: &spec.TableConfig{Filters: []spec.ID{ctor}}},
		},
	}
	if d := spec.Validate(s); d != nil {
		t.Fatalf("fixture invalid: %s", d.Error())
	}
	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	table := frontendFiles(t, g)["frontend/src/forge_generated/widget/WidgetsTablePage.tsx"]
	if table == "" {
		t.Fatal("the widgets table page must be generated")
	}
	for _, want := range []string{
		// The filters map has no prototype, at init and after every update.
		`const [filters, setFilters] = useState<Record<string, string>>(Object.create(null));`,
		`setFilters((prev) => Object.assign(Object.create(null), prev, { [col]: value }));`,
		// The "constructor" field is wired like any other filter.
		`onChange={(e) => setFilter("constructor", e.target.value)}`,
		`value={filters["constructor"] ?? ""}`,
		`"constructor": filters["constructor"] || undefined`,
		`}, [client, page, filters]);`,
	} {
		if !strings.Contains(table, want) {
			t.Errorf("table with a constructor filter must contain %q", want)
		}
	}
	// A plain object must not come back: {} reads Object.prototype.constructor
	// as the filter, and a spread update rebuilds a plain object.
	for _, unwanted := range []string{
		`useState<Record<string, string>>({})`,
		`({ ...prev, [col]: value })`,
	} {
		if strings.Contains(table, unwanted) {
			t.Errorf("table must not contain a plain-object filters map: %q", unwanted)
		}
	}
}
