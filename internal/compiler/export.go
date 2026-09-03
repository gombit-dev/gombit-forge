package compiler

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// exportModTime is the fixed timestamp stamped on every archive entry so the
// same source tree exports byte-identically (the determinism contract). Its
// value is arbitrary but fixed; the exported files are ordinary source, and a
// consumer that cares about real mtimes reads them from the checkout, not the
// archive.
var exportModTime = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

// exportSkipDirs are directory names never included in an export: version-control
// metadata and dependency/build output. Everything else under the project —
// scaffold, generated code and user extensions alike — is source the exporter
// hands over whole (ADR-001 §72, "own the code"). node_modules and dist are
// reproducible from source and would bloat the archive; .git is history, not
// source.
var exportSkipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"dist":         true,
}

// Export writes a deterministic ZIP of the Gombit project rooted at root to w
// (DESIGN.md §4.9, §28; ADR-001 §72). It archives the full repository tree —
// cmd/, internal/ (including internal/extensions/ user code), frontend/,
// database/, config/, docs/, gombit.yaml, go.mod, README.md — so the export is
// an ordinary Gombit repository with no Forge runtime dependency and no
// obligation to call Forge (P2, D10/D11). VCS metadata and dependency/build
// output are excluded (exportSkipDirs); every other file is included verbatim.
//
// Determinism: entries are written in sorted path order with a fixed timestamp,
// so the same tree yields a byte-identical archive. Paths use forward slashes.
func Export(root string, w io.Writer) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("export: %s is not a directory", root)
	}

	// Collect the files first so entries can be emitted in a stable, sorted order
	// regardless of filesystem walk order.
	type entry struct {
		rel  string
		abs  string
		mode fs.FileMode
	}
	var entries []entry
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == root {
			return nil
		}
		if d.IsDir() {
			if exportSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip symlinks and other non-regular files: an export is source, and a
		// symlink's target may point outside the tree.
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entries = append(entries, entry{rel: filepath.ToSlash(rel), abs: p, mode: info.Mode()})
		return nil
	})
	if err != nil {
		return fmt.Errorf("export: walk %s: %w", root, err)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	zw := zip.NewWriter(w)
	for _, e := range entries {
		if err := writeZipEntry(zw, e.rel, e.abs, e.mode); err != nil {
			_ = zw.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("export: finalize archive: %w", err)
	}
	return nil
}

// writeZipEntry copies one file into the archive with a fixed timestamp and a
// normalized mode (executables keep their bit, everything else is 0644), so the
// archive depends only on the tree's content and layout.
func writeZipEntry(zw *zip.Writer, name, abs string, mode fs.FileMode) error {
	perm := fs.FileMode(0o644)
	if mode&0o111 != 0 {
		perm = 0o755
	}
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(perm)
	header.Modified = exportModTime

	fw, err := zw.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("export: create entry %s: %w", name, err)
	}
	f, err := os.Open(abs)
	if err != nil {
		return fmt.Errorf("export: open %s: %w", name, err)
	}
	defer f.Close()
	if _, err := io.Copy(fw, f); err != nil {
		return fmt.Errorf("export: copy %s: %w", name, err)
	}
	return nil
}
