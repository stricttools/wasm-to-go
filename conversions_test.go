//go:build !generator

package main

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// No helper, and no translation, converts a float to an integer with Go's
// conversion: its result where the Go specification leaves it undefined
// differs by CPU, and strictgo refuses it in every program. The helpers are
// checked whole, since any of them may be copied into a translation, and so
// are translations using every float-to-integer operation.
func Test_no_float_to_int_conversion(t *testing.T) {
	for _, file := range []string{"helpers/helpers.go", "testdata/determinism/determinism.go", "testdata/trig/trig.go"} {
		for _, pos := range floatToIntConversions(t, file) {
			t.Errorf("%s: converts a float to an integer with Go's conversion", pos)
		}
	}
	// The check is live: a conversion it must see is seen.
	src := "package p\n\nfunc f(x float64) int32 { return int32(x) }\n"
	if got := floatToIntConversionsIn(t, "p.go", src); len(got) != 1 {
		t.Fatalf("the check finds %d conversions in %q, want 1", len(got), src)
	}
}

// floatToIntConversions type-checks one file, which imports nothing outside
// the standard library, and returns where it converts a float to an integer.
func floatToIntConversions(t *testing.T, filename string) []token.Position {
	t.Helper()
	return floatToIntConversionsIn(t, filename, nil)
}

func floatToIntConversionsIn(t *testing.T, filename string, src any) []token.Position {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	if _, err := conf.Check(f.Name.Name, fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	var found []token.Position
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		fun, ok := info.Types[call.Fun]
		if !ok || !fun.IsType() {
			return true
		}
		arg := info.Types[call.Args[0]]
		if arg.Value != nil { // a constant converts exactly or not at all
			return true
		}
		to, ok1 := fun.Type.Underlying().(*types.Basic)
		from, ok2 := arg.Type.Underlying().(*types.Basic)
		if ok1 && ok2 && to.Info()&types.IsInteger != 0 && from.Info()&types.IsFloat != 0 {
			found = append(found, fset.Position(call.Pos()))
		}
		return true
	})
	return found
}
