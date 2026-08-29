package compiler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gombit-dev/gombit-forge/internal/gombit"
	"github.com/gombit-dev/gombit-forge/internal/spec"
)

// TestMakeMigrationsGeneratesVersionedSQL is the issue #11 acceptance check:
// a versioned Atlas migration is generated from the compiled models, and adding
// a field produces a new migration.
//
// It scaffolds a real Gombit app, writes the compiled tree in, and drives
// `gombit db makemigrations` — the same delegation a build worker performs. The
// migration diff is computed by Atlas via Gombit, not by Forge (P4). Postgres
// is the managed target (D4); Atlas computes the diff against a throwaway dev
// database Gombit points at docker://postgres, so the test needs docker but not
// a standing server. Applying against a live database is the #12 harness.
//
// Requires installed gombit, atlas, go and docker; skips in -short or when any
// is absent, like the other toolchain integration tests.
func TestMakeMigrationsGeneratesVersionedSQL(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping migration integration test in -short")
	}
	for _, bin := range []string{gombit.DefaultBinary, "atlas", "go", "docker"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH: %v", bin, err)
		}
	}

	cli := &gombit.CLI{}
	version, err := cli.Version(context.Background())
	if err != nil {
		t.Fatalf("gombit version: %v", err)
	}
	if err := gombit.CheckSupported(version); err != nil {
		t.Skipf("installed toolchain unsupported: %v", err)
	}

	const module = "example.com/app"
	dir := filepath.Join(t.TempDir(), "app")

	// Scaffold a Postgres app and resolve its module graph.
	scaffold := gombit.ScaffoldRequest{
		Dir: dir, Name: "app", Module: module,
		Database: gombit.DatabasePostgres, Auth: gombit.AuthCookie, UI: gombit.UIMinimal,
		Tidy: true,
	}
	if err := cli.Scaffold(context.Background(), scaffold); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	s := sampleSpec(t)
	writeCompiled(t, dir, s)
	tidy(t, dir)

	models, err := MigrationModelsForSpec(s, module)
	if err != nil {
		t.Fatalf("migration models: %v", err)
	}

	// First migration.
	if err := cli.MakeMigrations(context.Background(), gombit.MakeMigrationsRequest{
		Dir: dir, Name: "initial", Driver: gombit.DatabasePostgres, Models: models,
	}); err != nil {
		t.Fatalf("makemigrations initial: %v", err)
	}
	initial := migrationSQLFiles(t, dir)
	if len(initial) == 0 {
		t.Fatal("no versioned migration was generated")
	}

	// Add a field, recompile, and generate again: a new migration must appear.
	s.Resources[0].Fields = append(s.Resources[0].Fields, &spec.Field{
		ID: spec.MustNewID(spec.KindField), Label: "Nickname", Type: spec.TypeString,
		CodeName: "Nickname", StorageName: "nickname",
	})
	if d := spec.Validate(s); d != nil {
		t.Fatalf("mutated spec invalid: %s", d.Error())
	}
	writeCompiled(t, dir, s)

	if err := cli.MakeMigrations(context.Background(), gombit.MakeMigrationsRequest{
		Dir: dir, Name: "add_nickname", Driver: gombit.DatabasePostgres, Models: models,
	}); err != nil {
		t.Fatalf("makemigrations add_nickname: %v", err)
	}
	after := migrationSQLFiles(t, dir)
	if len(after) <= len(initial) {
		t.Fatalf("adding a field did not produce a new migration: %d then %d", len(initial), len(after))
	}

	// The new migration should reference the added column.
	newest := after[len(after)-1]
	body, err := os.ReadFile(filepath.Join(dir, "database", "migrations", newest))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if !strings.Contains(strings.ToLower(string(body)), "nickname") {
		t.Errorf("new migration does not mention the added column:\n%s", body)
	}
}

func writeCompiled(t *testing.T, dir string, s *spec.ProjectSpec) {
	t.Helper()
	files, err := Compile(s)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, file := range files {
		full := filepath.Join(dir, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, file.Content, 0o644); err != nil {
			t.Fatalf("write %s: %v", file.Path, err)
		}
	}
}

func tidy(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy:\n%s", out)
	}
}

// migrationSQLFiles returns the sorted .sql migration file names in the app.
func migrationSQLFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "database", "migrations"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read migrations dir: %v", err)
	}
	var files []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}
	return files
}
