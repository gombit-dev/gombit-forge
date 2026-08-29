package gen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestGeneratedModelsCompile is the issue #6 acceptance criterion: the
// generated models must compile in a real module.
//
// It writes the models into a throwaway module whose go.mod requires the same
// gorm and decimal versions Gombit itself pins, then runs `go build ./...`.
// Skipped in -short and when the module cache cannot resolve the deps offline,
// so it never turns a green suite red on an air-gapped machine — the unit
// tests above still assert the generated text.
func TestGeneratedModelsCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping compile test in -short")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go toolchain not found: %v", err)
	}

	g, _ := buildGraph(t)
	files, err := Models(g)
	if err != nil {
		t.Fatalf("Models: %v", err)
	}

	root := t.TempDir()
	// These versions match Gombit's own go.mod, so a generated app inherits
	// them rather than pulling anything novel (ADR-004 D3).
	writeFile(t, filepath.Join(root, "go.mod"), `module example.com/sample

go 1.25.7

require (
	github.com/shopspring/decimal v1.4.0
	gorm.io/gorm v1.31.2
)
`)

	for _, file := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(file.Path)), string(file.Content))
	}

	// Resolve go.sum from the module cache without reaching the network; if the
	// deps are not cached this is where we skip rather than fail.
	if out, err := runGo(t, root, "GOFLAGS=-mod=mod", "GOPROXY=off"); err != nil {
		t.Skipf("dependencies not available offline (%v):\n%s", err, out)
	}

	if out, err := runGoBuild(t, root); err != nil {
		t.Fatalf("generated models did not compile:\n%s", out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// runGo runs `go mod tidy` with the given extra environment.
func runGo(t *testing.T, dir string, env ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runGoBuild(t *testing.T, dir string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
