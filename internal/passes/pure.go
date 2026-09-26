package passes

import (
	"go/ast"
	"go/token"
)

// Conversions (and the identity helpers i32 and i64) that cannot panic:
// a conversion between numeric types never does.
var safeConversions = set[string]{
	"int": {}, "int8": {}, "int16": {}, "int32": {}, "int64": {},
	"uint": {}, "uint8": {}, "uint16": {}, "uint32": {}, "uint64": {},
	"uintptr": {}, "byte": {}, "float32": {}, "float64": {},
	"i32": {}, "i64": {},
}

// TrapFree reports whether evaluating e can neither panic, nor have side
// effects, nor read anything but local variables and constants:
// identifiers, literals, numeric conversions, the identity helpers i32/i64,
// and unary and binary operators other than division, remainder, and shifts
// by a non-literal count (a negative shift count panics).
//
// In generated code identifiers are locals, parameters, and constants,
// never shadowed; fields (selectors), dereferences, indexing and calls
// other than conversions are rejected.
func TrapFree(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Ident, *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return TrapFree(e.X)
	case *ast.UnaryExpr:
		switch e.Op {
		case token.SUB, token.XOR, token.ADD, token.NOT:
			return TrapFree(e.X)
		}
	case *ast.BinaryExpr:
		switch e.Op {
		case token.QUO, token.REM:
			return false
		case token.SHL, token.SHR:
			if !is[*ast.BasicLit](e.Y) {
				return false
			}
		}
		return TrapFree(e.X) && TrapFree(e.Y)
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok && safeConversions.has(id.Name) &&
			len(e.Args) == 1 && !e.Ellipsis.IsValid() {
			return TrapFree(e.Args[0])
		}
	}
	return false
}

// cloneTrapFree copies an expression accepted by TrapFree.
// Identifiers are shared, as everywhere in generated code.
func cloneTrapFree(e ast.Expr) ast.Expr {
	switch e := e.(type) {
	case *ast.BasicLit:
		c := *e
		return &c
	case *ast.ParenExpr:
		return &ast.ParenExpr{X: cloneTrapFree(e.X)}
	case *ast.UnaryExpr:
		return &ast.UnaryExpr{Op: e.Op, X: cloneTrapFree(e.X)}
	case *ast.BinaryExpr:
		return &ast.BinaryExpr{Op: e.Op, X: cloneTrapFree(e.X), Y: cloneTrapFree(e.Y)}
	case *ast.CallExpr:
		return &ast.CallExpr{Fun: e.Fun, Args: []ast.Expr{cloneTrapFree(e.Args[0])}}
	}
	return e
}
