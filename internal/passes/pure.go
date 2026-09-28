package passes

import (
	"go/ast"
	"go/token"
)

// Conversions (and the one-operand helpers that are pure functions of
// their operand's bits: the identity helpers i32 and i64, and the float
// helpers that canonicalize, negate, or take the absolute value) that
// cannot panic: a conversion between numeric types never does.
var safeConversions = set[string]{
	"int": {}, "int8": {}, "int16": {}, "int32": {}, "int64": {},
	"uint": {}, "uint8": {}, "uint16": {}, "uint32": {}, "uint64": {},
	"uintptr": {}, "byte": {}, "float32": {}, "float64": {},
	"i32": {}, "i64": {},
	"f32_canon": {}, "f64_canon": {}, "f32_neg": {}, "f64_neg": {}, "f32_abs": {}, "f64_abs": {},
}

// TrapFree reports whether evaluating e can neither panic, nor have side
// effects, nor read anything but local variables and constants:
// identifiers, literals, numeric conversions, the identity helpers i32/i64,
// the float helpers that canonicalize, negate, or take the absolute value,
// math's bit reinterpretations (math.Float64frombits and the like),
// and unary and binary operators other than division, remainder, and shifts
// by a non-literal count (a negative shift count panics).
//
// In generated code identifiers are locals, parameters, and constants,
// never shadowed, and so is the package name math; fields (selectors),
// dereferences, indexing and calls other than conversions and math's bit
// reinterpretations are rejected.
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
		if len(e.Args) != 1 || e.Ellipsis.IsValid() {
			return false
		}
		switch fun := e.Fun.(type) {
		case *ast.Ident:
			if safeConversions.has(fun.Name) {
				return TrapFree(e.Args[0])
			}
		case *ast.SelectorExpr:
			// Float constants are written math.Float64frombits(0x...).
			if pkg, ok := fun.X.(*ast.Ident); ok && pkg.Name == "math" && mathBits.has(fun.Sel.Name) {
				return TrapFree(e.Args[0])
			}
		}
	}
	return false
}

// The math functions that reinterpret bits, which never panic.
var mathBits = set[string]{
	"Float32bits": {}, "Float32frombits": {}, "Float64bits": {}, "Float64frombits": {},
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
