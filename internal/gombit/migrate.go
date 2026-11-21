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
// subprocess per model (ADR-001 §68–69).
//
// The model set is additive, matching Gombit's registry contract, not a
// replacement of the desired schema. Gombit unions Models with the registry
// persisted in the migration directory (database/migrations/models.json) and
// removes ForgetModels; the net registry is the desired schema Atlas diffs
// against. So:
//
//   - re-declaring the same Models after a field change diffs against the
//     previous state and emits a new versioned migration;
//   - a model already registered (including whatever `gombit new` seeded)
//     stays in the schema unless named in ForgetModels;
//   - dropping a resource's table requires putting its model in ForgetModels —
//     listing only the survivors in Models does not drop it.
//
// Making Models the whole desired schema (computing ForgetModels from the
// registry as resources are deleted) is future work tied to resource-deletion
// semantics (ADR-001 §45–46); this boundary exposes the additive contract
// faithfully rather than hiding it.
type MakeMigrationsRequest struct {
	// Dir is the application directory the command runs in.
	Dir string
	// Name labels the migration (e.g. "initial").
	Name string
	// Driver selects the SQL dialect Atlas diffs against.
	Driver Database
	// Models are the GORM model types to add to (or refresh in) the migration
	// registry. Merged with the persisted registry — not the entire desired
	// schema by themselves.
	Models []Model
	// ForgetModels are model types to remove from the registry, proposing a
	// DROP for each one's table.
	ForgetModels []Model
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
	if len(r.Models) == 0 && len(r.ForgetModels) == 0 {
		return fmt.Errorf("gombit: make-migrations request declares no models")
	}
	for _, model := range r.Models {
		if err := model.Validate(); err != nil {
			return err
		}
	}
	for _, model := range r.ForgetModels {
		if err := model.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// MakeMigrations generates a versioned Atlas migration from the declared models
// (DESIGN.md §9 stage 13, §14). Forge never diffs schemas itself — Gombit and
// Atlas own that (P4); this is the coarse project-level invocation.
//
// The request gains the framework models the scaffold's migration registry
// omits for the detected toolchain (see frameworkSchemaModels), so the
// migration carries the whole schema the generated app runs against.
func (c *CLI) MakeMigrations(ctx context.Context, request MakeMigrationsRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}

	detected, err := c.Version(ctx)
	if err != nil {
		return err
	}
	if !c.SkipVersionCheck {
		if err := CheckSupported(detected); err != nil {
			return err
		}
	}
	request.Models = withModels(request.Models, frameworkSchemaModels(detected))

	output, err := c.runner()(ctx, request.Dir, c.binary(), makeMigrationsArgs(request)...)
	if err != nil {
		return fmt.Errorf("gombit: make migrations %q: %w: %s",
			request.Name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// frameworkSchemaModels returns the framework models a generated app's
// migration must declare for toolchain version v, beyond what `gombit new`
// records in the scaffold's migration registry.
//
// gombit v0.9.0 added five auth tables (one-time tokens for email
// verification and password reset, the security audit trail, TOTP factors,
// recovery codes and external identity links): auth.Models() returns them and
// the scaffold's AutoMigrate lists them, but `gombit new` seeds
// database/migrations/models.json and the bootstrap migration without them.
// An ordinary Gombit app does not notice, because it AutoMigrates at start.
// A Forge app never AutoMigrates (DESIGN.md §14); its schema is the migration
// alone, so without these the tables are missing and every login is a 500
// (the framework reads auth_totp_factors on each one).
//
// gombit v0.10.0 records every framework model in the registry itself
// (gombit #675), so this applies to v0.9.x only, and goes once
// MinimumVersion passes it.
func frameworkSchemaModels(v Version) []Model {
	if !v.AtLeast(Version{Major: 0, Minor: 9, Patch: 0}) || v.AtLeast(Version{Major: 0, Minor: 10, Patch: 0}) {
		return nil
	}
	const auth = ModulePath + "/auth"
	return []Model{
		{ImportPath: auth, TypeName: "OneTimeToken"},
		{ImportPath: auth, TypeName: "AuditEvent"},
		{ImportPath: auth, TypeName: "TOTPFactor"},
		{ImportPath: auth, TypeName: "RecoveryCode"},
		{ImportPath: auth, TypeName: "IdentityLink"},
	}
}

// withModels appends extra to models, skipping any already declared, and
// keeps the declared order first so the command line stays deterministic.
func withModels(models, extra []Model) []Model {
	if len(extra) == 0 {
		return models
	}
	seen := make(map[string]bool, len(models)+len(extra))
	out := make([]Model, 0, len(models)+len(extra))
	for _, model := range append(append([]Model(nil), models...), extra...) {
		if seen[model.spec()] {
			continue
		}
		seen[model.spec()] = true
		out = append(out, model)
	}
	return out
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
	for _, model := range request.ForgetModels {
		args = append(args, "--forget-model", model.spec())
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
