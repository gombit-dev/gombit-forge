package spec

import "testing"

func TestValidSpecPasses(t *testing.T) {
	if diagnostics := Validate(validSpec()); diagnostics != nil {
		t.Fatalf("expected valid spec, got:\n%s", diagnostics.Error())
	}
}

// TestValidateRejects covers each invalid category ADR-001 §36 calls out,
// plus the structural rules the generator depends on.
func TestValidateRejects(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ProjectSpec)
		wantAny Code
	}{
		{
			name: "relationship references missing resource",
			mutate: func(s *ProjectSpec) {
				s.Resources[1].Fields[0].Target = fixID(KindResource, "99")
			},
			wantAny: CodeDanglingRef,
		},
		{
			name: "relationship target emptied",
			mutate: func(s *ProjectSpec) {
				s.Resources[1].Fields[0].Target = ""
			},
			wantAny: CodeDanglingRef,
		},
		{
			name: "duplicate stable id across resources",
			mutate: func(s *ProjectSpec) {
				s.Resources[1].ID = s.Resources[0].ID
			},
			wantAny: CodeDuplicateID,
		},
		{
			name: "duplicate stable id across fields",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Fields[1].ID = s.Resources[0].Fields[0].ID
			},
			wantAny: CodeDuplicateID,
		},
		{
			name: "malformed stable id",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].ID = "res_nope"
			},
			wantAny: CodeMalformedID,
		},
		{
			name: "unsupported field type",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Fields[0].Type = "money"
			},
			wantAny: CodeUnknownType,
		},
		{
			name: "invalid storage identifier",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Fields[0].StorageName = "Name With Spaces"
			},
			wantAny: CodeInvalidStorage,
		},
		{
			name: "storage name collides within resource",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Fields[1].StorageName = s.Resources[0].Fields[0].StorageName
			},
			wantAny: CodeDuplicateStore,
		},
		{
			name: "code name is not an exported Go identifier",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Fields[0].CodeName = "name"
			},
			wantAny: CodeInvalidCodeName,
		},
		{
			name: "code name is a Go keyword",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].CodeName = "range"
			},
			wantAny: CodeInvalidCodeName,
		},
		{
			name: "code name collides within resource namespace",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Fields[1].CodeName = s.Resources[0].Fields[0].CodeName
			},
			wantAny: CodeDuplicateCode,
		},
		{
			name: "enum without values",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Fields[3].EnumValues = nil
			},
			wantAny: CodeInvalidEnum,
		},
		{
			name: "duplicate enum value",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Fields[3].EnumValues = []EnumValue{
					{Value: "free"}, {Value: "free"},
				}
			},
			wantAny: CodeInvalidEnum,
		},
		{
			name: "enum values on a non-enum field",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Fields[0].EnumValues = []EnumValue{{Value: "x"}}
			},
			wantAny: CodeInvalidEnum,
		},
		{
			name: "page references missing resource",
			mutate: func(s *ProjectSpec) {
				s.Pages[1].Resource = fixID(KindResource, "99")
			},
			wantAny: CodeDanglingRef,
		},
		{
			name: "table column belongs to another resource",
			mutate: func(s *ProjectSpec) {
				s.Pages[1].Table.Columns = []ID{fixInvoiceTotal}
			},
			wantAny: CodeDanglingRef,
		},
		{
			name: "duplicate page slug",
			mutate: func(s *ProjectSpec) {
				s.Pages[2].Slug = s.Pages[1].Slug
			},
			wantAny: CodeDuplicateSlug,
		},
		{
			name: "invalid page slug",
			mutate: func(s *ProjectSpec) {
				s.Pages[1].Slug = "Not A Slug"
			},
			wantAny: CodeInvalidSlug,
		},
		{
			name: "dashboard must not bind a resource",
			mutate: func(s *ProjectSpec) {
				s.Pages[0].Resource = fixCustomer
			},
			wantAny: CodeInvalidPage,
		},
		{
			name: "dashboard card references missing resource",
			mutate: func(s *ProjectSpec) {
				s.Pages[0].Dashboard.CountCards[0].Resource = fixID(KindResource, "99")
			},
			wantAny: CodeDanglingRef,
		},
		{
			name: "behavior references foreign field",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Behavior.ListFields = []ID{fixInvoiceTotal}
			},
			wantAny: CodeDanglingRef,
		},
		{
			name: "navigation references missing page",
			mutate: func(s *ProjectSpec) {
				s.Navigation[0].Page = fixID(KindPage, "99")
			},
			wantAny: CodeDanglingRef,
		},
		{
			name: "external navigation without http url",
			mutate: func(s *ProjectSpec) {
				s.Navigation[2].URL = "ftp://example.com"
			},
			wantAny: CodeInvalidNav,
		},
		{
			name: "unsupported database driver",
			mutate: func(s *ProjectSpec) {
				s.Database.Driver = "oracle"
			},
			wantAny: CodeInvalidDriver,
		},
		{
			name: "unsupported auth mode",
			mutate: func(s *ProjectSpec) {
				s.Auth.Mode = "saml"
			},
			wantAny: CodeInvalidAuth,
		},
		{
			name: "invalid project slug",
			mutate: func(s *ProjectSpec) {
				s.Project.Slug = "Acme CRM"
			},
			wantAny: CodeInvalidSlug,
		},
		{
			name: "missing resource label",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Label = "   "
			},
			wantAny: CodeMissingLabel,
		},
		{
			name: "unsupported form layout",
			mutate: func(s *ProjectSpec) {
				// Convert the page to a real form page so the only defect is
				// the layout, not a type/config mismatch.
				s.Pages[1].Type = PageResourceForm
				s.Pages[1].Table = nil
				s.Pages[1].Form = &FormConfig{Layout: "absolute"}
			},
			wantAny: CodeInvalidPage,
		},

		// Page type must agree with the attached configuration block,
		// otherwise the domain graph cannot project a single view per page.
		{
			name: "table page carrying a form block",
			mutate: func(s *ProjectSpec) {
				s.Pages[1].Form = &FormConfig{Layout: "single_column"}
			},
			wantAny: CodePageMismatch,
		},
		{
			name: "form page carrying a table block",
			mutate: func(s *ProjectSpec) {
				s.Pages[1].Type = PageResourceForm
			},
			wantAny: CodePageMismatch,
		},
		{
			name: "dashboard page carrying a table block",
			mutate: func(s *ProjectSpec) {
				s.Pages[0].Table = &TableConfig{}
			},
			wantAny: CodePageMismatch,
		},
		{
			name: "detail page carrying a table block",
			mutate: func(s *ProjectSpec) {
				s.Pages[1].Type = PageResourceDetail
			},
			wantAny: CodePageMismatch,
		},
		{
			name: "resource page carrying a dashboard block",
			mutate: func(s *ProjectSpec) {
				s.Pages[1].Dashboard = &DashboardConfig{}
			},
			wantAny: CodePageMismatch,
		},

		// gorm.Model already occupies these names on every generated model.
		{
			name: "code_name reserved by gorm.Model",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Fields[0].CodeName = "ID"
			},
			wantAny: CodeReservedName,
		},
		{
			name: "code_name CreatedAt reserved by gorm.Model",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Fields[0].CodeName = "CreatedAt"
			},
			wantAny: CodeReservedName,
		},
		{
			name: "storage_name reserved by gorm.Model",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Fields[0].StorageName = "id"
			},
			wantAny: CodeReservedName,
		},
		{
			name: "storage_name deleted_at reserved by gorm.Model",
			mutate: func(s *ProjectSpec) {
				s.Resources[0].Fields[0].StorageName = "deleted_at"
			},
			wantAny: CodeReservedName,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := validSpec()
			test.mutate(s)

			diagnostics := Validate(s)
			if diagnostics == nil {
				t.Fatalf("expected diagnostics, spec validated clean")
			}
			if !diagnostics.Has(test.wantAny) {
				t.Errorf("missing expected code %q; got %v\n%s",
					test.wantAny, diagnostics.Codes(), diagnostics.Error())
			}
		})
	}
}

// TestValidateFieldDefault checks a declared default against its field type.
// An unparseable default reaches the generated model and the migration as a
// literal, so it is a spec-validity question, not a presentation detail.
func TestValidateFieldDefault(t *testing.T) {
	tests := []struct {
		name       string
		fieldType  FieldType
		enumValues []EnumValue
		value      string
		wantValid  bool
	}{
		{name: "string accepts anything", fieldType: TypeString, value: "banana", wantValid: true},
		{name: "text accepts anything", fieldType: TypeText, value: "", wantValid: true},

		{name: "integer accepts digits", fieldType: TypeInteger, value: "42", wantValid: true},
		{name: "integer accepts negative", fieldType: TypeInteger, value: "-7", wantValid: true},
		{name: "integer rejects decimal", fieldType: TypeInteger, value: "1.5"},
		{name: "integer rejects words", fieldType: TypeInteger, value: "banana"},

		{name: "decimal accepts fraction", fieldType: TypeDecimal, value: "1.5", wantValid: true},
		{name: "decimal accepts integer form", fieldType: TypeDecimal, value: "2", wantValid: true},
		{name: "decimal rejects words", fieldType: TypeDecimal, value: "banana"},

		{name: "boolean accepts true", fieldType: TypeBoolean, value: "true", wantValid: true},
		{name: "boolean accepts false", fieldType: TypeBoolean, value: "false", wantValid: true},
		{name: "boolean rejects words", fieldType: TypeBoolean, value: "banana"},
		{name: "boolean rejects yes", fieldType: TypeBoolean, value: "yes"},

		{name: "datetime accepts RFC3339", fieldType: TypeDatetime, value: "2026-08-28T10:00:00Z", wantValid: true},
		{name: "datetime rejects date only", fieldType: TypeDatetime, value: "2026-08-28"},
		{name: "datetime rejects words", fieldType: TypeDatetime, value: "banana"},

		{name: "date accepts YYYY-MM-DD", fieldType: TypeDate, value: "2026-08-28", wantValid: true},
		{name: "date rejects datetime", fieldType: TypeDate, value: "2026-08-28T10:00:00Z"},
		{name: "date rejects impossible day", fieldType: TypeDate, value: "2026-02-31"},

		{
			name: "enum accepts a declared value", fieldType: TypeEnum, value: "pro",
			enumValues: []EnumValue{{Value: "free"}, {Value: "pro"}}, wantValid: true,
		},
		{
			name: "enum rejects an undeclared value", fieldType: TypeEnum, value: "enterprise",
			enumValues: []EnumValue{{Value: "free"}, {Value: "pro"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := validSpec()
			field := s.Resources[0].Fields[0]
			field.Type = test.fieldType
			field.EnumValues = test.enumValues
			field.Default = strptr(test.value)

			diagnostics := Validate(s)
			gotValid := !diagnostics.Has(CodeInvalidDefault)

			if gotValid != test.wantValid {
				t.Errorf("default %q on %s: valid=%v want %v\n%s",
					test.value, test.fieldType, gotValid, test.wantValid, diagnostics.Error())
			}
		})
	}
}

func TestValidateRejectsDefaultOnBelongsTo(t *testing.T) {
	s := validSpec()
	s.Resources[1].Fields[0].Default = strptr("anything")

	if diagnostics := Validate(s); !diagnostics.Has(CodeInvalidDefault) {
		t.Errorf("expected a belongs_to default to be rejected, got %v", diagnostics.Codes())
	}
}

// TestValidateReportsAllProblems confirms validation accumulates rather than
// stopping at the first failure, so the editor can show everything at once.
func TestValidateReportsAllProblems(t *testing.T) {
	s := validSpec()
	s.Database.Driver = "oracle"
	s.Auth.Mode = "saml"
	s.Resources[0].Fields[0].Type = "money"

	diagnostics := Validate(s)
	if len(diagnostics) < 3 {
		t.Fatalf("expected at least 3 diagnostics, got %d:\n%s",
			len(diagnostics), diagnostics.Error())
	}
	for _, want := range []Code{CodeInvalidDriver, CodeInvalidAuth, CodeUnknownType} {
		if !diagnostics.Has(want) {
			t.Errorf("missing %q in %v", want, diagnostics.Codes())
		}
	}
}

// TestValidateAllowsSameCodeNameInDifferentResources encodes ADR-001 §7: two
// resources may each legitimately expose Email; only same-namespace
// collisions are errors.
func TestValidateAllowsSameCodeNameInDifferentResources(t *testing.T) {
	s := validSpec()
	s.Resources[1].Fields[1].CodeName = "Email"
	s.Resources[1].Fields[1].StorageName = "email"

	if diagnostics := Validate(s); diagnostics != nil {
		t.Fatalf("cross-resource symbol reuse should be legal, got:\n%s", diagnostics.Error())
	}
}

func TestValidateNilSpec(t *testing.T) {
	if diagnostics := Validate(nil); diagnostics == nil {
		t.Fatal("expected diagnostics for nil spec")
	}
}

func TestNameHelpers(t *testing.T) {
	identTests := map[string]bool{
		"Email": true, "ContactEmail": true, "Email2": true, "TwoFactorEnabled": true,
		"email": false, "2Factor": false, "": false, "range": false,
		"Contact Email": false, "Contact-Email": false, "_Email": false,
	}
	for input, want := range identTests {
		if got := IsExportedGoIdent(input); got != want {
			t.Errorf("IsExportedGoIdent(%q) = %v, want %v", input, got, want)
		}
	}

	storageTests := map[string]bool{
		"email": true, "contact_email": true, "line_1": true,
		"Email": false, "contact__email": false, "contact_": false,
		"_email": false, "1st": false, "": false, "type": true,
	}
	for input, want := range storageTests {
		if got := IsStorageName(input); got != want {
			t.Errorf("IsStorageName(%q) = %v, want %v", input, got, want)
		}
	}

	slugTests := map[string]bool{
		"customers": true, "acme-crm": true, "v2-invoices": true,
		"Customers": false, "acme--crm": false, "-crm": false, "crm-": false, "": false,
	}
	for input, want := range slugTests {
		if got := IsSlug(input); got != want {
			t.Errorf("IsSlug(%q) = %v, want %v", input, got, want)
		}
	}
}
