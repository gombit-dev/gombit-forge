package gen

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gombit-dev/gombit-forge/internal/compiler/graph"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

func frontendFiles(t *testing.T, g *graph.Graph) map[string]string {
	t.Helper()
	files, err := Frontend(g)
	if err != nil {
		t.Fatalf("Frontend: %v", err)
	}
	out := map[string]string{}
	for _, f := range files {
		out[f.Path] = string(f.Content)
	}
	return out
}

func TestFrontendFileLayout(t *testing.T) {
	files := frontendFiles(t, buildGraph2(t))

	want := []string{
		"frontend/src/forge_generated/customer/CustomerListPage.tsx",
		"frontend/src/forge_generated/customer/CustomerDetailPage.tsx",
		"frontend/src/forge_generated/customer/CustomerFormPage.tsx",
		"frontend/src/forge_generated/invoice/InvoiceListPage.tsx",
		"frontend/src/forge_generated/invoice/InvoiceDetailPage.tsx",
		"frontend/src/forge_generated/invoice/InvoiceFormPage.tsx",
		"frontend/src/forge_generated/resources.tsx",
	}
	for _, path := range want {
		if _, ok := files[path]; !ok {
			t.Errorf("missing generated file %s", path)
		}
	}
	if len(files) != len(want) {
		t.Errorf("file count: got %d want %d", len(files), len(want))
	}
}

func TestFrontendBannerOnEveryFile(t *testing.T) {
	for path, src := range frontendFiles(t, buildGraph2(t)) {
		if !strings.HasPrefix(src, tsBanner) {
			t.Errorf("%s missing the DO-NOT-EDIT banner", path)
		}
	}
}

// TestFrontendConsumesGeneratedClient is the D3 contract: pages use Gombit's
// generated OpenAPI client and types, not a hand-rolled one.
func TestFrontendConsumesGeneratedClient(t *testing.T) {
	files := frontendFiles(t, buildGraph2(t))
	list := files["frontend/src/forge_generated/customer/CustomerListPage.tsx"]
	form := files["frontend/src/forge_generated/customer/CustomerFormPage.tsx"]
	detail := files["frontend/src/forge_generated/customer/CustomerDetailPage.tsx"]

	listWants := []string{
		`from "../../api/client"`,
		`from "../../api/generated/client"`,
		`from "../../api/generated/schema"`,
		`paths["/api/v1/customers"]["get"]`,
		`await client.GET("/api/v1/customers")`,
	}
	for _, w := range listWants {
		if !strings.Contains(list, w) {
			t.Errorf("list page must contain %q", w)
		}
	}

	formWants := []string{
		`from "react-hook-form"`,
		`from "../../api/formErrors"`,
		`applyContractErrors(setError, err)`,
		`await client.POST("/api/v1/customers", { body: values })`,
		`await client.PUT("/api/v1/customers/{id}"`,
	}
	for _, w := range formWants {
		if !strings.Contains(form, w) {
			t.Errorf("form page must contain %q", w)
		}
	}

	if !strings.Contains(detail, `await client.GET("/api/v1/customers/{id}"`) {
		t.Error("detail page must GET the record by id")
	}
	// No hand-rolled fetch/axios anywhere.
	for path, src := range files {
		if strings.Contains(src, "fetch(") || strings.Contains(src, "axios") {
			t.Errorf("%s must not hand-roll HTTP; use the Gombit client", path)
		}
	}
}

// TestFrontendFieldInputs maps each field type to the right form control.
func TestFrontendFieldInputs(t *testing.T) {
	files := frontendFiles(t, buildGraph2(t))
	customer := files["frontend/src/forge_generated/customer/CustomerFormPage.tsx"]
	invoice := files["frontend/src/forge_generated/invoice/InvoiceFormPage.tsx"]

	checks := []struct {
		name, src, want string
	}{
		{"string is text input", customer, `<input type="text" {...register("contact_email"`},
		{"boolean is checkbox", customer, `<input type="checkbox" {...register("active")`},
		{"enum is a select", customer, `<select {...register("tier")`},
		{"enum option free", customer, `<option value="free">free</option>`},
		{"datetime input", customer, `<input type="datetime-local" {...register("joined_at")`},
		{"belongs_to FK is number", invoice, `<input type="number" {...register("customer_id"`},
		{"decimal is text", invoice, `<input type="text" {...register("total"`},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(c.src, c.want) {
				t.Errorf("missing %q", c.want)
			}
		})
	}
}

// TestFrontendFormValueTypes checks the TS form value type maps field types to
// string/number/boolean.
func TestFrontendFormValueTypes(t *testing.T) {
	files := frontendFiles(t, buildGraph2(t))
	invoice := files["frontend/src/forge_generated/invoice/InvoiceFormPage.tsx"]

	for _, want := range []string{
		"customer_id: number;", // belongs_to FK
		"total: string;",       // decimal
	} {
		if !strings.Contains(invoice, want) {
			t.Errorf("form values type must contain %q", want)
		}
	}
}

// TestFrontendRegistryRoutes checks resources.tsx registers the four routes per
// resource and imports the page components.
func TestFrontendRegistryRoutes(t *testing.T) {
	registry := frontendFiles(t, buildGraph2(t))["frontend/src/forge_generated/resources.tsx"]

	for _, want := range []string{
		`import { CustomerListPage } from "./customer/CustomerListPage"`,
		`import { CustomerFormPage } from "./customer/CustomerFormPage"`,
		`{ path: "customers", element: <CustomerListPage /> }`,
		`{ path: "customers/new", element: <CustomerFormPage /> }`,
		`{ path: "customers/:id", element: <CustomerDetailPage /> }`,
		`{ path: "customers/:id/edit", element: <CustomerFormPage /> }`,
		`export const generatedResourceRoutes: RouteObject[]`,
		`export const generatedResources: GeneratedResource[]`,
	} {
		if !strings.Contains(registry, want) {
			t.Errorf("registry must contain %q", want)
		}
	}
}

// TestFrontendPathUsesStorageName checks routes and API paths are kebab-cased
// storage names, exercised with a multi-word resource.
func TestFrontendPathUsesStorageName(t *testing.T) {
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

	list := frontendFiles(t, g)["frontend/src/forge_generated/orderline/OrderLineListPage.tsx"]
	if !strings.Contains(list, `client.GET("/api/v1/order-lines")`) {
		t.Error("API path should be the kebab-cased storage name")
	}
	registry := frontendFiles(t, g)["frontend/src/forge_generated/resources.tsx"]
	if !strings.Contains(registry, `{ path: "order-lines", element: <OrderLineListPage /> }`) {
		t.Error("route should be the kebab-cased storage name")
	}
}

func TestFrontendIsDeterministic(t *testing.T) {
	g := buildGraph2(t)
	first, err := Frontend(g)
	if err != nil {
		t.Fatalf("Frontend: %v", err)
	}
	for i := 0; i < 20; i++ {
		next, err := Frontend(g)
		if err != nil {
			t.Fatalf("Frontend: %v", err)
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

func TestFrontendNilGraph(t *testing.T) {
	if _, err := Frontend(nil); err == nil {
		t.Fatal("expected an error for a nil graph")
	}
}

func TestFrontendRejectsReservedResourceName(t *testing.T) {
	g := oneResourceGraph(t, "Main", "mains", field("Name", "name", spec.TypeString))
	if _, err := Frontend(g); err == nil {
		t.Fatal("Frontend must reject a resource folding to package main")
	}
}
