package gen

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gombit-dev/gombit-forge/internal/compiler/graph"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

// adminGraph builds the standard fixture with the given admin visibility and
// CRUD toggles applied to every resource.
func adminGraph(t *testing.T, visible, create, update, del bool) *graph.Graph {
	t.Helper()
	g, _ := buildGraph(t)
	for _, resource := range g.Resources {
		resource.Spec.Behavior.AdminVisible = visible
		resource.Spec.Behavior.CreateEnabled = create
		resource.Spec.Behavior.UpdateEnabled = update
		resource.Spec.Behavior.DeleteEnabled = del
	}
	return g
}

func adminSource(t *testing.T, files []File, pkg string) (string, bool) {
	t.Helper()
	want := GeneratedRoot + "/" + pkg + "/admin.go"
	for _, f := range files {
		if f.Path == want {
			return string(f.Content), true
		}
	}
	return "", false
}

// TestAdminOnlyForVisibleResources is the visibility contract: a file is
// emitted only for admin-visible resources.
func TestAdminOnlyForVisibleResources(t *testing.T) {
	g, ids := buildGraph(t)
	g.Resource(ids["customer"]).Spec.Behavior.AdminVisible = true
	g.Resource(ids["invoice"]).Spec.Behavior.AdminVisible = false

	files, err := Admin(g)
	if err != nil {
		t.Fatalf("Admin: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected exactly one admin file, got %v", paths(files))
	}
	if _, ok := adminSource(t, files, "customer"); !ok {
		t.Error("visible customer should have admin.go")
	}
	if _, ok := adminSource(t, files, "invoice"); ok {
		t.Error("invisible invoice must not have admin.go")
	}
}

func TestAdminUsesPublicRegister(t *testing.T) {
	files, err := Admin(adminGraph(t, true, true, true, true))
	if err != nil {
		t.Fatalf("Admin: %v", err)
	}
	src, ok := adminSource(t, files, "customer")
	if !ok {
		t.Fatal("missing customer admin.go")
	}

	if !strings.HasPrefix(src, Banner) {
		t.Error("admin.go must start with the banner")
	}
	for _, want := range []string{
		"func RegisterAdmin(app *framework.App) error",
		"admin.Register(app, Customer{}, admin.Options{",
		`"github.com/gombit-dev/gombit/admin"`,
		`"github.com/gombit-dev/gombit/framework"`,
		`Slug:     "customers"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("admin.go must contain %q", want)
		}
	}
	// No Forge-specific admin machinery.
	if strings.Contains(src, "func (") {
		t.Error("admin.go should only declare RegisterAdmin, no methods of its own")
	}
}

// TestAdminActionsFollowToggles maps admin create/update/delete to the CRUD
// toggles, with list and detail always enabled.
func TestAdminActionsFollowToggles(t *testing.T) {
	cases := []struct{ create, update, del bool }{
		{false, false, false},
		{true, false, false},
		{false, true, false},
		{false, false, true},
		{true, true, true},
	}
	for _, tc := range cases {
		g := adminGraph(t, true, tc.create, tc.update, tc.del)
		files, err := Admin(g)
		if err != nil {
			t.Fatalf("Admin: %v", err)
		}
		src, _ := adminSource(t, files, "customer")

		assertContains(t, src, "List:   true", true)
		assertContains(t, src, "Detail: true", true)
		assertContains(t, src, "Create: true", tc.create)
		assertContains(t, src, "Update: true", tc.update)
		assertContains(t, src, "Delete: true", tc.del)
	}
}

func assertContains(t *testing.T, haystack, needle string, want bool) {
	t.Helper()
	if got := strings.Contains(haystack, needle); got != want {
		t.Errorf("contains(%q)=%v, want %v", needle, got, want)
	}
}

// TestAdminPluralOmittedWhenRedundant checks the plural label is emitted only
// when it adds information over the singular.
func TestAdminPluralOmittedWhenRedundant(t *testing.T) {
	g, ids := buildGraph(t)
	cust := g.Resource(ids["customer"]).Spec
	cust.Behavior.AdminVisible = true
	cust.LabelPlural = "Customers"

	inv := g.Resource(ids["invoice"]).Spec
	inv.Behavior.AdminVisible = true
	inv.LabelPlural = "" // no plural -> let Gombit derive

	files, _ := Admin(g)

	custSrc, _ := adminSource(t, files, "customer")
	if !strings.Contains(custSrc, `Plural:   "Customers"`) {
		t.Error("a distinct plural label should be emitted")
	}
	invSrc, _ := adminSource(t, files, "invoice")
	if strings.Contains(invSrc, "Plural:") {
		t.Error("an empty plural should be omitted so Gombit derives it")
	}
}

func TestAdminSlugIsStorageName(t *testing.T) {
	id := func(k spec.Kind) spec.ID { return spec.MustNewID(k) }
	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: id(spec.KindProject), Name: "Acme", Slug: "acme"},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{
				ID: id(spec.KindResource), Label: "Order line", CodeName: "OrderLine",
				StorageName: "order_lines",
				Behavior:    spec.ResourceBehavior{AdminVisible: true},
				Fields: []*spec.Field{
					{ID: id(spec.KindField), Label: "Qty", Type: spec.TypeInteger, CodeName: "Qty", StorageName: "qty"},
				},
			},
		},
	}
	if d := spec.Validate(s); d != nil {
		t.Fatalf("fixture invalid: %s", d.Error())
	}
	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	files, _ := Admin(g)
	src, _ := adminSource(t, files, "orderline")
	// storage_name is a valid admin slug (lower, may contain underscores).
	if !strings.Contains(src, `Slug:     "order_lines"`) {
		t.Errorf("slug should be the storage name order_lines:\n%s", src)
	}
}

func TestAdminIsDeterministic(t *testing.T) {
	g := adminGraph(t, true, true, true, true)
	first, err := Admin(g)
	if err != nil {
		t.Fatalf("Admin: %v", err)
	}
	for i := 0; i < 20; i++ {
		next, err := Admin(g)
		if err != nil {
			t.Fatalf("Admin: %v", err)
		}
		if len(next) != len(first) {
			t.Fatalf("file count changed")
		}
		for j := range first {
			if first[j].Path != next[j].Path || !bytes.Equal(first[j].Content, next[j].Content) {
				t.Fatalf("run %d differs at %s", i, first[j].Path)
			}
		}
	}
}

func TestAdminNilGraph(t *testing.T) {
	if _, err := Admin(nil); err == nil {
		t.Fatal("expected an error for a nil graph")
	}
}

func TestAdminRejectsReservedResourceName(t *testing.T) {
	g := oneResourceGraph(t, "Register", "things", field("Name", "name", spec.TypeString))
	g.Resources[0].Spec.Behavior.AdminVisible = true
	if _, err := Admin(g); err == nil {
		t.Fatal("Admin must reject a resource whose code symbol is a reserved package symbol")
	}
}
