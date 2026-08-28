// Package spec defines ProjectSpec, the declarative source of truth for a
// Forge application (DESIGN.md P1, §7).
//
// The canonical storage form is JSON (DESIGN.md §7). Serialization preserves
// stable IDs, frozen code symbols and authored ordering so that semantically
// identical specs never produce formatting churn (ADR-001 §70).
//
// Three naming domains are kept deliberately separate (ADR-001 D2, §3):
//
//	ID           immutable semantic identity
//	Label        human presentation, freely mutable
//	StorageName  database mapping, mutable through migration intent
//	CodeName     generated source ABI, frozen unless an explicit refactor
package spec

// SpecVersion is the ProjectSpec schema version understood by this package.
const SpecVersion = 1

// FieldType is a semantic field type in the Forge MVP (DESIGN.md §4.2).
//
// has_many is derived from BelongsTo rather than authored; many-to-many is
// deferred.
type FieldType string

const (
	TypeString    FieldType = "string"
	TypeText      FieldType = "text"
	TypeInteger   FieldType = "integer"
	TypeDecimal   FieldType = "decimal"
	TypeBoolean   FieldType = "boolean"
	TypeDatetime  FieldType = "datetime"
	TypeDate      FieldType = "date"
	TypeEnum      FieldType = "enum"
	TypeBelongsTo FieldType = "belongs_to"
)

// FieldTypes lists every supported MVP field type in a stable order.
func FieldTypes() []FieldType {
	return []FieldType{
		TypeString, TypeText, TypeInteger, TypeDecimal, TypeBoolean,
		TypeDatetime, TypeDate, TypeEnum, TypeBelongsTo,
	}
}

// Valid reports whether t is a supported MVP field type.
func (t FieldType) Valid() bool {
	for _, candidate := range FieldTypes() {
		if t == candidate {
			return true
		}
	}
	return false
}

// Scalar reports whether the type carries a value directly rather than a
// reference to another resource.
func (t FieldType) Scalar() bool { return t.Valid() && t != TypeBelongsTo }

// DatabaseDriver is the persistence driver for the generated application.
//
// Managed hosting is PostgreSQL-only in the MVP (DESIGN.md D4); exported
// applications may target any driver Gombit supports.
type DatabaseDriver string

const (
	DriverPostgres DatabaseDriver = "postgres"
	DriverSQLite   DatabaseDriver = "sqlite"
	DriverMySQL    DatabaseDriver = "mysql"
)

// AuthMode is the authentication mode of the generated application.
//
// Managed applications default to cookie/session auth (DESIGN.md D5).
type AuthMode string

const (
	AuthCookie AuthMode = "cookie"
	AuthJWT    AuthMode = "jwt"
	AuthNone   AuthMode = "none"
)

// PageType enumerates the structured MVP page types (DESIGN.md §4.4).
//
// There is deliberately no freeform canvas (DESIGN.md P3, D6).
type PageType string

const (
	PageResourceTable  PageType = "resource_table"
	PageResourceForm   PageType = "resource_form"
	PageResourceDetail PageType = "resource_detail"
	PageDashboard      PageType = "dashboard"
)

// ProjectSpec is the complete declarative description of a Forge application.
//
// Field order in this struct defines key order in canonical JSON.
type ProjectSpec struct {
	SpecVersion int         `json:"spec_version"`
	Project     Project     `json:"project"`
	Database    Database    `json:"database"`
	Auth        Auth        `json:"auth"`
	Resources   []*Resource `json:"resources"`
	Pages       []*Page     `json:"pages"`
	Navigation  []*NavItem  `json:"navigation"`
	Branding    *Branding   `json:"branding,omitempty"`
}

// Project carries project-level identity and presentation.
type Project struct {
	ID    ID     `json:"id"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Label string `json:"label,omitempty"`
}

// Database selects the persistence driver.
type Database struct {
	Driver DatabaseDriver `json:"driver"`
}

// Auth selects the authentication mode.
type Auth struct {
	Mode AuthMode `json:"mode"`
}

// Resource is one addressable domain entity.
type Resource struct {
	ID          ID     `json:"id"`
	Label       string `json:"label"`
	LabelPlural string `json:"label_plural,omitempty"`
	// CodeName is the frozen Go symbol for this resource (ADR-001 D3).
	CodeName string `json:"code_name"`
	// StorageName is the database table name (ADR-001 D2).
	StorageName string `json:"storage_name"`

	Fields []*Field `json:"fields"`

	Behavior ResourceBehavior `json:"behavior"`
}

// ResourceBehavior captures per-resource CRUD and presentation settings
// (DESIGN.md §4.3).
type ResourceBehavior struct {
	CreateEnabled bool `json:"create_enabled"`
	UpdateEnabled bool `json:"update_enabled"`
	DeleteEnabled bool `json:"delete_enabled"`
	AdminVisible  bool `json:"admin_visible"`

	// The following reference field IDs belonging to the owning resource.
	ListFields       []ID `json:"list_fields,omitempty"`
	SearchableFields []ID `json:"searchable_fields,omitempty"`
	SortableFields   []ID `json:"sortable_fields,omitempty"`
	FilterableFields []ID `json:"filterable_fields,omitempty"`
}

// Field is one attribute of a Resource.
type Field struct {
	ID    ID        `json:"id"`
	Label string    `json:"label"`
	Type  FieldType `json:"type"`
	// CodeName is the frozen Go symbol / extension accessor name (ADR-001 D3, §23).
	CodeName string `json:"code_name"`
	// StorageName is the database column name (ADR-001 D2).
	StorageName string `json:"storage_name"`

	Required bool `json:"required,omitempty"`
	Unique   bool `json:"unique,omitempty"`
	Index    bool `json:"index,omitempty"`

	// Default is the literal default value, encoded as written in the spec.
	Default *string `json:"default,omitempty"`

	// EnumValues is required when Type is enum and empty otherwise.
	EnumValues []EnumValue `json:"enum_values,omitempty"`

	// Target is the referenced Resource ID when Type is belongs_to.
	Target ID `json:"target,omitempty"`
	// InverseLabel names the derived has_many side on the target resource.
	InverseLabel string `json:"inverse_label,omitempty"`
}

// EnumValue is one permitted value of an enum field.
type EnumValue struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

// Page is one structured screen in the generated application.
type Page struct {
	ID    ID       `json:"id"`
	Slug  string   `json:"slug"`
	Label string   `json:"label"`
	Type  PageType `json:"type"`

	// Resource is required for every page type except dashboard.
	Resource ID `json:"resource,omitempty"`

	Table     *TableConfig     `json:"table,omitempty"`
	Form      *FormConfig      `json:"form,omitempty"`
	Dashboard *DashboardConfig `json:"dashboard,omitempty"`
}

// TableConfig configures a resource_table page (DESIGN.md §18).
type TableConfig struct {
	Title    string `json:"title,omitempty"`
	Columns  []ID   `json:"columns,omitempty"`
	Search   bool   `json:"search,omitempty"`
	PageSize int    `json:"page_size,omitempty"`
}

// FormConfig configures a resource_form page (DESIGN.md §18).
//
// Layout is structured; there is no absolute positioning.
type FormConfig struct {
	Layout string `json:"layout,omitempty"`
	Fields []ID   `json:"fields,omitempty"`
}

// DashboardConfig configures the MVP dashboard (DESIGN.md §4.4).
//
// There is deliberately no arbitrary chart designer.
type DashboardConfig struct {
	CountCards  []DashboardCard `json:"count_cards,omitempty"`
	RecentLists []DashboardCard `json:"recent_lists,omitempty"`
}

// DashboardCard is a count card or recent-record list bound to a resource.
type DashboardCard struct {
	Label    string `json:"label"`
	Resource ID     `json:"resource"`
	Limit    int    `json:"limit,omitempty"`
}

// NavTarget selects what a navigation entry points at (DESIGN.md §4.5).
type NavTarget string

const (
	NavPage     NavTarget = "page"
	NavExternal NavTarget = "external"
)

// NavItem is one ordered navigation entry.
type NavItem struct {
	ID     ID        `json:"id"`
	Label  string    `json:"label"`
	Target NavTarget `json:"target"`

	Page ID     `json:"page,omitempty"`
	URL  string `json:"url,omitempty"`
}

// Branding carries MVP branding settings (DESIGN.md §19).
type Branding struct {
	AppName     string `json:"app_name,omitempty"`
	LogoRef     string `json:"logo_ref,omitempty"`
	AccentColor string `json:"accent_color,omitempty"`
	Appearance  string `json:"appearance,omitempty"`
}

// FindResource returns the resource with the given ID, or nil.
//
// Nil entries are skipped rather than dereferenced: JSON such as
// "resources":[null,...] decodes successfully, and validation must be able to
// report that as a diagnostic instead of crashing while looking up an
// unrelated reference.
func (s *ProjectSpec) FindResource(id ID) *Resource {
	for _, resource := range s.Resources {
		if resource != nil && resource.ID == id {
			return resource
		}
	}
	return nil
}

// FindPage returns the page with the given ID, or nil. Nil entries are skipped.
func (s *ProjectSpec) FindPage(id ID) *Page {
	for _, page := range s.Pages {
		if page != nil && page.ID == id {
			return page
		}
	}
	return nil
}

// FindField returns the field with the given ID, or nil. Nil entries are skipped.
func (r *Resource) FindField(id ID) *Field {
	for _, field := range r.Fields {
		if field != nil && field.ID == id {
			return field
		}
	}
	return nil
}
