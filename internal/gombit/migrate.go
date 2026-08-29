package gombit

import (
	"context"
	"fmt"
	"strings"
)

// Model identifies one GORM model type for migration generation: the package
// import path and the exported type name. Gombit builds an Atlas Program Mode
// loader importing these types to compute the desired schema.
type Model struct {
	ImportPath string
	TypeName   string
}

// spec renders the model as Gombit's `import/path.TypeName` argument form.
func (m Model) spec() string { return m.ImportPath + "." + m.TypeName }

// Validate reports whether the model is well formed.
func (m Model) Validate() error {
	if strings.TrimSpace(m.ImportPath) == "" {
		return fmt.Errorf("gombit: model has no import path")
	}
	if strings.ContainsAny(m.ImportPath, " \t\r\n") {
		return fmt.Errorf("gombit: model import path %q contains whitespace", m.ImportPath)
	}
	if !isExportedGoName(m.TypeName) {
		return fmt.Errorf("gombit: model type name %q is not an exported Go identifier", m.TypeName)
	}
	return nil
}

// MakeMigrationsRequest describes one migration-generation invocation.
//
// It is project-level: every model is declared in a single call, not one
// subprocess per model (ADR-001 §68–69). Gombit merges Models with the
// registry persisted in the migration directory, so re-running after a schema
// change diffs against the previous state and emits a new versioned migration
// rather than restating the whole schema.
type MakeMigrationsRequest struct {
	// Dir is the application directory the command runs in.
	Dir string
	// Name labels the migration (e.g. "initial").
	Name string
	// Driver selects the SQL dialect Atlas diffs against.
	Driver Database
	// Models are the GORM model types the generated schema comprises.
	Models []Model
	// MigrationDir is the migration output directory relative to Dir; empty
	// uses Gombit's default (database/migrations).
	MigrationDir string
}

// Validate reports whether the request is well formed.
func (r MakeMigrationsRequest) Validate() error {
	if r.Dir == "" {
		return fmt.Errorf("gombit: make-migrations request needs a directory")
	}
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("gombit: make-migrations request needs a name")
	}
	switch r.Driver {
	case DatabasePostgres, DatabaseSQLite, DatabaseMySQL:
	default:
		return fmt.Errorf("gombit: unsupported database %q", r.Driver)
	}
	if len(r.Models) == 0 {
		return fmt.Errorf("gombit: make-migrations request declares no models")
	}
	for _, model := range r.Models {
		if err := model.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// MakeMigrations generates a versioned Atlas migration from the declared models
// (DESIGN.md §9 stage 13, §14). Forge never diffs schemas itself — Gombit and
// Atlas own that (P4); this is the coarse project-level invocation.
func (c *CLI) MakeMigrations(ctx context.Context, request MakeMigrationsRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}

	output, err := c.runner()(ctx, request.Dir, c.binary(), makeMigrationsArgs(request)...)
	if err != nil {
		return fmt.Errorf("gombit: make migrations %q: %w: %s",
			request.Name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// makeMigrationsArgs builds the argv for `gombit db makemigrations`.
//
// Kept pure and separate from execution so the command line is testable
// without a toolchain, and so argument order stays deterministic.
func makeMigrationsArgs(request MakeMigrationsRequest) []string {
	args := []string{
		"db", "makemigrations", request.Name,
		"--driver", string(request.Driver),
	}
	if request.MigrationDir != "" {
		args = append(args, "--dir", request.MigrationDir)
	}
	// Models keep their declared order so the command line is deterministic;
	// Gombit sorts the persisted registry itself.
	for _, model := range request.Models {
		args = append(args, "--model", model.spec())
	}
	return args
}

// isExportedGoName reports whether name is an exported Go identifier. It is a
// small local check so the boundary does not depend on the spec package.
func isExportedGoName(name string) bool {
	if name == "" {
		return false
	}
	for index, char := range name {
		switch {
		case char >= 'A' && char <= 'Z':
		case char >= 'a' && char <= 'z':
			if index == 0 {
				return false
			}
		case char >= '0' && char <= '9' || char == '_':
			if index == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
