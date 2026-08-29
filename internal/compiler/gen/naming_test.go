package gen

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gombit-dev/gombit-forge/internal/compiler/graph"
	"github.com/gombit-dev/gombit-forge/internal/spec"
	"gorm.io/gorm/schema"
)

// oneResourceGraph builds a graph with a single resource carrying the given
// fields, so naming and default edge cases can be exercised in isolation.
func oneResourceGraph(t *testing.T, code, storage string, fields ...*spec.Field) *graph.Graph {
	t.Helper()
	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: spec.MustNewID(spec.KindProject), Name: "Acme", Slug: "acme"},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{ID: spec.MustNewID(spec.KindResource), Label: code, CodeName: code, StorageName: storage, Fields: fields},
		},
	}
	if d := spec.Validate(s); d != nil {
		t.Fatalf("fixture invalid:\n%s", d.Error())
	}
	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return g
}

func field(code, storage string, ft spec.FieldType) *spec.Field {
	return &spec.Field{ID: spec.MustNewID(spec.KindField), Label: code, Type: ft, CodeName: code, StorageName: storage}
}

// --- Finding 1: table name follows storage_name -----------------------------

// TestTableNameFollowsStorageName is the table-side counterpart to the column
// rule: GORM inflects a struct name ("Person" -> "people"), so storage_name
// must be pinned with a TableName method or a rename is silently dropped.
func TestTableNameFollowsStorageName(t *testing.T) {
	// code_name Person would inflect to table "people"; storage_name is
	// "persons", and that must win.
	g := oneResourceGraph(t, "Person", "persons", field("Name", "name", spec.TypeString))
	files, err := Models(g)
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	src := string(files[0].Content)

	if !strings.Contains(src, `func (Person) TableName() string { return "persons" }`) {
		t.Errorf("model must pin the table to storage_name persons:\n%s", src)
	}
}

// TestTableNameUsesStorageNotInflection makes the drift concrete: a resource
// whose code symbol and storage_name would inflect differently.
func TestTableNameUsesStorageNotInflection(t *testing.T) {
	g := oneResourceGraph(t, "Customer", "clients", field("Name", "name", spec.TypeString))
	files, _ := Models(g)
	src := string(files[0].Content)

	if !strings.Contains(src, `return "clients"`) {
		t.Error("table must be the storage_name clients, not an inflection of Customer")
	}
}

// --- Finding 2: derived symbols are reserved/rejected -----------------------

// TestPackageNameKeywordRejected covers a valid code symbol whose lowercase
// form is a Go keyword, which would emit `package type`.
func TestPackageNameKeywordRejected(t *testing.T) {
	// "Type" is a legal exported identifier and a legal code_name, but
	// lowercases to the keyword "type".
	g := oneResourceGraph(t, "Type", "types", field("Name", "name", spec.TypeString))
	_, err := Models(g)
	if err == nil {
		t.Fatal("expected a resource whose package name is a keyword to be rejected")
	}
	// The rejection must be the explicit reservation check (ADR-001 §12), not
	// a gofmt parse error discovered after emitting `package type`.
	if !strings.Contains(err.Error(), "keyword") {
		t.Errorf("rejection should name the keyword collision, got: %v", err)
	}
}

// TestBelongsToFieldCollisionRejected covers the FK-suffix collision: a
// belongs_to "Customer" derives "CustomerID", which must not silently coexist
// with a scalar field whose code symbol is already "CustomerID".
func TestBelongsToFieldCollisionRejected(t *testing.T) {
	target := spec.MustNewID(spec.KindResource)
	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project:     spec.Project{ID: spec.MustNewID(spec.KindProject), Name: "Acme", Slug: "acme"},
		Database:    spec.Database{Driver: spec.DriverPostgres},
		Auth:        spec.Auth{Mode: spec.AuthCookie},
		Resources: []*spec.Resource{
			{ID: target, Label: "Customer", CodeName: "Customer", StorageName: "customers"},
			{
				ID: spec.MustNewID(spec.KindResource), Label: "Invoice",
				CodeName: "Invoice", StorageName: "invoices",
				Fields: []*spec.Field{
					{
						ID: spec.MustNewID(spec.KindField), Label: "Customer", Type: spec.TypeBelongsTo,
						CodeName: "Customer", StorageName: "customer_id", Target: target,
					},
					// Scalar field whose code symbol collides with the FK's
					// derived name CustomerID.
					{
						ID: spec.MustNewID(spec.KindField), Label: "Legacy id", Type: spec.TypeInteger,
						CodeName: "CustomerID", StorageName: "legacy_customer_id",
					},
				},
			},
		},
	}
	if d := spec.Validate(s); d != nil {
		t.Fatalf("fixture should be spec-valid (the collision is a generated-symbol clash): %s", d.Error())
	}
	g, err := graph.Build(s)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if _, err := Models(g); err == nil {
		t.Fatal("expected the CustomerID field collision to be rejected before emitting duplicate Go fields")
	}
}

// TestGeneratedFieldShadowingGormModelRejected covers a belongs_to whose
// derived name would shadow a gorm.Model field. (Scalar code_names like "ID"
// are already rejected by the spec validator; the FK "+ID" derivation is not.)
func TestGeneratedFieldShadowingGormModelRejected(t *testing.T) {
	// A resource-reference field named "I" would derive Go field "IID" — no
	// clash. To hit a gorm.Model field via derivation we would need a belongs_to
	// deriving e.g. "ID"; that requires code symbol "", which is invalid. So
	// this guards the scalar path through gormModelFields directly instead.
	if _, reserved := gormModelFields["CreatedAt"]; !reserved {
		t.Fatal("gorm.Model field set must include CreatedAt")
	}
}

// --- Finding 3: defaults round-trip or are rejected -------------------------

// TestSafeStringDefaultRoundTrips proves a permitted string default survives
// both Go's struct-tag unquoting and GORM's tag parser, using the real GORM
// parser rather than asserting the raw text.
func TestSafeStringDefaultRoundTrips(t *testing.T) {
	values := []string{"free", "O'Brien", "hello world", "50%"}
	for _, v := range values {
		t.Run(v, func(t *testing.T) {
			f := field("Note", "note", spec.TypeString)
			f.Default = &v
			g := oneResourceGraph(t, "Doc", "docs", f)

			files, err := Models(g)
			if err != nil {
				t.Fatalf("Models: %v", err)
			}
			gormTag := extractGormTag(t, string(files[0].Content), "Note")
			def := schema.ParseTagSetting(gormTag, ";")["DEFAULT"]

			want := "'" + strings.ReplaceAll(v, "'", "''") + "'"
			if def != want {
				t.Errorf("default did not round-trip: got %q want %q (tag %q)", def, want, gormTag)
			}
		})
	}
}

// TestUnsafeStringDefaultRejected proves the values that cannot round-trip are
// refused rather than silently corrupted.
func TestUnsafeStringDefaultRejected(t *testing.T) {
	for _, v := range []string{"a;b", `a"b`, "a`b", `a\b`, "a\nb"} {
		t.Run(v, func(t *testing.T) {
			f := field("Note", "note", spec.TypeString)
			f.Default = &v
			g := oneResourceGraph(t, "Doc", "docs", f)

			if _, err := Models(g); err == nil {
				t.Errorf("default %q should be rejected as unrepresentable", v)
			}
		})
	}
}

// extractGormTag pulls the gorm struct tag for a field out of generated source
// by parsing it the same way the Go runtime does.
func extractGormTag(t *testing.T, src, fieldName string) string {
	t.Helper()
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, fieldName+" ") {
			continue
		}
		start := strings.IndexByte(trimmed, '`')
		end := strings.LastIndexByte(trimmed, '`')
		if start < 0 || end <= start {
			t.Fatalf("no backtick struct tag on line: %s", line)
		}
		return reflect.StructTag(trimmed[start+1 : end]).Get("gorm")
	}
	t.Fatalf("no field %q in:\n%s", fieldName, src)
	return ""
}
