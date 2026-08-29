package gen

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/gombit-dev/gombit-forge/internal/compiler/graph"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

// Models generates one model.go per resource under
// internal/forge_generated/<resource>/ (DESIGN.md §9 stage 3).
//
// Each model embeds gorm.Model — so it inherits ID, CreatedAt, UpdatedAt and
// DeletedAt, matching how a hand-written Gombit app models a resource (ADR-004
// D3) — and carries one field per spec field. Deployed apps never AutoMigrate
// (DESIGN.md §14); these models feed Atlas migration generation instead.
//
// Files come back in the graph's authored resource order.
func Models(g *graph.Graph) ([]File, error) {
	if g == nil {
		return nil, fmt.Errorf("gen: nil graph")
	}

	files := make([]File, 0, len(g.Resources))
	for _, resource := range g.Resources {
		file, err := modelFile(resource)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func modelFile(resource *graph.Resource) (File, error) {
	if err := validateNames(resource); err != nil {
		return File{}, err
	}

	// Resolve every field's type and tag first: this collects the imports and
	// fails before any source is emitted if a field type is unmapped or a
	// default cannot be safely represented.
	type renderedField struct {
		name    string
		goType  string
		tagBody string
	}

	fields := make([]renderedField, 0, len(resource.Fields))
	imports := map[string]struct{}{"gorm.io/gorm": {}}

	for _, field := range resource.Fields {
		mapping, err := resolveType(field)
		if err != nil {
			return File{}, err
		}
		tagBody, err := gormTag(field, mapping)
		if err != nil {
			return File{}, err
		}
		if mapping.importPath != "" {
			imports[mapping.importPath] = struct{}{}
		}
		fields = append(fields, renderedField{
			name:    goFieldName(field),
			goType:  mapping.goType,
			tagBody: tagBody,
		})
	}

	var b strings.Builder
	b.WriteString(Banner)
	b.WriteString("\n\npackage ")
	b.WriteString(PackageName(resource))
	b.WriteString("\n\n")
	b.WriteString(renderImports(imports))
	b.WriteString("\n")

	fmt.Fprintf(&b, "// %s is the generated persistence model for the %q resource.\n",
		resource.CodeName(), resource.Spec.Label)
	fmt.Fprintf(&b, "type %s struct {\n", resource.CodeName())
	b.WriteString("\tgorm.Model\n")
	for _, field := range fields {
		fmt.Fprintf(&b, "\t%s %s `gorm:%q`\n", field.name, field.goType, field.tagBody)
	}
	b.WriteString("}\n\n")

	// The table name is storage_name, not GORM's inflection of the struct
	// name. Without this, "Person" would map to table "people" and a resource
	// whose storage_name was renamed would silently keep the old table —
	// the same naming-domain drift the explicit column tags prevent
	// (ADR-001 D2). storage_name is a validated lower_snake_case identifier,
	// so it needs no quoting beyond the Go string literal.
	fmt.Fprintf(&b, "// TableName reports the storage table for %s.\n", resource.CodeName())
	fmt.Fprintf(&b, "func (%s) TableName() string { return %q }\n",
		resource.CodeName(), resource.Spec.StorageName)

	relPath := path.Join(PackageDir(resource), "model.go")
	return formatGo(relPath, b.String())
}

// validateNames rejects a resource whose generated Go identifiers would be
// invalid or collide. These are symbols the generator derives — the package
// name from the code symbol, and a belongs_to key from the code symbol plus
// "ID" — that the spec validator never saw, so they are checked here rather
// than left for gofmt or go build to reject (ADR-001 §12).
func validateNames(resource *graph.Resource) error {
	if pkg := PackageName(resource); spec.IsGoKeyword(pkg) {
		return fmt.Errorf(
			"gen: resource %s code_name %q yields the Go keyword package name %q; rename the resource code symbol",
			resource.Spec.ID, resource.CodeName(), pkg)
	}

	// Every generated struct-field name must be unique and must not shadow a
	// field promoted from gorm.Model.
	seen := make(map[string]spec.ID, len(resource.Fields))
	for _, field := range resource.Fields {
		name := goFieldName(field)
		if _, reserved := gormModelFields[name]; reserved {
			return fmt.Errorf(
				"gen: field %s generates Go field %q, which gorm.Model already provides",
				field.Spec.ID, name)
		}
		if owner, dup := seen[name]; dup {
			return fmt.Errorf(
				"gen: fields %s and %s both generate the Go struct field %q",
				owner, field.Spec.ID, name)
		}
		seen[name] = field.Spec.ID
	}
	return nil
}

// renderImports renders an import block from a set of paths.
//
// Paths are sorted so output is deterministic despite the set being a map;
// gofmt would group them anyway, but sorting here keeps the pre-format string
// stable too.
func renderImports(paths map[string]struct{}) string {
	if len(paths) == 0 {
		return ""
	}

	sorted := make([]string, 0, len(paths))
	for importPath := range paths {
		sorted = append(sorted, importPath)
	}
	sort.Strings(sorted)

	if len(sorted) == 1 {
		return fmt.Sprintf("import %q\n", sorted[0])
	}

	var b strings.Builder
	b.WriteString("import (\n")
	for _, importPath := range sorted {
		fmt.Fprintf(&b, "\t%q\n", importPath)
	}
	b.WriteString(")\n")
	return b.String()
}
