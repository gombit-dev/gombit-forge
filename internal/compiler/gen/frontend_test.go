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
		// Detail/form are resource-driven; the list is page-driven (named by the
		// resource_table page's slug), so there is no CustomerListPage.
		"frontend/src/forge_generated/customer/CustomerDetailPage.tsx",
		"frontend/src/forge_generated/customer/CustomerFormPage.tsx",
		"frontend/src/forge_generated/customer/CustomersTablePage.tsx",
		"frontend/src/forge_generated/invoice/InvoiceDetailPage.tsx",
		"frontend/src/forge_generated/invoice/InvoiceFormPage.tsx",
		"frontend/src/forge_generated/invoice/InvoicesTablePage.tsx",
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
	table := files["frontend/src/forge_generated/customer/CustomersTablePage.tsx"]
	form := files["frontend/src/forge_generated/customer/CustomerFormPage.tsx"]
	detail := files["frontend/src/forge_generated/customer/CustomerDetailPage.tsx"]

	tableWants := []string{
		`from "../../api/client"`,
		`from "../../api/generated/client"`,
		`from "../../api/generated/schema"`,
		`paths["/api/v1/customers"]["get"]`,
		`await client.GET("/api/v1/customers")`,
	}
	for _, w := range tableWants {
		if !strings.Contains(table, w) {
			t.Errorf("table page must contain %q", w)
		}
	}

	formWants := []string{
		`from "react-hook-form"`,
		`from "../../api/formErrors"`,
		`applyContractErrors(setError, err)`,
		`await client.POST("/api/v1/customers", { body })`,
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
		{"enum option free", customer, `<option value="free">{ "free" }</option>`},
		{"datetime is text (RFC3339 round-trips)", customer, `<input type="text" placeholder="YYYY-MM-DDTHH:MM:SSZ" {...register("joined_at")`},
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
		`import { CustomerDetailPage } from "./customer/CustomerDetailPage"`,
		`import { CustomerFormPage } from "./customer/CustomerFormPage"`,
		`import { CustomersTablePage } from "./customer/CustomersTablePage"`,
		`{ path: "customers", element: <CustomersTablePage /> }`,
		`{ path: "customers/new", element: <CustomerFormPage /> }`,
		`{ path: "customers/:id", element: <CustomerDetailPage /> }`,
		`{ path: "customers/:id/edit", element: <CustomerFormPage /> }`,
		`{ slug: "customers", title: "Customers", listPath: "/customers" }`,
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
	resID := id(spec.KindResource)
	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: id(spec.KindProject), Name: "Acme", Slug: "acme"},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{
				ID: resID, Label: "Order line", CodeName: "OrderLine",
				StorageName: "order_lines",
				Fields: []*spec.Field{
					{ID: id(spec.KindField), Label: "Qty", Type: spec.TypeInteger, CodeName: "Qty", StorageName: "qty"},
				},
			},
		},
		Pages: []*spec.Page{
			{ID: id(spec.KindPage), Slug: "order-lines", Label: "Order lines", Type: spec.PageResourceTable, Resource: resID},
		},
	}
	if d := spec.Validate(s); d != nil {
		t.Fatalf("fixture invalid: %s", d.Error())
	}
	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// The API collection path comes from the resource storage name; the table's
	// own route comes from the page slug (here the same kebab string).
	table := frontendFiles(t, g)["frontend/src/forge_generated/orderline/OrderLinesTablePage.tsx"]
	if !strings.Contains(table, `client.GET("/api/v1/order-lines")`) {
		t.Error("API path should be the kebab-cased storage name")
	}
	registry := frontendFiles(t, g)["frontend/src/forge_generated/resources.tsx"]
	if !strings.Contains(registry, `{ path: "order-lines", element: <OrderLinesTablePage /> }`) {
		t.Error("route should be the page slug")
	}
}

// TestFrontendHonorsToggles is the contract the handlers already enforce: the
// UI's write operations match the routes. It uses partial toggles so an
// always-full-CRUD implementation fails.
func TestFrontendHonorsToggles(t *testing.T) {
	t.Run("create only", func(t *testing.T) {
		g := toggledFrontendGraph(t, true, false, false)
		files := frontendFiles(t, g)
		form := files["frontend/src/forge_generated/customer/CustomerFormPage.tsx"]
		list := files["frontend/src/forge_generated/customer/CustomersTablePage.tsx"]
		detail := files["frontend/src/forge_generated/customer/CustomerDetailPage.tsx"]
		registry := files["frontend/src/forge_generated/resources.tsx"]

		if !strings.Contains(form, `client.POST(`) {
			t.Error("create-only form must POST")
		}
		if strings.Contains(form, `client.PUT(`) {
			t.Error("create-only form must not PUT")
		}
		if strings.Contains(form, "useEffect") {
			t.Error("create-only form must not load a record")
		}
		if !strings.Contains(list, `to="/customers/new"`) {
			t.Error("create-only table must show a New link")
		}
		if strings.Contains(detail, "/edit") {
			t.Error("create-only detail must not show an Edit link")
		}
		if !strings.Contains(registry, `{ path: "customers/new"`) {
			t.Error("registry must route /new when create is on")
		}
		if strings.Contains(registry, `/:id/edit`) {
			t.Error("registry must not route /:id/edit when update is off")
		}
	})

	t.Run("update only", func(t *testing.T) {
		g := toggledFrontendGraph(t, false, true, false)
		files := frontendFiles(t, g)
		form := files["frontend/src/forge_generated/customer/CustomerFormPage.tsx"]
		list := files["frontend/src/forge_generated/customer/CustomersTablePage.tsx"]
		registry := files["frontend/src/forge_generated/resources.tsx"]

		if !strings.Contains(form, `client.PUT(`) {
			t.Error("update-only form must PUT")
		}
		if strings.Contains(form, `client.POST(`) {
			t.Error("update-only form must not POST")
		}
		if !strings.Contains(form, "const editing = true;") {
			t.Error("update-only form is always editing")
		}
		if strings.Contains(list, `/customers/new`) {
			t.Error("update-only table must not show a New link")
		}
		if strings.Contains(registry, `{ path: "customers/new"`) {
			t.Error("registry must not route /new when create is off")
		}
		if !strings.Contains(registry, `{ path: "customers/:id/edit"`) {
			t.Error("registry must route /:id/edit when update is on")
		}
	})

	t.Run("read only", func(t *testing.T) {
		g := toggledFrontendGraph(t, false, false, false)
		files := frontendFiles(t, g)

		if _, ok := files["frontend/src/forge_generated/customer/CustomerFormPage.tsx"]; ok {
			t.Error("a read-only resource must not get a form page")
		}
		registry := files["frontend/src/forge_generated/resources.tsx"]
		if strings.Contains(registry, "FormPage") {
			t.Error("registry must not import or route a form page for a read-only resource")
		}
		// The table (page-driven, independent of toggles) and detail still exist.
		for _, p := range []string{"CustomersTablePage.tsx", "CustomerDetailPage.tsx"} {
			if _, ok := files["frontend/src/forge_generated/customer/"+p]; !ok {
				t.Errorf("read-only resource still needs %s", p)
			}
		}
		// A read-only table shows no New link.
		if strings.Contains(files["frontend/src/forge_generated/customer/CustomersTablePage.tsx"], `/customers/new`) {
			t.Error("read-only table must not show a New link")
		}
	})
}

// toggledFrontendGraph applies the given toggles to every resource.
func toggledFrontendGraph(t *testing.T, create, update, del bool) *graph.Graph {
	t.Helper()
	g, _ := buildGraph(t)
	for _, resource := range g.Resources {
		resource.Spec.Behavior.CreateEnabled = create
		resource.Spec.Behavior.UpdateEnabled = update
		resource.Spec.Behavior.DeleteEnabled = del
	}
	return g
}

// TestFrontendDateFieldsRoundTrip guards the RFC 3339 representation: date and
// datetime use a text input holding the wire string, not a native
// date/datetime-local control that would corrupt the time.Time JSON.
func TestFrontendDateFieldsRoundTrip(t *testing.T) {
	id := func(k spec.Kind) spec.ID { return spec.MustNewID(k) }
	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: id(spec.KindProject), Name: "Acme", Slug: "acme"},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{
				ID: id(spec.KindResource), Label: "Event", CodeName: "Event", StorageName: "events",
				Behavior: spec.ResourceBehavior{CreateEnabled: true},
				Fields: []*spec.Field{
					{ID: id(spec.KindField), Label: "Day", Type: spec.TypeDate, CodeName: "Day", StorageName: "day"},
					{ID: id(spec.KindField), Label: "At", Type: spec.TypeDatetime, CodeName: "At", StorageName: "at"},
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
	form := frontendFiles(t, g)["frontend/src/forge_generated/event/EventFormPage.tsx"]

	// No native date controls, which cannot hold or produce RFC 3339.
	if strings.Contains(form, `type="date"`) || strings.Contains(form, `type="datetime-local"`) {
		t.Errorf("date fields must not use native date controls:\n%s", form)
	}
	if !strings.Contains(form, `<input type="text" placeholder="YYYY-MM-DDT00:00:00Z" {...register("day"`) {
		t.Error("date field must be a text input with an RFC 3339 placeholder")
	}
	if !strings.Contains(form, `<input type="text" placeholder="YYYY-MM-DDTHH:MM:SSZ" {...register("at"`) {
		t.Error("datetime field must be a text input with an RFC 3339 placeholder")
	}
	// The values are typed as strings (the wire format), not Date objects.
	if !strings.Contains(form, "day: string;") || !strings.Contains(form, "at: string;") {
		t.Error("date/datetime form values must be strings")
	}
	// An empty date must be dropped from the request body: time.Time rejects
	// "" but accepts a missing key.
	if !strings.Contains(form, `if (body.day === "") {`) || !strings.Contains(form, `delete (body as Partial<EventFormValues>).day;`) {
		t.Errorf("empty date must be omitted from the request body:\n%s", form)
	}
	if !strings.Contains(form, `if (body.at === "") {`) {
		t.Error("empty datetime must be omitted from the request body")
	}
}

// TestFrontendOmitsEmptyDecimal covers the other wire type that rejects "":
// decimal.Decimal. An empty optional decimal must be dropped from the body, not
// POSTed as "".
func TestFrontendOmitsEmptyDecimal(t *testing.T) {
	id := func(k spec.Kind) spec.ID { return spec.MustNewID(k) }
	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: id(spec.KindProject), Name: "Acme", Slug: "acme"},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{
				ID: id(spec.KindResource), Label: "Quote", CodeName: "Quote", StorageName: "quotes",
				Behavior: spec.ResourceBehavior{CreateEnabled: true},
				Fields: []*spec.Field{
					// Optional decimal, and a plain string alongside it.
					{ID: id(spec.KindField), Label: "Fee", Type: spec.TypeDecimal, CodeName: "Fee", StorageName: "fee"},
					{ID: id(spec.KindField), Label: "Note", Type: spec.TypeString, CodeName: "Note", StorageName: "note"},
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
	form := frontendFiles(t, g)["frontend/src/forge_generated/quote/QuoteFormPage.tsx"]

	if !strings.Contains(form, `if (body.fee === "") {`) {
		t.Errorf("empty decimal must be omitted from the request body:\n%s", form)
	}
	// A plain string must NOT be omitted — "" is a valid string value.
	if strings.Contains(form, `if (body.note === "") {`) {
		t.Error("a string field must not be omitted; \"\" is a valid value")
	}
}

// TestFrontendEscapesLabels guards against a spec label breaking the module:
// unconstrained human text must become an inert JS string, not JSX or an
// unterminated literal.
func TestFrontendEscapesLabels(t *testing.T) {
	id := func(k spec.Kind) spec.ID { return spec.MustNewID(k) }
	itemID := id(spec.KindResource)
	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: id(spec.KindProject), Name: "Acme", Slug: "acme"},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{
				ID: itemID, Label: `Item "X"`, LabelPlural: "Items {n}",
				CodeName: "Item", StorageName: "items",
				Behavior: spec.ResourceBehavior{CreateEnabled: true},
				Fields: []*spec.Field{
					{ID: id(spec.KindField), Label: `Day "d"`, Type: spec.TypeString, CodeName: "Day", StorageName: "day", Required: true},
					{ID: id(spec.KindField), Label: "Tier", Type: spec.TypeEnum, CodeName: "Tier", StorageName: "tier",
						EnumValues: []spec.EnumValue{{Value: `a"b`}}},
				},
			},
		},
		// The table title carries the same unconstrained text so its escaping in
		// the table <h1> is exercised.
		Pages: []*spec.Page{
			{ID: id(spec.KindPage), Slug: "items", Label: "Items {n}", Type: spec.PageResourceTable, Resource: itemID},
		},
	}
	if d := spec.Validate(s); d != nil {
		t.Fatalf("fixture invalid: %s", d.Error())
	}
	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	files := frontendFiles(t, g)
	list := files["frontend/src/forge_generated/item/ItemsTablePage.tsx"]
	form := files["frontend/src/forge_generated/item/ItemFormPage.tsx"]

	// The title "Items {n}" must be an inert string, not a JSX expression.
	if !strings.Contains(list, `<h1>{ "Items {n}" }</h1>`) {
		t.Errorf("table title must be escaped as a JS string:\n%s", list)
	}
	// A quote in a field label must be escaped inside the required message.
	if !strings.Contains(form, `required: "Day \"d\" is required"`) {
		t.Errorf("field label with a quote must be escaped in the required message:\n%s", form)
	}
	// A quote in an enum value must be escaped in both the attribute and text.
	if !strings.Contains(form, `<option value="a\"b">{ "a\"b" }</option>`) {
		t.Errorf("enum value with a quote must be escaped:\n%s", form)
	}
	// The raw unescaped forms must not appear.
	if strings.Contains(list, `<h1>Items {n}</h1>`) {
		t.Error("raw unescaped plural leaked into JSX")
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

// TestFrontendTableHonorsConfiguredColumns: the customers table pins columns
// [email, active], so those headers appear and the unlisted fields (tier,
// joined) do not — the table is driven by TableConfig.columns, not every field.
func TestFrontendTableHonorsConfiguredColumns(t *testing.T) {
	table := frontendFiles(t, buildGraph2(t))["frontend/src/forge_generated/customer/CustomersTablePage.tsx"]

	for _, want := range []string{`<th>{ "Email" }</th>`, `<th>{ "Active" }</th>`} {
		if !strings.Contains(table, want) {
			t.Errorf("configured column header missing: %q", want)
		}
	}
	for _, unwanted := range []string{`{ "Tier" }`, `{ "Joined" }`} {
		if strings.Contains(table, unwanted) {
			t.Errorf("unconfigured column leaked into the table: %q", unwanted)
		}
	}
}

// TestFrontendMultipleTablesPerResource is a #51 acceptance point: two
// resource_table pages can target one resource and generate independent tables
// with their own component, route and columns.
func TestFrontendMultipleTablesPerResource(t *testing.T) {
	id := func(k spec.Kind) spec.ID { return spec.MustNewID(k) }
	cust := id(spec.KindResource)
	email := id(spec.KindField)
	active := id(spec.KindField)
	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: id(spec.KindProject), Name: "Acme", Slug: "acme"},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{
				ID: cust, Label: "Customer", CodeName: "Customer", StorageName: "customers",
				Fields: []*spec.Field{
					{ID: email, Label: "Email", Type: spec.TypeString, CodeName: "Email", StorageName: "email"},
					{ID: active, Label: "Active", Type: spec.TypeBoolean, CodeName: "Active", StorageName: "active"},
				},
			},
		},
		Pages: []*spec.Page{
			{ID: id(spec.KindPage), Slug: "all-customers", Label: "All customers", Type: spec.PageResourceTable,
				Resource: cust, Table: &spec.TableConfig{Columns: []spec.ID{email}}},
			{ID: id(spec.KindPage), Slug: "active-customers", Label: "Active customers", Type: spec.PageResourceTable,
				Resource: cust, Table: &spec.TableConfig{Columns: []spec.ID{active}}},
		},
	}
	if d := spec.Validate(s); d != nil {
		t.Fatalf("fixture invalid: %s", d.Error())
	}
	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	files := frontendFiles(t, g)

	all := files["frontend/src/forge_generated/customer/AllCustomersTablePage.tsx"]
	act := files["frontend/src/forge_generated/customer/ActiveCustomersTablePage.tsx"]
	if all == "" || act == "" {
		t.Fatalf("both table pages must be generated; got files %v", paths(frontendFilesSlice(t, g)))
	}
	// Each honors its own single column.
	if !strings.Contains(all, `<th>{ "Email" }</th>`) || strings.Contains(all, `{ "Active" }`) {
		t.Error("all-customers table must show only its Email column")
	}
	if !strings.Contains(act, `<th>{ "Active" }</th>`) || strings.Contains(act, `{ "Email" }`) {
		t.Error("active-customers table must show only its Active column")
	}
	// Both routes are registered independently, and both appear in nav metadata.
	registry := files["frontend/src/forge_generated/resources.tsx"]
	for _, want := range []string{
		`{ path: "all-customers", element: <AllCustomersTablePage /> }`,
		`{ path: "active-customers", element: <ActiveCustomersTablePage /> }`,
		`{ slug: "all-customers", title: "All customers", listPath: "/all-customers" }`,
		`{ slug: "active-customers", title: "Active customers", listPath: "/active-customers" }`,
	} {
		if !strings.Contains(registry, want) {
			t.Errorf("registry must contain %q", want)
		}
	}
}

// TestFrontendNoImplicitList is the other #51 acceptance point: a resource with
// no resource_table page gets no list page, route or nav entry — but still gets
// its resource-driven detail page.
func TestFrontendNoImplicitList(t *testing.T) {
	id := func(k spec.Kind) spec.ID { return spec.MustNewID(k) }
	listed := id(spec.KindResource)
	unlisted := id(spec.KindResource)
	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: id(spec.KindProject), Name: "Acme", Slug: "acme"},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{ID: listed, Label: "Customer", CodeName: "Customer", StorageName: "customers",
				Fields: []*spec.Field{{ID: id(spec.KindField), Label: "Email", Type: spec.TypeString, CodeName: "Email", StorageName: "email"}}},
			{ID: unlisted, Label: "Secret", CodeName: "Secret", StorageName: "secrets",
				Fields: []*spec.Field{{ID: id(spec.KindField), Label: "Code", Type: spec.TypeString, CodeName: "Code", StorageName: "code"}}},
		},
		Pages: []*spec.Page{
			{ID: id(spec.KindPage), Slug: "customers", Label: "Customers", Type: spec.PageResourceTable, Resource: listed},
		},
	}
	if d := spec.Validate(s); d != nil {
		t.Fatalf("fixture invalid: %s", d.Error())
	}
	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	files := frontendFiles(t, g)

	// The unlisted resource has a detail page but no table page of any name.
	if _, ok := files["frontend/src/forge_generated/secret/SecretDetailPage.tsx"]; !ok {
		t.Error("a resource without a table page still gets its detail page")
	}
	for path := range files {
		if strings.Contains(path, "/secret/") && strings.Contains(path, "TablePage") {
			t.Errorf("a resource without a table page must get no list page; found %s", path)
		}
	}
	// No route or nav entry for the unlisted resource.
	registry := files["frontend/src/forge_generated/resources.tsx"]
	if strings.Contains(registry, `{ path: "secrets", element:`) {
		t.Error("no implicit list route for a resource without a table page")
	}
	if strings.Contains(registry, `slug: "secrets"`) {
		t.Error("no nav entry for a resource without a table page")
	}
	// The detail route still exists (reachable directly / from other pages).
	if !strings.Contains(registry, `{ path: "secrets/:id", element: <SecretDetailPage /> }`) {
		t.Error("the resource's detail route must still be registered")
	}
}

func frontendFilesSlice(t *testing.T, g *graph.Graph) []File {
	t.Helper()
	files, err := Frontend(g)
	if err != nil {
		t.Fatalf("Frontend: %v", err)
	}
	return files
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
