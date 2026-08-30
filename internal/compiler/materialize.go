package compiler

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gombit-dev/gombit-forge/internal/compiler/gen"
)

// ExtensionsRoot is the user-owned code directory (ADR-001 §17). Forge may
// compile it, observe whether files exist, and archive it with explicit consent,
// but it never rewrites the implementation. Nothing in this package writes here.
const ExtensionsRoot = "internal/extensions"

// GeneratedRoots are the directories Forge wholly owns (ADR-001 §16): it wipes
// and recreates them on every materialization and writes nothing outside them.
// Everything Compile emits must live under one of these.
var GeneratedRoots = []string{gen.GeneratedRoot, gen.FrontendRoot}

// Materialize writes the compiled files into the project rooted at dir.
//
// It first removes the generated roots entirely, then writes the files, so a
// resource the spec deleted, renamed or split leaves no stale generated file
// behind — the generated tree is recreated, never merged (ADR-001 §16).
// Ownership is at the directory level (§18): Materialize touches only the
// generated roots, never internal/extensions/** (§17) or anything else in the
// project.
//
// Every path is validated to be a clean relative path under a generated root
// before anything is removed or written, so a generator bug cannot write into
// user territory and a bad path fails before the destructive wipe rather than
// after it.
func Materialize(dir string, files []gen.File) error {
	if dir == "" {
		return fmt.Errorf("compiler: Materialize needs a project directory")
	}
	for _, f := range files {
		if err := checkGeneratedPath(f.Path); err != nil {
			return err
		}
	}

	// Recreate, not merge: wipe each generated root so deletions and renames in
	// the spec are reflected exactly. internal/extensions is never in this list.
	for _, root := range GeneratedRoots {
		if err := os.RemoveAll(filepath.Join(dir, filepath.FromSlash(root))); err != nil {
			return fmt.Errorf("compiler: wiping %s: %w", root, err)
		}
	}

	for _, f := range files {
		full := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("compiler: %s: %w", f.Path, err)
		}
		if err := os.WriteFile(full, f.Content, 0o644); err != nil {
			return fmt.Errorf("compiler: %s: %w", f.Path, err)
		}
	}
	return nil
}

// checkGeneratedPath rejects a path that is not a clean, relative, slash path
// under one of the generated roots. This is the guard that keeps generated
// output from landing in user-owned (internal/extensions) or out-of-tree
// locations, whether by a generator bug or a crafted path (§18): a "..",
// an absolute path, or a non-canonical path that cleans to somewhere else are
// all refused.
func checkGeneratedPath(p string) error {
	if p == "" {
		return fmt.Errorf("compiler: empty generated file path")
	}
	if path.IsAbs(p) || path.Clean(p) != p {
		return fmt.Errorf("compiler: generated file path %q is not a clean relative path", p)
	}
	for _, root := range GeneratedRoots {
		if p == root || strings.HasPrefix(p, root+"/") {
			return nil
		}
	}
	return fmt.Errorf("compiler: generated file path %q is outside the generated roots %v (Forge never writes user-owned or out-of-tree files)", p, GeneratedRoots)
}
