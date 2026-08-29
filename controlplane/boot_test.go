package controlplane_test

// Boot test for the control-plane bootstrap (issue #35): build the server,
// point it at a throwaway Postgres, boot it in cookie-auth mode, and prove over
// HTTP that (1) it is alive and (2) the admin plane is mounted and gated by the
// session cookie. This is the dogfooding claim of DESIGN.md §6/D7 reduced to
// something a CI machine can check: Forge's control plane is an ordinary Gombit
// application, standing up with the framework's own cookie auth and admin.
//
// Like the M0 end-to-end harness it drives real infrastructure, so it needs go
// and docker and skips in -short or when either is absent. It deliberately does
// not exercise login: that needs the User table, which arrives with #36. A 401
// (not a 404) from the admin endpoint is the honest bootstrap signal — the
// route exists and the cookie gate is active — without any tables.

import (
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
)

func TestControlPlaneBoots(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping control-plane boot harness in -short")
	}
	for _, bin := range []string{"go", "docker"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH: %v", bin, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Build the server binary from this module.
	moduleDir := repoDir(t)
	bin := filepath.Join(t.TempDir(), "server")
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, "./cmd/server")
	build.Dir = moduleDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/server:\n%s", out)
	}

	pg := startPostgres(t, ctx)
	defer pg.stop()

	port := freePort(t)
	env := append(os.Environ(),
		"GOMBIT_DATABASE_DRIVER=postgres",
		"GOMBIT_DATABASE_DSN="+pg.dsn,
		// Cookie mode: the secret signs sessions and the CSRF token. Cookies are
		// insecure because the test speaks plain HTTP.
		"GOMBIT_JWT_SECRET=control-plane-boot-secret-please-change-0001",
		"GOMBIT_AUTH_MODE=cookie",
		"GOMBIT_COOKIE_SECURE=false",
		"GOMBIT_COOKIE_SAMESITE=lax",
		"GOMBIT_HTTP_ADDR=127.0.0.1:"+port,
	)

	srv := startServer(t, ctx, bin, env)
	defer srv.stop()

	base := "http://127.0.0.1:" + port
	waitForHTTP(t, base+"/livez")

	// The app is alive.
	if code := statusOf(t, base+"/livez"); code != http.StatusOK {
		t.Errorf("GET /livez = %d, want 200", code)
	}

	// #35's acceptance path: /admin/. In cookie mode framework.New mounts the
	// framework-owned admin SPA (gombit's internal/adminui) at /admin/,
	// independently of the app's own frontend — so the control plane serves it
	// now, with no embed of ours. This asserts the criterion directly, not a
	// proxy for it.
	adminCode, adminType := getWithContentType(t, base+"/admin/")
	if adminCode != http.StatusOK {
		t.Errorf("GET /admin/ = %d, want 200 (framework admin SPA)", adminCode)
	}
	if !strings.Contains(adminType, "text/html") {
		t.Errorf("GET /admin/ Content-Type = %q, want text/html", adminType)
	}

	// The admin data plane is mounted and gated by the session cookie:
	// unauthenticated it must be rejected, not missing. A 404 would mean the
	// plane never mounted; a 200 would mean the cookie gate is off.
	code := statusOf(t, base+"/api/v1/admin/meta")
	if code == http.StatusNotFound {
		t.Errorf("GET /api/v1/admin/meta = 404: admin data plane not mounted")
	}
	if code != http.StatusUnauthorized {
		t.Errorf("GET /api/v1/admin/meta = %d, want 401 (cookie auth gate active)", code)
	}
}

// getWithContentType GETs url and returns its status code and Content-Type.
func getWithContentType(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("Content-Type")
}

// repoDir returns this module's root (the directory holding go.mod).
func repoDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}

func statusOf(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
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

// serverProc is a running control-plane process.
type serverProc struct {
	cmd *exec.Cmd
	buf *strings.Builder
	t   *testing.T
}

func startServer(t *testing.T, ctx context.Context, bin string, env []string) *serverProc {
	t.Helper()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = env
	buf := &strings.Builder{}
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
		"-e", "POSTGRES_DB=controlplane",
		"-p", "127.0.0.1:"+port+":5432",
		"postgres:15",
	).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run postgres:\n%s", out)
	}
	id := strings.TrimSpace(string(out))
	pg := &pgContainer{
		id:  id,
		dsn: fmt.Sprintf("postgres://postgres:forge@127.0.0.1:%s/controlplane?sslmode=disable", port),
		t:   t,
	}

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if err := exec.CommandContext(ctx, "docker", "exec", id, "pg_isready", "-U", "postgres").Run(); err == nil {
			time.Sleep(time.Second) // pg_isready can pass just before connections are accepted
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
