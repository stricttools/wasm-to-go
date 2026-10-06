package passes

import "go/ast"

// archsimdOp returns target's direct code of a SIMD operation, calls of
// simd/archsimd's methods, or "" where there is none, and the operation is
// written lane by lane (simdPortable).
func archsimdOp(target SIMDTarget, op string, args []ast.Expr) string {
	return ""
}

// archsimdStore returns target's statements of a store, or nil.
func archsimdStore(target SIMDTarget, op string, args []ast.Expr) []string {
	return nil
}
