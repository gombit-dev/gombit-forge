// Package dbtest provides a throwaway PostgreSQL for the control plane's
// integration tests. The control plane is Postgres-only (DESIGN.md D4) and the
// environment's SQLite is unusable (cgo segfaults), so model and service tests
// run against a real Postgres in Docker, exactly like the M0 e2e harness.
//
// Every entry point skips the test — it does not fail it — when the test is run
// with -short or when Docker is unavailable, so the unit suite stays fast and
// hermetic and CI's merge gate never depends on Docker (see the Makefile's
// cp-test-short).
package dbtest

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/gombit-dev/gombit-forge/controlplane/internal/platform"
)

// Container is a running throwaway Postgres.
type Container struct {
	ID  string
	DSN string
}

// StartPostgres launches a throwaway Postgres under Docker and registers its
// teardown with t.Cleanup. It skips the test when integration infra is absent.
func StartPostgres(t *testing.T) *Container {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping Postgres-backed test in -short")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker not on PATH: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	port := FreePort(t)
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
	c := &Container{
		ID:  id,
		DSN: fmt.Sprintf("postgres://postgres:forge@127.0.0.1:%s/controlplane?sslmode=disable", port),
	}
	t.Cleanup(func() { _ = exec.Command("docker", "stop", id).Run() })

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if err := exec.CommandContext(ctx, "docker", "exec", id, "pg_isready", "-U", "postgres").Run(); err == nil {
			time.Sleep(time.Second) // pg_isready can pass just before connections are accepted
			return c
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("postgres did not become ready")
	return nil
}

// DB starts a throwaway Postgres and returns a GORM handle with the control
// plane's full schema applied via AutoMigrate. AutoMigrate is a test-only
// convenience for standing up a scratch schema; deployment uses Atlas
// migrations (DESIGN.md §14).
func DB(t *testing.T) *gorm.DB {
	t.Helper()
	c := StartPostgres(t)
	db, err := gorm.Open(postgres.Open(c.DSN), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	if err := db.AutoMigrate(platform.Models()...); err != nil {
		t.Fatalf("automigrate schema: %v", err)
	}
	return db
}

// FreePort returns an unused loopback TCP port as a string.
func FreePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}
