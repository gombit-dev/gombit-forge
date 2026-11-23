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
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
)

// RequireEnv is the variable that turns a missing toolchain from a skip into a
// failure. It is a boolean in strconv.ParseBool's spelling ("1", "true", "0",
// "false", ...). Unset or empty, it is off. Any other value counts as on, so a
// typo can't quietly switch the requirement off in the lane that sets it.
const RequireEnv = "FORGE_REQUIRE_TOOLCHAIN"

// Required reports whether RequireEnv asks for the toolchain.
func Required() bool {
	v := os.Getenv(RequireEnv)
	if v == "" {
		return false
	}
	on, err := strconv.ParseBool(v)
	return on || err != nil
}

// Skipf skips t, or fails it when the toolchain is required.
func Skipf(t testing.TB, format string, args ...any) {
	t.Helper()
	if Required() {
		t.Fatalf("%s, so this is a failure, not a skip: %s", requiredBecause(), fmt.Sprintf(format, args...))
	}
	t.Skipf(format, args...)
}

// requiredBecause names the setting that made the toolchain required, and says
// so when the value isn't a boolean.
func requiredBecause() string {
	v := os.Getenv(RequireEnv)
	if _, err := strconv.ParseBool(v); err != nil {
		return fmt.Sprintf("%s=%q is not a boolean, which counts as set", RequireEnv, v)
	}
	return fmt.Sprintf("%s=%s", RequireEnv, v)
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
