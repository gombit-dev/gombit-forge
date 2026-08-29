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
// It writes the models into a throwaway module requiring the same gorm and
// decimal versions this module pins, then runs `go build ./...`. Because those
// are real dependencies of gombit-forge (see deps_test.go), they and their
// transitive deps are already in the module cache once this package's own
// tests build, so the throwaway module resolves offline. The test therefore
// runs on CI rather than skipping — it is skipped only in -short or when the
// go toolchain is absent.
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
	// These versions match this module's own go.mod (deps_test.go keeps them
	// honest), so the generated app inherits them from the warm module cache
	// rather than pulling anything novel (ADR-004 D3).
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/sample\n\ngo 1.25.7\n\nrequire (\n\t"+
		depDecimal+"\n\t"+depGorm+"\n)\n")

	for _, file := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(file.Path)), string(file.Content))
	}

	// Populate go.sum from the module cache. GOPROXY=off proves no network is
	// needed; if a required module is genuinely missing this fails, which is
	// the acceptance test doing its job rather than skipping.
	if out, err := runGo(t, root, "GOFLAGS=-mod=mod", "GOPROXY=off"); err != nil {
		t.Fatalf("go mod tidy could not resolve deps from cache:\n%s", out)
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
