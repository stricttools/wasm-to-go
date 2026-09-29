package passes

import "go/ast"

// The encoding/binary function each memory access helper's portable body
// calls, for the helpers Lower replaces.
var lowerFuncs = map[string]string{
	"load16": "Uint16", "load32": "Uint32", "load64": "Uint64",
	"store16": "PutUint16", "store32": "PutUint32", "store64": "PutUint64",
	"load16u": "Uint16", "load32u": "Uint32", "load64u": "Uint64",
	"store16u": "PutUint16", "store32u": "PutUint32", "store64u": "PutUint64",
}

// Lower replaces every call of a memory access helper with the helper's
// portable body, written as an expression:
//
//	load32(mem, a)       → binary.LittleEndian.Uint32(mem[a:])
//	store32(mem, a, v)   → binary.LittleEndian.PutUint32(mem[a:], v)
//	load32u(mem, a)      → binary.LittleEndian.Uint32(mem[a:])
//
// and likewise for the 16- and 64-bit and the other unchecked (-unsafe)
// helpers. It changes each call in place and returns a function that
// restores them, and the number of calls it replaced.
//
// Why: the Go compiler gives every inlined call its own copies of the
// callee's parameters, named variables with DWARF location lists, and an
// inline mark; a translated module makes tens of thousands of memory
// accesses, and inlining the helper at each one more than doubled the
// compile memory of QuickJS (see the README's compile cost section). The
// expression is what the compiler inlines anyway: the helpers are generic
// only in the address type, which the expression keeps.
//
// # Why this preserves behavior
//
// The checked helpers of helpers.go are these expressions. The -unsafe
// helpers (helpers_unsafe.go) are used only in the file built on the
// platforms outside passes.ExpandPlatforms (the expanded file is written
// by Expand, not by Lower): there the checked helpers perform one bounds
// check of the last byte and an unaligned access, and the expression one
// check of the start and one of the length, which fail for the same
// addresses (a+size > len(mem)), before any access. An unchecked helper
// is used only where RemoveBoundsChecks proved an earlier check covers
// the access, so the check the expression adds never fails. Evaluation
// order is unchanged: the arguments are evaluated before the access, as
// in the call.
func Lower(fn *ast.FuncDecl) (undo func(), sites int) {
	type saved struct {
		call *ast.CallExpr
		fun  ast.Expr
		args []ast.Expr
	}
	var changed []saved
	if fn.Body != nil {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			name := lowerFuncs[id.Name]
			store := id.Name[0] == 's'
			if name == "" || len(call.Args) != 2+btoi(store) {
				return true
			}
			changed = append(changed, saved{call, call.Fun, call.Args})
			mem := call.Args[0]
			switch mem.(type) {
			case *ast.Ident, *ast.SelectorExpr, *ast.ParenExpr:
			default:
				mem = &ast.ParenExpr{X: mem}
			}
			args := []ast.Expr{&ast.SliceExpr{X: mem, Low: call.Args[1]}}
			call.Fun = &ast.SelectorExpr{
				X:   &ast.SelectorExpr{X: ast.NewIdent("binary"), Sel: ast.NewIdent("LittleEndian")},
				Sel: ast.NewIdent(name)}
			call.Args = append(args, call.Args[2:]...)
			return true
		})
	}
	undo = func() {
		for _, s := range changed {
			s.call.Fun, s.call.Args = s.fun, s.args
		}
	}
	return undo, len(changed)
}
