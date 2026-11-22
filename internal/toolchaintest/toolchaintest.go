// Package toolchaintest gates the tests that drive real external tools (the
// gombit CLI, atlas, go, docker, node).
//
// By default a test whose tools are missing skips, so `go test ./...` passes on
// a machine without them. CI's toolchain job installs every tool and sets
// FORGE_REQUIRE_TOOLCHAIN=1, which turns each of those skips into a failure: a
// broken install, or a gombit too old for Forge, fails the job instead of
// turning it green by skipping everything it exists to run.
//
// It imports nothing from Forge, so internal/gombit's own tests can use it
// without an import cycle.
package toolchaintest

import (
	"os"
	"os/exec"
	"testing"
)

// RequireEnv is the variable that turns a missing toolchain from a skip into a
// failure. Any non-empty value other than "0" enables it.
const RequireEnv = "FORGE_REQUIRE_TOOLCHAIN"

// Required reports whether RequireEnv is set.
func Required() bool {
	v := os.Getenv(RequireEnv)
	return v != "" && v != "0"
}

// Skipf skips t, or fails it when the toolchain is required.
func Skipf(t testing.TB, format string, args ...any) {
	t.Helper()
	if Required() {
		t.Fatalf(RequireEnv+" is set, so this is a failure, not a skip: "+format, args...)
	}
	t.Skipf(format, args...)
}

// RequireBinaries calls Skipf for the first of bins that is not on PATH.
func RequireBinaries(t testing.TB, bins ...string) {
	t.Helper()
	for _, bin := range bins {
		if _, err := exec.LookPath(bin); err != nil {
			Skipf(t, "%s not on PATH: %v", bin, err)
		}
	}
}
