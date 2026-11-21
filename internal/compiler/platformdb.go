package compiler

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gombit-dev/gombit-forge/internal/gombit"
)

// PlatformDatabasePath is the scaffold's database wiring in the app tree. Its
// AutoMigrate call is where Gombit's tooling reads an app's models from.
const PlatformDatabasePath = "internal/platform/database.go"

// platformAutoMigrateNote joins the scaffold's doc comment on AutoMigrate, so a
// reader of the exported tree knows why the function exists when nothing calls
// it.
const platformAutoMigrateNote = `//
// The composition root never calls it: the schema comes from the migrations
// alone. It still lists every model, because gombit db check compares this
// list with the migration registry, and gombit make resource adds to it.
`

// RegisterPlatformModels keeps the scaffold's AutoMigrate list in step with
// the models the generated application migrates.
//
// A Forge application never AutoMigrates (DESIGN.md §14): the composition root
// does not call the scaffold's platform.AutoMigrate. Gombit's tooling still
// reads that call as the app's models: `gombit db check` compares it with the
// migration registry, so without them it reports every generated model as
// drift, and `gombit make resource` builds on it. So the generated models are
// added to it, the way `gombit make resource` adds one, and the exported app's
// tooling sees the whole schema.
//
// Each model is imported under its package name, or as "forge<package>" when
// the file already imports another package by that name. A model already listed
// is not added twice. Source with no AutoMigrate call is returned unchanged: a
// toolchain whose scaffold has none reads no models there. More than one call
// is an error, since which one the tooling reads is ambiguous.
func RegisterPlatformModels(src []byte, models []gombit.Model) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, PlatformDatabasePath, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("compiler: parse %s: %w", PlatformDatabasePath, err)
	}

	var calls []*ast.CallExpr
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "AutoMigrate" {
				calls = append(calls, call)
			}
		}
		return true
	})
	switch len(calls) {
	case 0:
		return src, nil
	case 1:
	default:
		return nil, fmt.Errorf("compiler: %s has %d AutoMigrate calls, want one", PlatformDatabasePath, len(calls))
	}
	call := calls[0]

	// The edits are text inserted at offsets the parse found, then gofmt: the
	// scaffold's comments and layout stay where they were, which reprinting an
	// edited syntax tree does not guarantee.
	names, err := importNames(file)
	if err != nil {
		return nil, err
	}
	var newImports, newArgs strings.Builder
	for _, model := range models {
		name, spec, err := importName(file, names, model.ImportPath)
		if err != nil {
			return nil, err
		}
		newImports.WriteString(spec)
		if !listsModel(call, name, model.TypeName) {
			fmt.Fprintf(&newArgs, "&%s.%s{},\n", name, model.TypeName)
		}
	}
	if newImports.Len() == 0 && newArgs.Len() == 0 {
		return noteAutoMigrate(src), nil
	}

	offset := func(pos token.Pos) int { return fset.Position(pos).Offset }
	var edits []textInsertion
	if newArgs.Len() > 0 {
		args := newArgs.String()
		// A last argument with no trailing comma (a call on one line) needs one.
		if n := len(call.Args); n > 0 && !bytes.Contains(src[offset(call.Args[n-1].End()):offset(call.Rparen)], []byte(",")) {
			args = ",\n" + args
		}
		edits = append(edits, textInsertion{offset(call.Rparen), args})
	}
	if newImports.Len() > 0 {
		edits = append(edits, importInsertion(file, offset, newImports.String()))
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].at > edits[j].at })
	out := append([]byte(nil), src...)
	for _, e := range edits {
		out = append(out[:e.at], append([]byte(e.text), out[e.at:]...)...)
	}

	formatted, err := format.Source(out)
	if err != nil {
		return nil, fmt.Errorf("compiler: format %s: %w", PlatformDatabasePath, err)
	}
	return noteAutoMigrate(formatted), nil
}

// textInsertion is text to insert into a source file at a byte offset.
type textInsertion struct {
	at   int
	text string
}

// importInsertion is where, and as what text, specs (import lines) go in
// file: at the end of its first import block, or as a block of their own after
// an unparenthesized import or, with none, after the package clause.
func importInsertion(file *ast.File, offset func(token.Pos) int, specs string) textInsertion {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		if gen.Lparen.IsValid() {
			return textInsertion{offset(gen.Rparen), specs}
		}
		return textInsertion{offset(gen.End()), "\nimport (\n" + specs + ")"}
	}
	return textInsertion{offset(file.Name.End()), "\n\nimport (\n" + specs + ")\n"}
}

// ApplyPlatformModels runs RegisterPlatformModels on the app tree at dir. A
// tree without the scaffold's platform file is left as it is.
func ApplyPlatformModels(dir string, models []gombit.Model) error {
	full := filepath.Join(dir, filepath.FromSlash(PlatformDatabasePath))
	// #nosec G304 -- a file of the application tree being assembled.
	src, err := os.ReadFile(full)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("compiler: read %s: %w", PlatformDatabasePath, err)
	}
	out, err := RegisterPlatformModels(src, models)
	if err != nil {
		return err
	}
	if bytes.Equal(out, src) {
		return nil
	}
	if err := os.WriteFile(full, out, 0o644); err != nil {
		return fmt.Errorf("compiler: write %s: %w", PlatformDatabasePath, err)
	}
	return nil
}

// importNames maps each name the file's imports bind to its import path.
func importNames(file *ast.File) (map[string]string, error) {
	names := make(map[string]string, len(file.Imports))
	for _, imp := range file.Imports {
		importPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("compiler: %s import %s: %w", PlatformDatabasePath, imp.Path.Value, err)
		}
		name := path.Base(importPath)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "_" || name == "." {
			continue
		}
		names[name] = importPath
	}
	return names, nil
}

// importName returns the name importPath is bound to in file and, when it is
// not imported yet, the import line that binds it: under its package name, or
// as "forge"+name when that is taken. The last path element is the package
// name: Forge's generated packages are named after their directory. names
// gains the new binding.
func importName(file *ast.File, names map[string]string, importPath string) (name, spec string, err error) {
	for _, imp := range file.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err != nil || p != importPath {
			continue
		}
		if imp.Name == nil {
			return path.Base(importPath), "", nil
		}
		if imp.Name.Name != "_" && imp.Name.Name != "." {
			return imp.Name.Name, "", nil
		}
	}
	for n, p := range names {
		if p == importPath {
			return n, "", nil // added for an earlier model
		}
	}
	base := path.Base(importPath)
	if _, taken := names[base]; !taken {
		names[base] = importPath
		return base, strconv.Quote(importPath) + "\n", nil
	}
	alias := "forge" + base
	if _, taken := names[alias]; taken {
		return "", "", fmt.Errorf("compiler: %s already imports packages named %s and %s; cannot import %s", PlatformDatabasePath, base, alias, importPath)
	}
	names[alias] = importPath
	return alias, alias + " " + strconv.Quote(importPath) + "\n", nil
}

// listsModel reports whether call already has the argument &name.typeName{}.
func listsModel(call *ast.CallExpr, name, typeName string) bool {
	for _, arg := range call.Args {
		unary, ok := arg.(*ast.UnaryExpr)
		if !ok || unary.Op != token.AND {
			continue
		}
		lit, ok := unary.X.(*ast.CompositeLit)
		if !ok {
			continue
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == name && sel.Sel.Name == typeName {
			return true
		}
	}
	return false
}

// noteAutoMigrate appends platformAutoMigrateNote to the doc comment of
// `func AutoMigrate`, once. A function with no doc comment, or source already
// carrying the note, is left as it is.
func noteAutoMigrate(src []byte) []byte {
	if bytes.Contains(src, []byte(platformAutoMigrateNote)) {
		return src
	}
	decl := []byte("\nfunc AutoMigrate(")
	if bytes.Count(src, decl) != 1 {
		return src
	}
	i := bytes.Index(src, decl)
	// The doc comment is the run of "//" lines directly above the function.
	lineStart := bytes.LastIndexByte(src[:i], '\n') + 1
	if !bytes.HasPrefix(src[lineStart:i], []byte("//")) {
		return src
	}
	out := make([]byte, 0, len(src)+len(platformAutoMigrateNote))
	out = append(out, src[:i+1]...)
	out = append(out, platformAutoMigrateNote...)
	out = append(out, src[i+1:]...)
	return out
}
