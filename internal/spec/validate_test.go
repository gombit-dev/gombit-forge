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
				s.Pages[1].Form = &FormConfig{Layout: "absolute"}
			},
			wantAny: CodeInvalidPage,
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
