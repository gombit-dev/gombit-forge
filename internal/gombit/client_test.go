package gombit

import (
	"strings"
	"testing"

	"github.com/gombit-dev/gombit-forge/internal/spec"
)

func minimalSpec(t *testing.T) *spec.ProjectSpec {
	t.Helper()

	s := &spec.ProjectSpec{
		SpecVersion: spec.SpecVersion,
		Project: spec.Project{
			ID:   spec.MustNewID(spec.KindProject),
			Name: "Acme CRM",
			Slug: "acme-crm",
		},
		Database: spec.Database{Driver: spec.DriverPostgres},
		Auth:     spec.Auth{Mode: spec.AuthCookie},
	}

	if diagnostics := spec.Validate(s); diagnostics != nil {
		t.Fatalf("fixture spec is invalid:\n%s", diagnostics.Error())
	}
	return s
}

func TestScaffoldRequestFor(t *testing.T) {
	s := minimalSpec(t)

	request, err := ScaffoldRequestFor(s, "/tmp/build/acme-crm", "example.com/acme")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	if request.Database != DatabasePostgres {
		t.Errorf("database: got %q", request.Database)
	}
	if request.Auth != AuthCookie {
		t.Errorf("auth: got %q", request.Auth)
	}
	// Forge supersedes the scaffolded CRUD screens with its own generated
	// pages, so the shell only needs the headless preset.
	if request.UI != UIMinimal {
		t.Errorf("ui: got %q want %q", request.UI, UIMinimal)
	}
	if request.Name != "acme-crm" {
		t.Errorf("name: got %q", request.Name)
	}
	if request.Module != "example.com/acme" {
		t.Errorf("module: got %q", request.Module)
	}

	if err := request.Validate(); err != nil {
		t.Errorf("derived request should be valid: %v", err)
	}
}

// TestScaffoldRequestForMapsEveryDriverAndAuthMode keeps the translation total:
// spec validation admits these, so the boundary must not reject them.
func TestScaffoldRequestForMapsEveryDriverAndAuthMode(t *testing.T) {
	drivers := map[spec.DatabaseDriver]Database{
		spec.DriverPostgres: DatabasePostgres,
		spec.DriverSQLite:   DatabaseSQLite,
		spec.DriverMySQL:    DatabaseMySQL,
	}
	for driver, want := range drivers {
		t.Run("driver "+string(driver), func(t *testing.T) {
			s := minimalSpec(t)
			s.Database.Driver = driver

			request, err := ScaffoldRequestFor(s, "/tmp/x", "example.com/x")
			if err != nil {
				t.Fatalf("derive: %v", err)
			}
			if request.Database != want {
				t.Errorf("got %q want %q", request.Database, want)
			}
		})
	}

	modes := map[spec.AuthMode]Auth{
		spec.AuthCookie: AuthCookie,
		spec.AuthJWT:    AuthJWT,
		spec.AuthNone:   AuthNone,
	}
	for mode, want := range modes {
		t.Run("auth "+string(mode), func(t *testing.T) {
			s := minimalSpec(t)
			s.Auth.Mode = mode

			request, err := ScaffoldRequestFor(s, "/tmp/x", "example.com/x")
			if err != nil {
				t.Fatalf("derive: %v", err)
			}
			if request.Auth != want {
				t.Errorf("got %q want %q", request.Auth, want)
			}
		})
	}
}

// TestScaffoldRequestForRefusesInvalidSpec keeps a malformed project from
// reaching the toolchain at all.
func TestScaffoldRequestForRefusesInvalidSpec(t *testing.T) {
	s := minimalSpec(t)
	s.Project.Slug = "Not A Slug"

	if _, err := ScaffoldRequestFor(s, "/tmp/x", "example.com/x"); err == nil {
		t.Fatal("expected an invalid spec to be refused")
	} else if !strings.Contains(err.Error(), "invalid spec") {
		t.Errorf("error should name the cause: %v", err)
	}
}
