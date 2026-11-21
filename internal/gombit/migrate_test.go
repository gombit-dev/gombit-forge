package gombit

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func validMigrateRequest(dir string) MakeMigrationsRequest {
	return MakeMigrationsRequest{
		Dir:    dir,
		Name:   "initial",
		Driver: DatabasePostgres,
		Models: []Model{
			{ImportPath: "example.com/app/internal/forge_generated/customer", TypeName: "Customer"},
			{ImportPath: "example.com/app/internal/forge_generated/invoice", TypeName: "Invoice"},
		},
	}
}

// migrateCLI is a CLI whose toolchain reports version, recording its calls.
func migrateCLI(version string) (*recorder, *CLI) {
	rec := &recorder{outputs: map[string]string{"version": "gombit:   " + version + "\n"}}
	return rec, &CLI{Binary: "gombit", Run: rec.run}
}

// makemigrationsCall returns the one `db makemigrations` invocation, after
// the version probe.
func makemigrationsCall(t *testing.T, rec *recorder) string {
	t.Helper()
	if len(rec.calls) != 2 || rec.calls[0][1] != "version" {
		t.Fatalf("expected the version probe then one coarse invocation, got %v", rec.calls)
	}
	return strings.Join(rec.calls[1], " ")
}

func TestMakeMigrationsArgs(t *testing.T) {
	rec, cli := migrateCLI("v0.8.0")

	if err := cli.MakeMigrations(context.Background(), validMigrateRequest("/tmp/app")); err != nil {
		t.Fatalf("make migrations: %v", err)
	}

	got := makemigrationsCall(t, rec)
	want := "gombit db makemigrations initial --driver postgres " +
		"--model example.com/app/internal/forge_generated/customer.Customer " +
		"--model example.com/app/internal/forge_generated/invoice.Invoice"
	if got != want {
		t.Errorf("argv:\n got: %s\nwant: %s", got, want)
	}
	if rec.dirs[1] != "/tmp/app" {
		t.Errorf("command must run in the app dir, got %q", rec.dirs[1])
	}
}

// TestMakeMigrationsAddsFrameworkSchemaModels pins the v0.9.0 auth tables onto
// the request: `gombit new` v0.9.0 leaves them out of the scaffold's
// migration registry, and a Forge app never AutoMigrates, so without them the
// app's schema lacks auth_totp_factors and every login is a 500.
func TestMakeMigrationsAddsFrameworkSchemaModels(t *testing.T) {
	rec, cli := migrateCLI("v0.9.0")

	req := validMigrateRequest("/tmp/app")
	// One already declared: it must not be passed twice.
	req.Models = append(req.Models, Model{ImportPath: ModulePath + "/auth", TypeName: "AuditEvent"})
	if err := cli.MakeMigrations(context.Background(), req); err != nil {
		t.Fatalf("make migrations: %v", err)
	}

	got := makemigrationsCall(t, rec)
	want := "gombit db makemigrations initial --driver postgres " +
		"--model example.com/app/internal/forge_generated/customer.Customer " +
		"--model example.com/app/internal/forge_generated/invoice.Invoice " +
		"--model github.com/gombit-dev/gombit/auth.AuditEvent " +
		"--model github.com/gombit-dev/gombit/auth.OneTimeToken " +
		"--model github.com/gombit-dev/gombit/auth.TOTPFactor " +
		"--model github.com/gombit-dev/gombit/auth.RecoveryCode " +
		"--model github.com/gombit-dev/gombit/auth.IdentityLink"
	if got != want {
		t.Errorf("argv:\n got: %s\nwant: %s", got, want)
	}
	if len(req.Models) != 3 {
		t.Errorf("the caller's request must not be modified, got %d models", len(req.Models))
	}
}

// TestMakeMigrationsAddsNoFrameworkModelsFromV010: `gombit new` v0.10.0
// records every framework model in the registry (gombit #675), so the request
// is passed as declared, as it is before v0.9.0.
func TestMakeMigrationsAddsNoFrameworkModelsFromV010(t *testing.T) {
	for _, version := range []string{"v0.8.0", "v0.10.0", "v0.11.2", "v1.0.0"} {
		t.Run(version, func(t *testing.T) {
			rec, cli := migrateCLI(version)
			if err := cli.MakeMigrations(context.Background(), validMigrateRequest("/tmp/app")); err != nil {
				t.Fatalf("make migrations: %v", err)
			}
			got := makemigrationsCall(t, rec)
			want := "gombit db makemigrations initial --driver postgres " +
				"--model example.com/app/internal/forge_generated/customer.Customer " +
				"--model example.com/app/internal/forge_generated/invoice.Invoice"
			if got != want {
				t.Errorf("argv:\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

func TestMakeMigrationsRefusesUnsupportedToolchain(t *testing.T) {
	rec, cli := migrateCLI("v0.7.1")

	if err := cli.MakeMigrations(context.Background(), validMigrateRequest("/tmp/app")); err == nil {
		t.Fatal("expected an unsupported toolchain to be refused")
	}
	if len(rec.calls) != 1 {
		t.Errorf("only the version probe may run, got %v", rec.calls)
	}
}

func TestMakeMigrationsCustomDir(t *testing.T) {
	rec, cli := migrateCLI("v0.8.0")

	req := validMigrateRequest("/tmp/app")
	req.MigrationDir = "db/migrations"
	if err := cli.MakeMigrations(context.Background(), req); err != nil {
		t.Fatalf("make migrations: %v", err)
	}
	if got := makemigrationsCall(t, rec); !strings.Contains(got, "--dir db/migrations") {
		t.Errorf("custom migration dir not passed: %v", got)
	}
}

func TestMakeMigrationsForgetModels(t *testing.T) {
	rec, cli := migrateCLI("v0.8.0")

	req := validMigrateRequest("/tmp/app")
	req.ForgetModels = []Model{
		{ImportPath: "example.com/app/internal/forge_generated/legacy", TypeName: "Legacy"},
	}
	if err := cli.MakeMigrations(context.Background(), req); err != nil {
		t.Fatalf("make migrations: %v", err)
	}
	got := makemigrationsCall(t, rec)
	if !strings.Contains(got, "--forget-model example.com/app/internal/forge_generated/legacy.Legacy") {
		t.Errorf("forget-model not passed: %s", got)
	}
	// A request that only forgets is valid.
	_, cli2 := migrateCLI("v0.8.0")
	onlyForget := MakeMigrationsRequest{
		Dir: "/tmp/app", Name: "drop", Driver: DatabasePostgres,
		ForgetModels: req.ForgetModels,
	}
	if err := cli2.MakeMigrations(context.Background(), onlyForget); err != nil {
		t.Errorf("a forget-only request should be valid: %v", err)
	}
}

func TestMakeMigrationsValidates(t *testing.T) {
	tests := map[string]func(*MakeMigrationsRequest){
		"no dir":            func(r *MakeMigrationsRequest) { r.Dir = "" },
		"no name":           func(r *MakeMigrationsRequest) { r.Name = "" },
		"bad driver":        func(r *MakeMigrationsRequest) { r.Driver = "oracle" },
		"no models":         func(r *MakeMigrationsRequest) { r.Models = nil },
		"model no import":   func(r *MakeMigrationsRequest) { r.Models[0].ImportPath = "" },
		"model bad type":    func(r *MakeMigrationsRequest) { r.Models[0].TypeName = "lowercase" },
		"import whitespace": func(r *MakeMigrationsRequest) { r.Models[0].ImportPath = "bad path" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{}
			cli := &CLI{Binary: "gombit", Run: rec.run}

			req := validMigrateRequest("/tmp/app")
			mutate(&req)
			if err := cli.MakeMigrations(context.Background(), req); err == nil {
				t.Fatal("expected validation to reject the request")
			}
			if len(rec.calls) != 0 {
				t.Errorf("a rejected request must not reach the toolchain, got %v", rec.calls)
			}
		})
	}
}

func TestMakeMigrationsSurfacesToolchainOutput(t *testing.T) {
	rec := &recorder{err: errors.New("exit status 1")}
	cli := &CLI{Binary: "gombit", Run: rec.run}

	err := cli.MakeMigrations(context.Background(), validMigrateRequest("/tmp/app"))
	if err == nil {
		t.Fatal("expected the failure to propagate")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error should include toolchain output: %v", err)
	}
}

func TestModelSpec(t *testing.T) {
	m := Model{ImportPath: "example.com/app/internal/forge_generated/order_line", TypeName: "OrderLine"}
	if got, want := m.spec(), "example.com/app/internal/forge_generated/order_line.OrderLine"; got != want {
		t.Errorf("spec(): got %q want %q", got, want)
	}
}
