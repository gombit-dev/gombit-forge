package compiler

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gombit-dev/gombit-forge/internal/compiler/gen"
)

// writeFile writes content at dir/rel, creating parents. Test helper for
// seeding pre-existing files.
func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, dir, rel string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b), true
}

// TestMaterializeWritesAndWipes covers the §16 recreate-not-merge rule: a
// generated file for a resource that no longer exists is removed, and the fresh
// files are written.
func TestMaterializeWritesAndWipes(t *testing.T) {
	dir := t.TempDir()
	// A stale generated file from a resource the spec no longer has.
	writeFile(t, dir, "internal/forge_generated/ghost/model.go", "package ghost\n")

	files, err := Compile(sampleSpec(t), testModule)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := Materialize(dir, files); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	if _, ok := readFile(t, dir, "internal/forge_generated/ghost/model.go"); ok {
		t.Error("a stale generated file must be wiped on materialize (§16 recreate, not merge)")
	}
	// Every produced file is on disk with its exact bytes.
	for _, f := range files {
		got, ok := readFile(t, dir, f.Path)
		if !ok {
			t.Errorf("generated file %q was not written", f.Path)
			continue
		}
		if got != string(f.Content) {
			t.Errorf("generated file %q content does not match", f.Path)
		}
	}
}

// TestMaterializeNeverTouchesExtensions is the ownership guarantee (ADR-001 §17,
// §95): a wipe-and-regenerate leaves every user-owned file under
// internal/extensions byte-for-byte unchanged. Checked by hash, before and
// after, across two regenerations.
func TestMaterializeNeverTouchesExtensions(t *testing.T) {
	dir := t.TempDir()
	const hooksRel = "internal/extensions/customer/hooks.go"
	const userCode = "package customer\n\n// hand-written by the developer\nfunc BeforeCreate() {}\n"
	writeFile(t, dir, hooksRel, userCode)

	hash := func() [32]byte {
		content, ok := readFile(t, dir, hooksRel)
		if !ok {
			t.Fatal("user extension file vanished")
		}
		return sha256.Sum256([]byte(content))
	}
	before := hash()

	files, err := Compile(sampleSpec(t), testModule)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// Two materializations: the guarantee holds across regeneration, not just once.
	for i := 0; i < 2; i++ {
		if err := Materialize(dir, files); err != nil {
			t.Fatalf("materialize %d: %v", i, err)
		}
		if hash() != before {
			t.Fatalf("materialize %d modified a user extension file (§17: Forge must never rewrite it)", i)
		}
	}
	if got, _ := readFile(t, dir, hooksRel); got != userCode {
		t.Error("user extension content changed")
	}
}

// TestMaterializeRejectsPathsOutsideGeneratedRoots is the §18 guard: a file
// whose path escapes the generated roots — into user territory, out of tree, or
// via traversal — is refused, and refused *before* the destructive wipe so a bad
// path cannot both fail and destroy the generated tree.
func TestMaterializeRejectsPathsOutsideGeneratedRoots(t *testing.T) {
	bad := []string{
		"internal/extensions/customer/hooks.go",       // user territory
		"internal/other/x.go",                         // elsewhere in tree
		"../escape.go",                                // traversal out of tree
		"/etc/passwd",                                 // absolute
		"internal/forge_generated/../extensions/x.go", // cleans out of the root
		"go.mod", // a stable-shell file Forge does not own
	}
	for _, p := range bad {
		t.Run(p, func(t *testing.T) {
			dir := t.TempDir()
			// A generated file already on disk: it must survive a rejected call.
			writeFile(t, dir, "internal/forge_generated/keep/model.go", "package keep\n")

			err := Materialize(dir, []gen.File{{Path: p, Content: []byte("x")}})
			if err == nil {
				t.Fatalf("Materialize must reject path %q outside the generated roots", p)
			}
			if _, ok := readFile(t, dir, "internal/forge_generated/keep/model.go"); !ok {
				t.Error("a rejected materialize must not have wiped the generated tree (validate before destroy)")
			}
		})
	}
}

// TestGeneratedOutputIsWholeFileOwned covers §16/§18: every generated source
// file is a compiler-owned whole file — it carries the DO-NOT-EDIT banner and
// contains no region markers, because Forge does not use region-based
// generation. If a stage ever emitted a FORGE:BEGIN/END block this fails.
func TestGeneratedOutputIsWholeFileOwned(t *testing.T) {
	files, err := Compile(sampleSpec(t), testModule)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, f := range files {
		content := string(f.Content)
		if strings.Contains(content, "FORGE:BEGIN") || strings.Contains(content, "FORGE:END") {
			t.Errorf("%s contains a region marker; Forge does not use region-based generation (§18)", f.Path)
		}
		// Source files carry the whole-file banner on the first line.
		if strings.HasSuffix(f.Path, ".go") || strings.HasSuffix(f.Path, ".ts") || strings.HasSuffix(f.Path, ".tsx") {
			firstLine := content
			if i := strings.IndexByte(content, '\n'); i >= 0 {
				firstLine = content[:i]
			}
			if !strings.Contains(firstLine, "Code generated by Gombit Forge. DO NOT EDIT") {
				t.Errorf("%s does not open with the DO-NOT-EDIT banner (first line: %q)", f.Path, firstLine)
			}
		}
	}
}

// TestGeneratedPathsAreUnderGeneratedRoots is the invariant Materialize relies
// on: the compiler only ever emits files under the generated roots, so
// materializing its output can never write outside them.
func TestGeneratedPathsAreUnderGeneratedRoots(t *testing.T) {
	files, err := Compile(sampleSpec(t), testModule)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, f := range files {
		if err := checkGeneratedPath(f.Path); err != nil {
			t.Errorf("compiler emitted a file outside the generated roots: %v", err)
		}
	}
}
