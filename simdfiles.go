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

// The SIMD files of a package file: the portable code for every build
// (with -unsafe, as an expanded and a generic file).
func simdFiles() []simdFile {
	if *unsafe {
		return []simdFile{
			{"_simd", passes.ExpandPlatforms, passes.SIMDPortable, true},
			{"_simd_generic", "!(" + passes.ExpandPlatforms + ")", passes.SIMDPortable, false},
		}
	}
	return []simdFile{{"_simd", "", passes.SIMDPortable, false}}
}

// The constraint expr combined with the user's -tags.
func withTags(expr string) (string, error) {
	if expr == "" {
		return *tags, nil
	}
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
		decls := []ast.Decl{passes.SIMDTypeDecl(f.target)}
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
	return nil
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
