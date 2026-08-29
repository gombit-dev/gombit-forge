package compiler

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gombit-dev/gombit-forge/internal/gombit"
)

// TestM0EndToEnd is the M0 go/no-go gate (DESIGN.md §31–32): from a fixed
// two-resource ProjectSpec, generate a Gombit application, apply its migration
// on Postgres, build and boot it, and confirm the resource and admin routes are
// mounted — with no Forge runtime dependency.
//
// It orchestrates the real toolchain and a throwaway Postgres, so it needs
// gombit, atlas, go and docker, and skips in -short or when any is absent. The
// determinism, structure and per-stage contracts are covered by the unit tests;
// this proves they compose into a running application.
func TestM0EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping end-to-end harness in -short")
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	const module = "example.com/app"
	dir := filepath.Join(t.TempDir(), "app")

	// --- scaffold + generate ------------------------------------------------
	if err := cli.Scaffold(ctx, gombit.ScaffoldRequest{
		Dir: dir, Name: "app", Module: module,
		Database: gombit.DatabasePostgres, Auth: gombit.AuthCookie, UI: gombit.UIMinimal,
		Tidy: true,
	}); err != nil {
		t.Fatalf("scaffold: %v", err)
	}

	s := sampleSpec(t)
	files, err := CompileApp(s, module)
	if err != nil {
		t.Fatalf("compile app: %v", err)
	}
	for _, f := range files {
		writeAppFile(t, dir, f.Path, f.Content)
	}
	// Forge owns the composition root: main wires every resource through
	// RegisterAll and applies migrations out of band, never AutoMigrate (§14).
	// The server no longer registers the scaffold's demo resource; its package
	// stays in the tree (the management CLI still imports it) but is not mounted.
	writeAppFile(t, dir, "cmd/server/main.go", []byte(forgeMainGo))

	runCmd(t, ctx, dir, nil, "go", "mod", "tidy")

	// --- no Forge runtime dependency (D2/P5) --------------------------------
	assertNoForgeDependency(t, dir)

	// --- migration ----------------------------------------------------------
	models, err := MigrationModelsForSpec(s, module)
	if err != nil {
		t.Fatalf("migration models: %v", err)
	}
	if err := cli.MakeMigrations(ctx, gombit.MakeMigrationsRequest{
		Dir: dir, Name: "initial", Driver: gombit.DatabasePostgres, Models: models,
	}); err != nil {
		t.Fatalf("makemigrations: %v", err)
	}

	// --- Postgres + apply ---------------------------------------------------
	pg := startPostgres(t, ctx)
	defer pg.stop()

	env := []string{
		"GOMBIT_DATABASE_DRIVER=postgres",
		"GOMBIT_DATABASE_DSN=" + pg.dsn,
	}
	runCmd(t, ctx, dir, env, gombit.DefaultBinary, "db", "migrate")

	// Migration applied: the tables now exist.
	assertTableExists(t, ctx, pg, "customers")
	assertTableExists(t, ctx, pg, "invoices")

	// --- build + boot -------------------------------------------------------
	bin := filepath.Join(dir, "server")
	runCmd(t, ctx, dir, nil, "go", "build", "-o", bin, "./cmd/server")

	port := freePort(t)
	appEnv := append(env,
		"GOMBIT_HTTP_ADDR=127.0.0.1:"+port,
		"GOMBIT_ENV=production",
	)
	server := startServer(t, ctx, dir, bin, appEnv)
	defer server.stop()

	base := "http://127.0.0.1:" + port
	waitForHTTP(t, base+"/livez")

	// --- routes + admin mounted --------------------------------------------
	// The resource routes exist (200 or an auth 401/403, never 404).
	assertMounted(t, base+"/api/v1/customers")
	assertMounted(t, base+"/api/v1/invoices")
	// The admin data plane is mounted.
	assertMounted(t, base+"/api/v1/admin/meta")

	t.Logf("M0 gate: app built, migrated, booted, and served customers/invoices/admin at %s", base)
}

// forgeMainGo is the composition root Forge owns: it boots the framework and
// wires every generated resource with one RegisterAll call. It deliberately
// does not AutoMigrate — migrations are applied out of band (DESIGN.md §14).
const forgeMainGo = `package main

import (
	"context"
	"log"

	"github.com/gombit-dev/gombit/config"
	"github.com/gombit-dev/gombit/framework"

	forge "example.com/app/internal/forge_generated"
	"example.com/app/internal/platform"
	"example.com/app/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := platform.OpenDatabase(cfg.Database)
	if err != nil {
		log.Fatal(err)
	}
	app, err := framework.New(
		framework.WithConfig(cfg),
		framework.WithDatabase(db),
		framework.WithEmbeddedFrontend(web.FS()),
	)
	if err != nil {
		_ = db.Close()
		log.Fatal(err)
	}
	if err := forge.RegisterAll(app); err != nil {
		log.Fatal(err)
	}
	app.OnStop(func(context.Context) error { return db.Close() })
	if err := framework.Run(app); err != nil {
		log.Fatal(err)
	}
}
`

func writeAppFile(t *testing.T, dir, rel string, content []byte) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func runCmd(t *testing.T, ctx context.Context, dir string, env []string, name string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s:\n%s", name, strings.Join(args, " "), out)
	}
}

// assertNoForgeDependency confirms the generated application has no compile-time
// dependency on Forge (D2/P5): it must keep working if Forge disappears.
func assertNoForgeDependency(t *testing.T, dir string) {
	t.Helper()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".go") && filepath.Base(path) != "go.mod" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte("gombit-forge")) {
			rel, _ := filepath.Rel(dir, path)
			t.Errorf("%s references gombit-forge; the app must not depend on Forge", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

func assertMounted(t *testing.T, url string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Errorf("GET %s: %v", url, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Errorf("GET %s: route not mounted (404)", url)
	}
}

func waitForHTTP(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("app did not become healthy at %s", url)
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}

// serverProc is a running application process.
type serverProc struct {
	cmd *exec.Cmd
	buf *bytes.Buffer
	t   *testing.T
}

func startServer(t *testing.T, ctx context.Context, dir, bin string, env []string) *serverProc {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	buf := &bytes.Buffer{}
	cmd.Stdout, cmd.Stderr = buf, buf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	return &serverProc{cmd: cmd, buf: buf, t: t}
}

func (s *serverProc) stop() {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
	if s.t.Failed() {
		s.t.Logf("server output:\n%s", s.buf.String())
	}
}

// pgContainer is a throwaway Postgres run under docker.
type pgContainer struct {
	id  string
	dsn string
	t   *testing.T
}

func startPostgres(t *testing.T, ctx context.Context) *pgContainer {
	t.Helper()
	port := freePort(t)
	out, err := exec.CommandContext(ctx, "docker", "run", "-d", "--rm",
		"-e", "POSTGRES_PASSWORD=forge",
		"-e", "POSTGRES_DB=app",
		"-p", "127.0.0.1:"+port+":5432",
		"postgres:15",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run postgres:\n%s", out)
	}
	id := strings.TrimSpace(string(out))
	pg := &pgContainer{
		id:  id,
		dsn: fmt.Sprintf("postgres://postgres:forge@127.0.0.1:%s/app?sslmode=disable", port),
		t:   t,
	}

	// Wait for readiness.
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if err := exec.CommandContext(ctx, "docker", "exec", id, "pg_isready", "-U", "postgres").Run(); err == nil {
			// pg_isready can pass a moment before connections are accepted.
			time.Sleep(time.Second)
			return pg
		}
		time.Sleep(500 * time.Millisecond)
	}
	pg.stop()
	t.Fatal("postgres did not become ready")
	return nil
}

func (p *pgContainer) stop() {
	if p.id != "" {
		_ = exec.Command("docker", "stop", p.id).Run()
	}
}

func assertTableExists(t *testing.T, ctx context.Context, pg *pgContainer, table string) {
	t.Helper()
	query := fmt.Sprintf("SELECT to_regclass('public.%s');", table)
	out, err := exec.CommandContext(ctx, "docker", "exec", pg.id,
		"psql", "-U", "postgres", "-d", "app", "-tAc", query).CombinedOutput()
	if err != nil {
		t.Fatalf("psql %s: %v\n%s", table, err, out)
	}
	if strings.TrimSpace(string(out)) != table {
		t.Errorf("table %s was not created by the migration (got %q)", table, strings.TrimSpace(string(out)))
	}
}
