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
	//
	// A date field is Gombit's types.Date. This module stays gombit-free (it
	// drives Gombit only through the CLI), so the module cache has no Gombit to
	// build against offline: a stand-in module provides the package, and the
	// models compile against its Date. The real types.Date is compiled by the
	// gombit-app compile tests (handler_compile_test.go) and the e2e suites.
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/sample\n\ngo 1.25.7\n\nrequire (\n\t"+
		depDecimal+"\n\t"+depGorm+"\n\tgithub.com/gombit-dev/gombit v0.0.0\n)\n\n"+
		"replace github.com/gombit-dev/gombit => ./_gombit\n")
	writeFile(t, filepath.Join(root, "_gombit", "go.mod"), "module github.com/gombit-dev/gombit\n\ngo 1.25.7\n")
	writeFile(t, filepath.Join(root, "_gombit", "types", "date.go"),
		"// Package types stands in for Gombit's types package in the model compile test.\npackage types\n\n"+
			"// Date stands in for Gombit's calendar date.\ntype Date struct{}\n")

	for _, file := range files {
		writeFile(t, filepath.Join(root, filepath.FromSlash(file.Path)), string(file.Content))
	}

	// `go build -mod=mod` resolves the build graph from the module cache and
	// writes go.sum as it goes. GOPROXY=off proves no network is needed and
	// keeps the test hermetic; if a build dependency is genuinely missing this
	// fails, which is the acceptance test doing its job rather than skipping.
	//
	// `go mod tidy` is deliberately avoided: it resolves the *test* graph of
	// every dependency too (e.g. gorm's own gorm.io/driver/sqlite), which this
	// module never caches, so it would fail offline on something unrelated to
	// whether the generated models compile.
	if out, err := runGoBuild(t, root, "GOFLAGS=-mod=mod", "GOPROXY=off"); err != nil {
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

func runGoBuild(t *testing.T, dir string, env ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("go", "build", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
