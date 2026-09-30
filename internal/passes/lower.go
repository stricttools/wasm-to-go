package passes

import (
	"go/ast"
	"go/token"
	"strconv"

	"golang.org/x/tools/go/ast/astutil"
)

// The encoding/binary function each memory access helper's portable body
// calls, for the helpers Lower replaces.
var lowerFuncs = map[string]string{
	"load16": "Uint16", "load32": "Uint32", "load64": "Uint64",
	"store16": "PutUint16", "store32": "PutUint32", "store64": "PutUint64",
	"load16u": "Uint16", "load32u": "Uint32", "load64u": "Uint64",
	"store16u": "PutUint16", "store32u": "PutUint32", "store64u": "PutUint64",
}

// The size in bytes of the access of each memory access helper.
var lowerSizes = map[string]int{
	"load16": 2, "load32": 4, "load64": 8, "store16": 2, "store32": 4, "store64": 8,
	"load16u": 2, "load32u": 4, "load64u": 8, "store16u": 2, "store32u": 4, "store64u": 8,
}

// Lower replaces every call of a memory access helper with the helper's
// portable body, and returns the number of calls it replaced. Without
// bytes, it writes calls of encoding/binary:
//
//	load32(mem, a)       → binary.LittleEndian.Uint32(mem[a:])
//	store32(mem, a, v)   → binary.LittleEndian.PutUint32(mem[a:], v)
//
// With bytes, it writes the byte operations those functions are, so no
// call is left for the Go compiler to inline:
//
//	load32(mem, a)       → uint32(mem[a:][3])<<24 | uint32(mem[a:][0]) | uint32(mem[a:][1])<<8 | uint32(mem[a:][2])<<16
//	store32(mem, a, v)   → { mem[a:][3] = byte(v >> 24); mem[a:][0] = byte(v); mem[a:][1] = byte(v >> 8); mem[a:][2] = byte(v >> 16) }
//
// and likewise for the 16- and 64-bit and the unchecked (-unsafe) helpers.
// A store whose value is not a variable, a constant, or an operation on
// them keeps the call.
//
// Why: the Go compiler gives every inlined call its own copies of the
// callee's parameters, named variables with DWARF location lists, and an
// inline mark; a translated module makes tens of thousands of memory
// accesses, and inlining the helper at each one more than doubled the
// compile memory of QuickJS (see the README's compile cost section). The
// call of encoding/binary is what the compiler inlines anyway: the helpers
// are generic only in the address type, which the expression keeps. Into
// a function the compiler considers big, though, it inlines only callees
// of cost 20 or less, and the 32- and 64-bit functions cost more: there the
// translator writes the bytes.
//
// # Why this preserves behavior
//
// The checked helpers of helpers.go are the calls, and the calls are the
// byte operations: the functions check the last byte's index first, then
// access the bytes in little-endian order, and the Go compiler combines
// them into one access. The -unsafe helpers (helpers_unsafe.go) are used
// only in the file built on the platforms outside passes.ExpandPlatforms
// (the expanded file is written by Expand, not by Lower): there the
// checked helpers perform one bounds check of the last byte and an
// unaligned access, and the portable body one check of the start and one
// of the last byte, which fail for the same addresses (a+size > len(mem)),
// before any byte is accessed. An unchecked helper is used only where
// RemoveBoundsChecks proved an earlier check covers the access, so the
// checks the body adds never fail. Evaluation order is unchanged: the
// address and the value are evaluated before the access, as the call's
// arguments are; repeating them, when written as bytes, evaluates the same
// operations on the same variables, which nothing assigns in between.
func Lower(fn *ast.FuncDecl, bytes bool) int {
	if fn.Body == nil {
		return 0
	}
	sites := 0
	astutil.Apply(fn.Body, nil, func(c *astutil.Cursor) bool {
		var call *ast.CallExpr
		stmt := false
		switch n := c.Node().(type) {
		case *ast.ExprStmt:
			call, _ = n.X.(*ast.CallExpr)
			stmt = true
		case *ast.CallExpr:
			call = n
		}
		if call == nil {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		name := lowerFuncs[id.Name]
		store := id.Name[0] == 's'
		if name == "" || len(call.Args) != 2+btoi(store) || store != stmt {
			return true
		}
		mem := call.Args[0]
		switch mem.(type) {
		case *ast.Ident, *ast.SelectorExpr, *ast.ParenExpr:
		default:
			mem = &ast.ParenExpr{X: mem}
		}
		size := lowerSizes[id.Name]
		tail := func() ast.Expr { return &ast.SliceExpr{X: mem, Low: call.Args[1]} }
		sites++
		switch {
		case bytes && !store:
			c.Replace(loadBytes(tail, size))
		case bytes && store && pureValue(call.Args[2]):
			c.Replace(storeBytes(tail, call.Args[2], size))
		default:
			args := []ast.Expr{tail()}
			fun := &ast.SelectorExpr{
				X:   &ast.SelectorExpr{X: ast.NewIdent("binary"), Sel: ast.NewIdent("LittleEndian")},
				Sel: ast.NewIdent(name)}
			lowered := &ast.CallExpr{Fun: fun, Args: append(args, call.Args[2:]...)}
			if store {
				c.Replace(&ast.ExprStmt{X: lowered})
			} else {
				c.Replace(lowered)
			}
		}
		return true
	})
	return sites
}

// The indexes of a little-endian access of size bytes, in the order the
// byte operations use them: the last byte, whose check covers the others,
// first.
func byteOrder(size int) []int {
	order := []int{size - 1}
	for i := range size - 1 {
		order = append(order, i)
	}
	return order
}

func uintType(size int) string { return "uint" + strconv.Itoa(8*size) }

func lit(v int) ast.Expr { return &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(v)} }

// uintN(tail[last])<<8*last | uintN(tail[0]) | uintN(tail[1])<<8 | ...
func loadBytes(tail func() ast.Expr, size int) ast.Expr {
	var e ast.Expr
	for _, i := range byteOrder(size) {
		var b ast.Expr = &ast.CallExpr{Fun: ast.NewIdent(uintType(size)),
			Args: []ast.Expr{&ast.IndexExpr{X: tail(), Index: lit(i)}}}
		if i > 0 {
			b = &ast.BinaryExpr{X: b, Op: token.SHL, Y: lit(8 * i)}
		}
		if e == nil {
			e = b
		} else {
			e = &ast.BinaryExpr{X: e, Op: token.OR, Y: b}
		}
	}
	return e
}

// { tail[last] = byte(v >> 8*last); tail[0] = byte(v); ... }
func storeBytes(tail func() ast.Expr, v ast.Expr, size int) ast.Stmt {
	blk := &ast.BlockStmt{}
	for _, i := range byteOrder(size) {
		var b ast.Expr = v
		if i > 0 {
			b = &ast.BinaryExpr{X: v, Op: token.SHR, Y: lit(8 * i)}
		}
		blk.List = append(blk.List, &ast.AssignStmt{
			Lhs: []ast.Expr{&ast.IndexExpr{X: tail(), Index: lit(i)}},
			Tok: token.ASSIGN,
			Rhs: []ast.Expr{&ast.CallExpr{Fun: ast.NewIdent("byte"), Args: []ast.Expr{b}}}})
	}
	return blk
}

// Reports whether a store's value may be evaluated once per byte: a
// variable, a constant, or conversions and operations on them, and
// math.Float32bits and math.Float64bits of those, none of which reads
// memory or can panic.
func pureValue(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Ident, *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return pureValue(e.X)
	case *ast.UnaryExpr:
		return e.Op != token.ARROW && e.Op != token.AND && pureValue(e.X)
	case *ast.BinaryExpr:
		switch e.Op {
		case token.QUO, token.REM:
			return false
		}
		return pureValue(e.X) && pureValue(e.Y)
	case *ast.CallExpr:
		if len(e.Args) != 1 || !pureValue(e.Args[0]) {
			return false
		}
		switch f := e.Fun.(type) {
		case *ast.Ident:
			_, ok := intRanges[f.Name]
			return ok || f.Name == "i32" || f.Name == "i64"
		case *ast.SelectorExpr:
			pkg, ok := f.X.(*ast.Ident)
			return ok && pkg.Name == "math" && (f.Sel.Name == "Float32bits" || f.Sel.Name == "Float64bits")
		}
	}
	return false
}
