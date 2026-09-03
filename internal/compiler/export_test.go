package compiler

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeTree materializes a map of relative path → content under a temp dir,
// creating parent directories, and returns the dir.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return dir
}

// exportEntries returns the archive entry names (sorted) and a name→content map.
func exportEntries(t *testing.T, data []byte) ([]string, map[string]string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	names := make([]string, 0, len(zr.File))
	content := map[string]string{}
	for _, f := range zr.File {
		names = append(names, f.Name)
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", f.Name, err)
		}
		buf := new(bytes.Buffer)
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatalf("read entry %s: %v", f.Name, err)
		}
		rc.Close()
		content[f.Name] = buf.String()
	}
	sort.Strings(names)
	return names, content
}

// a representative §28 Gombit repo tree, plus excluded dirs.
func sampleRepo() map[string]string {
	return map[string]string{
		"go.mod":             "module example.com/app\n",
		"gombit.yaml":        "name: app\n",
		"README.md":          "# App\n",
		"cmd/server/main.go": "package main\n",
		"internal/forge_generated/customer/model.go": "package customer\n",
		"internal/extensions/customer/hooks.go":      "package customer // user code\n",
		"frontend/src/forge_generated/resources.tsx": "export const x = 1;\n",
		"database/migrations/0001.sql":               "-- sql\n",
		"config/app.yaml":                            "k: v\n",
		"docs/README.md":                             "docs\n",
		// Excluded: VCS and build/dependency output.
		".git/HEAD":                    "ref: refs/heads/main\n",
		"node_modules/dep/index.js":    "module.exports = {}\n",
		"frontend/node_modules/x/i.js": "x\n",
		"frontend/dist/bundle.js":      "bundled\n",
	}
}

func TestExportIncludesFullTreeAndExtensions(t *testing.T) {
	dir := writeTree(t, sampleRepo())
	var buf bytes.Buffer
	if err := Export(dir, &buf); err != nil {
		t.Fatalf("export: %v", err)
	}
	names, content := exportEntries(t, buf.Bytes())

	// The full §28 tree, including user extensions, is present.
	for _, want := range []string{
		"go.mod", "gombit.yaml", "README.md",
		"cmd/server/main.go",
		"internal/forge_generated/customer/model.go",
		"internal/extensions/customer/hooks.go",
		"frontend/src/forge_generated/resources.tsx",
		"database/migrations/0001.sql",
		"config/app.yaml",
		"docs/README.md",
	} {
		if _, ok := content[want]; !ok {
			t.Errorf("export missing %s; got %v", want, names)
		}
	}
	// User extension content is preserved verbatim (it is user-owned, §72).
	if content["internal/extensions/customer/hooks.go"] != "package customer // user code\n" {
		t.Error("extension content must be exported verbatim")
	}
}

func TestExportExcludesVCSAndBuildOutput(t *testing.T) {
	dir := writeTree(t, sampleRepo())
	var buf bytes.Buffer
	if err := Export(dir, &buf); err != nil {
		t.Fatalf("export: %v", err)
	}
	names, _ := exportEntries(t, buf.Bytes())
	for _, n := range names {
		for _, bad := range []string{".git/", "node_modules/", "/dist/", "dist/"} {
			if strings.Contains(n, bad) {
				t.Errorf("export must exclude %q; found %s", bad, n)
			}
		}
	}
}

// TestExportIsDeterministic: the same tree exports to byte-identical archives.
func TestExportIsDeterministic(t *testing.T) {
	dir := writeTree(t, sampleRepo())
	var a, b bytes.Buffer
	if err := Export(dir, &a); err != nil {
		t.Fatalf("export a: %v", err)
	}
	if err := Export(dir, &b); err != nil {
		t.Fatalf("export b: %v", err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("the same tree must export byte-identically")
	}
}

func TestExportRejectsNonDir(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Export(f, new(bytes.Buffer)); err == nil {
		t.Error("Export must reject a non-directory root")
	}
}
