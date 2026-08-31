package gen

import (
	"strings"
	"testing"

	"github.com/gombit-dev/gombit-forge/internal/compiler/graph"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

const refsModule = "example.com/app"

func extensionSource(t *testing.T, files []File) string {
	t.Helper()
	want := GeneratedRoot + "/extension/extension.go"
	for _, file := range files {
		if file.Path == want {
			return string(file.Content)
		}
	}
	t.Fatalf("no extension package at %s; got %v", want, paths(files))
	return ""
}

func fieldsSource(t *testing.T, files []File, pkg string) string {
	t.Helper()
	want := GeneratedRoot + "/" + pkg + "/fields.go"
	for _, file := range files {
		if file.Path == want {
			return string(file.Content)
		}
	}
	t.Fatalf("no generated file at %s; got %v", want, paths(files))
	return ""
}

func TestExtensionPackageRejectionAPI(t *testing.T) {
	g, _ := buildGraph(t)
	files, err := Extension(g)
	if err != nil {
		t.Fatalf("Extension: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("Extension must emit exactly one file, got %v", paths(files))
	}
	src := extensionSource(t, files)

	if !strings.HasPrefix(src, Banner) {
		t.Error("extension.go must start with the DO-NOT-EDIT banner")
	}
	if !strings.Contains(src, "package extension\n") {
		t.Error("extension.go must declare package extension")
	}
	// The leaf package must not drag in any Forge runtime (D2): it imports only
	// the standard library, and here nothing at all.
	if strings.Contains(src, "gombit-forge") || strings.Contains(src, "\nimport ") {
		t.Errorf("extension package must be dependency-free; got:\n%s", src)
	}
	for _, want := range []string{
		"type FieldRef struct {",
		"type FieldError struct {",
		"func (e *FieldError) Error() string",
		"func InvalidField(field FieldRef, message string) error",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("extension.go missing %q; got:\n%s", want, src)
		}
	}
}

func TestFieldRefsFilePaths(t *testing.T) {
	g, _ := buildGraph(t)
	files, err := FieldRefs(g, refsModule)
	if err != nil {
		t.Fatalf("FieldRefs: %v", err)
	}
	want := []string{
		"internal/forge_generated/customer/fields.go",
		"internal/forge_generated/invoice/fields.go",
	}
	got := paths(files)
	if len(got) != len(want) {
		t.Fatalf("file count: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("file %d: got %q want %q", i, got[i], want[i])
		}
	}
}

// TestFieldRefsPerField checks each field gets a Field<code symbol> reference
// that imports the shared extension package and embeds the field's stable IDs
// and its current API (storage) name (ADR-001 §27).
func TestFieldRefsPerField(t *testing.T) {
	g, ids := buildGraph(t)
	files, _ := FieldRefs(g, refsModule)
	src := fieldsSource(t, files, "customer")

	if !strings.Contains(src, `import "example.com/app/internal/forge_generated/extension"`) {
		t.Errorf("fields.go must import the shared extension package; got:\n%s", src)
	}
	// Email's code symbol is Email but its storage name is contact_email: the
	// reference symbol follows the frozen code symbol; Name follows storage.
	wantRef := `FieldEmail = extension.FieldRef{Resource: "` + string(ids["customer"]) +
		`", Field: "` + string(ids["email"]) + `", Name: "contact_email"}`
	if !strings.Contains(collapseRefWS(src), wantRef) {
		t.Errorf("fields.go missing %q; got:\n%s", wantRef, src)
	}
}

// TestFieldRefsBelongsTo checks a belongs_to gets a reference under its foreign
// key symbol (FieldCustomerID), matching the mutation/model field name.
func TestFieldRefsBelongsTo(t *testing.T) {
	g, _ := buildGraph(t)
	files, _ := FieldRefs(g, refsModule)
	src := collapseRefWS(fieldsSource(t, files, "invoice"))
	if !strings.Contains(src, "FieldCustomerID = extension.FieldRef{") {
		t.Errorf("belongs_to must get FieldCustomerID; got:\n%s", src)
	}
}

// TestFieldRefsSurviveRelabelAndStorageRename is the §27 stability guarantee:
// the reference symbol a hook writes and the field identity it carries do not
// change under a relabel or storage rename. Only the API Name (storage) tracks
// the rename.
func TestFieldRefsSurviveRelabelAndStorageRename(t *testing.T) {
	base, _ := buildGraph(t)
	// Capture the reference output before any mutation.
	baseFiles, err := FieldRefs(base, refsModule)
	if err != nil {
		t.Fatalf("FieldRefs(base): %v", err)
	}

	// Relabel and rename storage on the very same spec — same stable IDs and
	// code symbols — then rebuild and regenerate. Only labels and storage names
	// change; identity is frozen.
	renamed := renameForRefs(t, base)
	renamedFiles, err := FieldRefs(renamed, refsModule)
	if err != nil {
		t.Fatalf("FieldRefs(renamed): %v", err)
	}

	for _, pkg := range []string{"customer", "invoice"} {
		before := parseRefs(fieldsSource(t, baseFiles, pkg))
		after := parseRefs(fieldsSource(t, renamedFiles, pkg))
		if len(before) == 0 || len(after) != len(before) {
			t.Fatalf("%s: ref count changed %d -> %d", pkg, len(before), len(after))
		}
		for name, fieldID := range before {
			got, ok := after[name]
			if !ok {
				t.Errorf("%s: reference %s disappeared under rename", pkg, name)
				continue
			}
			if got != fieldID {
				t.Errorf("%s: reference %s identity moved %q -> %q under rename", pkg, name, fieldID, got)
			}
		}
	}
}

// TestFieldRefsSkipsFieldlessResource checks a resource with no fields emits no
// fields.go, so the extension import is never unused.
func TestFieldRefsSkipsFieldlessResource(t *testing.T) {
	g := fieldlessResourceGraph(t)
	files, err := FieldRefs(g, refsModule)
	if err != nil {
		t.Fatalf("FieldRefs: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("a field-less resource must emit no fields.go; got %v", paths(files))
	}
}

// TestFieldRefsRejectModelTypeCollision gives the guard teeth: a resource whose
// own code symbol is Field<field> would emit a var colliding with the model
// type in the same package.
func TestFieldRefsRejectModelTypeCollision(t *testing.T) {
	g := collidingRefGraph(t)
	if _, err := FieldRefs(g, refsModule); err == nil {
		t.Fatal("FieldRefs must reject a reference var colliding with the model type")
	}
}

func TestExtensionNilGraph(t *testing.T) {
	if _, err := Extension(nil); err == nil {
		t.Error("Extension(nil) must error")
	}
}

func TestFieldRefsNilGraphAndEmptyModule(t *testing.T) {
	g, _ := buildGraph(t)
	if _, err := FieldRefs(nil, refsModule); err == nil {
		t.Error("FieldRefs(nil, …) must error")
	}
	if _, err := FieldRefs(g, ""); err == nil {
		t.Error("FieldRefs(…, \"\") must error")
	}
}

// collapseRefWS joins on whitespace runs so a substring check ignores gofmt's
// column alignment of the var block.
func collapseRefWS(s string) string { return strings.Join(strings.Fields(s), " ") }

// parseRefs maps each reference symbol to the stable Field ID it carries.
func parseRefs(src string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		name, rest, ok := strings.Cut(line, " = extension.FieldRef{")
		if !ok {
			continue
		}
		_, after, ok := strings.Cut(rest, `Field: "`)
		if !ok {
			continue
		}
		id, _, ok := strings.Cut(after, `"`)
		if !ok {
			continue
		}
		out[strings.TrimSpace(name)] = id
	}
	return out
}

// renameForRefs mutates the given graph's spec — every label and storage_name
// changed, every code symbol and stable ID frozen — and rebuilds it. The caller
// must have already captured any output generated from the pre-mutation graph.
func renameForRefs(t *testing.T, g *graph.Graph) *graph.Graph {
	t.Helper()
	for _, resource := range g.Resources {
		resource.Spec.Label = "Renamed " + resource.Spec.Label
		resource.Spec.StorageName = resource.Spec.StorageName + "_v2"
		for i, field := range resource.Fields {
			field.Spec.Label = "Renamed field"
			if strings.HasSuffix(field.Spec.StorageName, "_id") {
				field.Spec.StorageName = "renamed_ref_id"
			} else {
				field.Spec.StorageName = "renamed_col_" + string(rune('a'+i))
			}
		}
	}
	if diagnostics := spec.Validate(g.Spec); diagnostics != nil {
		t.Fatalf("renamed fixture is invalid:\n%s", diagnostics.Error())
	}
	rebuilt, err := graph.Build(g.Spec)
	if err != nil {
		t.Fatalf("rebuild renamed graph: %v", err)
	}
	return rebuilt
}

func fieldlessResourceGraph(t *testing.T) *graph.Graph {
	t.Helper()
	id := func(k spec.Kind) spec.ID { return spec.MustNewID(k) }
	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: id(spec.KindProject), Name: "Acme", Slug: "acme"},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{ID: id(spec.KindResource), Label: "Empty", CodeName: "Empty", StorageName: "empties"},
		},
	}
	if d := spec.Validate(s); d != nil {
		t.Fatalf("field-less fixture invalid:\n%s", d.Error())
	}
	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build field-less graph: %v", err)
	}
	return g
}

// collidingRefGraph builds a resource whose code symbol is FieldName and which
// has a field code symbol Name, so the field ref FieldName collides with the
// model type.
func collidingRefGraph(t *testing.T) *graph.Graph {
	t.Helper()
	id := func(k spec.Kind) spec.ID { return spec.MustNewID(k) }
	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: id(spec.KindProject), Name: "Acme", Slug: "acme"},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{
				ID: id(spec.KindResource), Label: "Widget",
				CodeName: "FieldName", StorageName: "widgets",
				Fields: []*spec.Field{
					{ID: id(spec.KindField), Label: "Name", Type: spec.TypeString, CodeName: "Name", StorageName: "name"},
				},
			},
		},
	}
	if d := spec.Validate(s); d != nil {
		t.Fatalf("colliding fixture invalid:\n%s", d.Error())
	}
	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build colliding graph: %v", err)
	}
	return g
}
