package main

import (
	"errors"
	"go/ast"
	"go/build/constraint"
	"go/token"
	"io"
	"path"
	"strings"

	"github.com/stricttools/wasm-to-go/internal/passes"
)

// A file of a package's SIMD code (passes.LowerSIMD): the functions that
// use v128 values, written for one target, under the build constraint of
// the builds that target is for.
type simdFile struct {
	suffix     string // of the file's name, after the package file's
	constraint string
	target     passes.SIMDTarget
	expanded   bool // memory accesses written with unsafe (passes.Expand), as -unsafe's expanded file
}

// The Go versions whose simd/archsimd the archsimd targets are written for
// and were tested with: the package has no compatibility promise, and its
// API changed between Go 1.26 and 1.27.
const simdGoVersions = "go1.27 && !go1.28"

// The builds that call simd/archsimd: GOEXPERIMENT=simd on amd64 with AVX2
// (GOAMD64=v3, since archsimd's 128-bit operations need AVX), arm64, and
// wasm.
const simdArchBuilds = "goexperiment.simd && (amd64.v3 || arm64 || wasm)"

// simdUntested names, in the build error of a build that calls
// simd/archsimd with a Go version the translation was not tested with, the
// ways out: build with Go 1.27, or without GOEXPERIMENT=simd, which uses
// the portable SIMD code.
const simdUntested = "wasm2go_simd_needs_go1_27_or_GOEXPERIMENT_simd_off"

// The SIMD files of a package file: one per archsimd target, the portable
// code for every other build (with -unsafe, as an expanded and a generic
// file), and a file that fails the build of an untested Go version.
func simdFiles() []simdFile {
	files := []simdFile{
		{"_simd_amd64", "goexperiment.simd && amd64.v3 && " + simdGoVersions, passes.SIMDAMD64, *unsafe},
		{"_simd_arm64", "goexperiment.simd && arm64 && " + simdGoVersions, passes.SIMDARM64, *unsafe},
		{"_simd_wasm", "goexperiment.simd && wasm && " + simdGoVersions, passes.SIMDWasm, *unsafe},
	}
	portable := "!(" + simdArchBuilds + ")"
	if *unsafe {
		files = append(files,
			simdFile{"_simd", portable + " && (" + passes.ExpandPlatforms + ")", passes.SIMDPortable, true},
			simdFile{"_simd_generic", portable + " && !(" + passes.ExpandPlatforms + ")", passes.SIMDPortable, false})
	} else {
		files = append(files, simdFile{"_simd", portable, passes.SIMDPortable, false})
	}
	return files
}

// The constraint expr combined with the user's -tags.
func withTags(expr string) (string, error) {
	e, err := constraint.Parse("//go:build " + expr)
	if err != nil {
		return "", err
	}
	if *tags != "" {
		user, err := constraint.Parse("//go:build " + *tags)
		if err != nil {
			return "", err
		}
		e = &constraint.AndExpr{X: user, Y: e}
	}
	return e.String(), nil
}

// Splits decls into those that use SIMD (passes.UsesSIMD) and the others.
func splitSIMD(decls []ast.Decl) (base, simd []ast.Decl) {
	for _, d := range decls {
		if fd, ok := d.(*ast.FuncDecl); ok && passes.UsesSIMD(fd) {
			simd = append(simd, d)
		} else {
			base = append(base, d)
		}
	}
	return base, simd
}

// The name of the SIMD file of suffix for the package file at rel (the
// output file, named name, if rel is "").
func simdFileName(rel, name, suffix string) string {
	if rel == "" {
		return strings.TrimSuffix(name, ".go") + suffix + ".go"
	}
	return strings.TrimSuffix(rel, ".go") + suffix + ".go"
}

// Writes the SIMD files of a file of package pkg at rel (the output file,
// named name, if rel is ""), holding simd, the decls that use SIMD; each
// gets the helpers its code uses that base, the file's other declarations,
// does not (helpers returns the helper declarations decls use, or nil).
func (t *translator) printSIMDFiles(open func(rel string) (io.Writer, error), rel, name, pkg string, simd []ast.Decl,
	fset *token.FileSet, doc bool, more func([]ast.Decl) []string, extraHelpers func(decls []ast.Decl) []ast.Decl) error {
	if open == nil {
		return errors.New("a module with SIMD instructions needs -o: its SIMD code is written to files of its own, one for each target")
	}
	capIsLen := t.memory != nil && t.memory.owned()
	for _, f := range simdFiles() {
		decls := passes.SIMDTypeDecls(f.target)
		var code []*ast.FuncDecl
		for _, d := range simd {
			c := passes.Clone(d.(*ast.FuncDecl))
			passes.LowerSIMD(c, f.target, capIsLen)
			// An operation whose code does not read an operand (a shuffle
			// of one operand's lanes) leaves the operand's variable unused.
			passes.RemoveUnusedLocals(c)
			decls = append(decls, c)
			code = append(code, c)
		}
		if f.expanded {
			t.expand(code)
		} else {
			t.lower(code)
		}
		if extraHelpers != nil {
			decls = append(decls, extraHelpers(decls)...)
		}
		tags, err := withTags(f.constraint)
		if err != nil {
			return err
		}
		w, err := open(simdFileName(rel, name, f.suffix))
		if err != nil {
			return err
		}
		if err := t.printDecls(w, fset, pkg, decls, tags, doc, more); err != nil {
			return err
		}
	}
	tags, err := withTags(simdArchBuilds + " && !(" + simdGoVersions + ")")
	if err != nil {
		return err
	}
	w, err := open(simdFileName(rel, name, "_simd_untested"))
	if err != nil {
		return err
	}
	untested := &ast.GenDecl{
		Tok: token.VAR,
		Doc: &ast.CommentGroup{List: []*ast.Comment{
			{Text: "// This build calls simd/archsimd (GOEXPERIMENT=simd), whose API has no compatibility"},
			{Text: "// promise, with a Go version the translation's SIMD code was not written for and tested"},
			{Text: "// with (" + simdGoVersions + "): build with Go 1.27, or without GOEXPERIMENT=simd."},
		}},
		Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("_")}, Values: []ast.Expr{ast.NewIdent(simdUntested)}}},
	}
	return t.printDecls(w, fset, pkg, []ast.Decl{untested}, tags, true, nil)
}

// codeFuncs returns the FuncDecls of decls that are the module's code.
func (t *translator) codeFuncs(decls []ast.Decl) []*ast.FuncDecl {
	code := map[ast.Decl]bool{}
	for _, d := range t.code {
		code[d] = true
	}
	var out []*ast.FuncDecl
	for _, d := range decls {
		if code[d] {
			out = append(out, d.(*ast.FuncDecl))
		}
	}
	return out
}

var _ = path.Join
