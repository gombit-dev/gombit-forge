package gen

import (
	"fmt"
	"strings"

	"github.com/gombit-dev/gombit-forge/internal/compiler/graph"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

// goMapping is the Go and GORM representation of one field type.
type goMapping struct {
	// goType is the Go type of the struct field.
	goType string
	// importPath is the package the goType needs, or "" for a builtin.
	importPath string
	// gormType is the explicit GORM column type, or "" to let GORM choose.
	gormType string
}

// belongs_to is deliberately absent from this table: a relationship is not a
// scalar column and is mapped separately (see relationshipField), because its
// Go field name, column and type are all derived differently.
var scalarMappings = map[spec.FieldType]goMapping{
	// A relabel/rename can make code_name and storage_name diverge, so string
	// length lives in the GORM tag rather than being implied.
	spec.TypeString:   {goType: "string"},
	spec.TypeText:     {goType: "string", gormType: "text"},
	spec.TypeInteger:  {goType: "int64"},
	spec.TypeBoolean:  {goType: "bool"},
	spec.TypeDatetime: {goType: "time.Time", importPath: "time"},
	spec.TypeDate:     {goType: "time.Time", importPath: "time", gormType: "date"},
	spec.TypeEnum:     {goType: "string"},

	// Money must not be a float. shopspring/decimal is the ecosystem standard,
	// implements sql.Scanner/driver.Valuer, and maps losslessly to Postgres
	// numeric. It is already a dependency of Gombit itself, so a generated app
	// gains no novel dependency by using it (ADR-004 D3).
	spec.TypeDecimal: {
		goType:     "decimal.Decimal",
		importPath: "github.com/shopspring/decimal",
		gormType:   "numeric",
	},
}

// stringColumnSize is the GORM size applied to plain string columns, matching
// the convention Gombit's own resource generator uses.
const stringColumnSize = 255

// relationshipFKType is the Go type of a foreign-key column. It matches
// gorm.Model.ID, which is uint.
const relationshipFKType = "uint"

// goFieldName is the Go struct field name for a field.
//
// For a scalar it is the frozen code symbol. For a belongs_to it is the code
// symbol plus an "ID" suffix, matching GORM's foreign-key convention: the
// association is named by the code symbol and its key column by <Name>ID.
func goFieldName(field *graph.Field) string {
	if field.Spec.Type == spec.TypeBelongsTo {
		return field.CodeName() + "ID"
	}
	return field.CodeName()
}

// resolveType returns the Go type, needed import, and GORM type for a field.
func resolveType(field *graph.Field) (goMapping, error) {
	if field.Spec.Type == spec.TypeBelongsTo {
		return goMapping{goType: relationshipFKType}, nil
	}
	mapping, ok := scalarMappings[field.Spec.Type]
	if !ok {
		// Unreachable for a validated spec; surfaced rather than silently
		// emitting a zero type, so a new field type cannot slip through
		// untyped.
		return goMapping{}, fmt.Errorf(
			"gen: no Go mapping for field type %q (field %s)",
			field.Spec.Type, field.Spec.ID)
	}
	return mapping, nil
}

// gormTag builds the struct tag body for a field: the parts inside
// `gorm:"..."`. The column is always emitted explicitly because storage_name
// is an independent naming domain and need not match what GORM would derive
// from the Go field name (ADR-001 D2).
func gormTag(field *graph.Field, mapping goMapping) string {
	parts := []string{"column:" + field.Spec.StorageName}

	if mapping.gormType != "" {
		parts = append(parts, "type:"+mapping.gormType)
	} else if field.Spec.Type == spec.TypeString {
		parts = append(parts, fmt.Sprintf("size:%d", stringColumnSize))
	}

	if field.Spec.Required {
		parts = append(parts, "not null")
	}

	// unique implies an index, so the two are mutually exclusive in the tag.
	switch {
	case field.Spec.Unique:
		parts = append(parts, "uniqueIndex")
	case field.Spec.Index:
		parts = append(parts, "index")
	}

	if field.Spec.Default != nil {
		parts = append(parts, "default:"+gormDefault(field))
	}

	return strings.Join(parts, ";")
}

// gormDefault renders a field's default for a GORM default: tag.
//
// The value is validated against its type before it reaches here (a spec with
// a bad default never builds a graph), so this only quotes it for the tag.
func gormDefault(field *graph.Field) string {
	value := *field.Spec.Default
	switch field.Spec.Type {
	case spec.TypeString, spec.TypeText, spec.TypeEnum, spec.TypeDate, spec.TypeDatetime:
		// Wrap in single quotes so a value containing a semicolon or space
		// does not corrupt the surrounding tag or SQL.
		return "'" + value + "'"
	default:
		return value
	}
}
