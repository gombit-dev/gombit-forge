package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gombit-dev/gombit-forge/internal/gombit"
)

// scaffoldPlatformDatabase is internal/platform/database.go as `gombit new`
// v0.10.0 writes it (module example.com/app).
const scaffoldPlatformDatabase = `package platform

import (
	"github.com/gombit-dev/gombit/auth"
	"github.com/gombit-dev/gombit/config"
	"github.com/gombit-dev/gombit/database"
	"github.com/gombit-dev/gombit/storage/claims"

	"example.com/app/internal/product"
)

// OpenDatabase opens the SQL database from typed config.
func OpenDatabase(cfg config.DatabaseConfig) (*database.DB, error) {
	return database.Open(cfg)
}

// AutoMigrate runs GORM AutoMigrate for the framework's tables (auth, its
// one-time tokens, and the storage claims that file uploads are owned
// through) and
// feature-package models so the example API can serve before Atlas
// migrations. Framework models must stay in this call: gombit make resource
// collects every AutoMigrate argument as the entire desired Atlas schema.
// gombit db makemigrations builds its desired schema from the persisted
// migration registry instead, and refuses --forget-model for a model still
// listed here, so the two sources never disagree.
func AutoMigrate(db *database.DB) error {
	return db.AutoMigrate(
		&auth.User{},
		&auth.RefreshToken{},
		&auth.Group{},
		&auth.Permission{},
		&auth.OneTimeToken{},
		&auth.AuditEvent{},
		&auth.TOTPFactor{},
		&auth.RecoveryCode{},
		&auth.IdentityLink{},
		&claims.Claim{},
		&product.Product{},
	)
}
`

func forgeModels(names ...string) []gombit.Model {
	models := make([]gombit.Model, 0, len(names))
	for _, name := range names {
		models = append(models, gombit.Model{
			ImportPath: "example.com/app/internal/forge_generated/" + strings.ToLower(name),
			TypeName:   name,
		})
	}
	return models
}

func TestRegisterPlatformModels(t *testing.T) {
	out, err := RegisterPlatformModels([]byte(scaffoldPlatformDatabase), forgeModels("Customer", "Invoice"))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	got := string(out)
	for _, want := range []string{
		`"example.com/app/internal/forge_generated/customer"`,
		`"example.com/app/internal/forge_generated/invoice"`,
		"\t\t&product.Product{},\n\t\t&customer.Customer{},\n\t\t&invoice.Invoice{},\n\t)",
		// The scaffold's own models and doc stay as they were.
		"\t\t&auth.User{},\n",
		"// listed here, so the two sources never disagree.\n" + platformAutoMigrateNote + "func AutoMigrate(",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}

	// Running it again changes nothing: no model or note is added twice.
	again, err := RegisterPlatformModels(out, forgeModels("Customer", "Invoice"))
	if err != nil {
		t.Fatalf("register again: %v", err)
	}
	if string(again) != got {
		t.Errorf("second run changed the file:\n%s", again)
	}
}

// A generated package named like one the file imports is aliased, so the
// scaffold's product package and a Forge resource called product both resolve.
func TestRegisterPlatformModelsAliasesATakenName(t *testing.T) {
	out, err := RegisterPlatformModels([]byte(scaffoldPlatformDatabase), forgeModels("Product", "Database"))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	got := string(out)
	for _, want := range []string{
		`forgeproduct "example.com/app/internal/forge_generated/product"`,
		`forgedatabase "example.com/app/internal/forge_generated/database"`,
		"\t\t&product.Product{},\n\t\t&forgeproduct.Product{},\n\t\t&forgedatabase.Database{},\n\t)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}

	src := strings.Replace(scaffoldPlatformDatabase, "import (\n", "import (\n\tforgeproduct \"example.com/other\"\n", 1)
	if _, err := RegisterPlatformModels([]byte(src), forgeModels("Product")); err == nil {
		t.Error("both names taken: want an error, got nil")
	}
}

func TestRegisterPlatformModelsWithoutAutoMigrate(t *testing.T) {
	src := "package platform\n\nimport \"github.com/gombit-dev/gombit/database\"\n\nfunc Open() (*database.DB, error) { return nil, nil }\n"
	out, err := RegisterPlatformModels([]byte(src), forgeModels("Customer"))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if string(out) != src {
		t.Errorf("a file with no AutoMigrate call must be left as it is, got:\n%s", out)
	}
}

func TestRegisterPlatformModelsRefusesAmbiguousOrBrokenSource(t *testing.T) {
	two := scaffoldPlatformDatabase + "\nfunc Again(db *database.DB) error { return db.AutoMigrate(&auth.User{}) }\n"
	if _, err := RegisterPlatformModels([]byte(two), forgeModels("Customer")); err == nil || !strings.Contains(err.Error(), "2 AutoMigrate calls") {
		t.Errorf("two AutoMigrate calls: err = %v, want the ambiguity reported", err)
	}
	if _, err := RegisterPlatformModels([]byte("package platform\n\nfunc AutoMigrate(\n"), forgeModels("Customer")); err == nil {
		t.Error("unparsable source: want an error, got nil")
	}
}

func TestApplyPlatformModels(t *testing.T) {
	dir := t.TempDir()
	if err := ApplyPlatformModels(dir, forgeModels("Customer")); err != nil {
		t.Fatalf("a tree without the platform file: %v, want nil", err)
	}
	if err := writeRootFile(dir, PlatformDatabasePath, []byte(scaffoldPlatformDatabase)); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPlatformModels(dir, forgeModels("Customer")); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(PlatformDatabasePath)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "&customer.Customer{}") {
		t.Errorf("platform file not updated:\n%s", got)
	}
}

// Sources unlike the scaffold's still get a valid file: a call on one line, a
// single unparenthesized import, or none.
func TestRegisterPlatformModelsOtherLayouts(t *testing.T) {
	for name, src := range map[string]string{
		"one-line call": "package platform\n\nimport (\n\t\"github.com/gombit-dev/gombit/auth\"\n\t\"github.com/gombit-dev/gombit/database\"\n)\n\n// AutoMigrate migrates.\nfunc AutoMigrate(db *database.DB) error { return db.AutoMigrate(&auth.User{}) }\n",
		"single import": "package platform\n\nimport \"github.com/gombit-dev/gombit/database\"\n\n// AutoMigrate migrates.\nfunc AutoMigrate(db *database.DB) error { return db.AutoMigrate() }\n",
		"no import":     "package platform\n\ntype migrator interface{ AutoMigrate(...any) error }\n\n// AutoMigrate migrates.\nfunc AutoMigrate(db migrator) error { return db.AutoMigrate() }\n",
	} {
		t.Run(name, func(t *testing.T) {
			out, err := RegisterPlatformModels([]byte(src), forgeModels("Customer"))
			if err != nil {
				t.Fatalf("register: %v", err)
			}
			got := string(out)
			for _, want := range []string{`"example.com/app/internal/forge_generated/customer"`, "&customer.Customer{}", platformAutoMigrateNote} {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q:\n%s", want, got)
				}
			}
		})
	}
}
