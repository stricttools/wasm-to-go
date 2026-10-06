package passes

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// SIMDPrefix starts the names of the operations of the SIMD form the
// translator writes WebAssembly's fixed-width SIMD instructions in:
// simd_f32x4_add(x, y) for f32x4.add, with the instruction's immediates as
// constant operands, simd_v128_load(mem, addr) for a load, and
// simd_f32x4_canon(x) for the canonicalization of a result's NaN lanes.
// No Go code declares them: LowerSIMD rewrites them for each target.
const SIMDPrefix = "simd_"

// SIMDType is the Go type of a v128 value in translated code: an alias the
// file of each target declares (SIMDTypeDecls).
const SIMDType = "vec128"

// SIMDTypeF32 and SIMDTypeF64 are the Go types of the portable code's
// vectors of float lanes, as floats (portableDomains).
const (
	SIMDTypeF32 = "vec128f32"
	SIMDTypeF64 = "vec128f64"
)

// A SIMDTarget is what a file's SIMD code is written for.
type SIMDTarget int

const (
	// SIMDPortable is inline scalar code over four uint32 lanes, which
	// builds everywhere.
	SIMDPortable SIMDTarget = iota
	// SIMDAMD64, SIMDARM64, and SIMDWasm are direct calls of
	// simd/archsimd's methods, on Go 1.27 with GOEXPERIMENT=simd (and
	// GOAMD64=v3 for amd64, whose 128-bit operations need AVX).
	SIMDAMD64
	SIMDARM64
	SIMDWasm
)

// SIMDTypeDecls are the declarations of SIMDType for target, and of the
// portable code's float vectors.
func SIMDTypeDecls(target SIMDTarget) []ast.Decl {
	alias := func(name, typ string) ast.Decl {
		return &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{&ast.TypeSpec{
			Name: ast.NewIdent(name), Assign: 1, Type: mustParseExpr(typ)}}}
	}
	if target != SIMDPortable {
		return []ast.Decl{alias(SIMDType, "archsimd.Uint32x4")}
	}
	return []ast.Decl{
		alias(SIMDType, "struct{ L0, L1, L2, L3 uint32 }"),
		alias(SIMDTypeF32, "struct{ F0, F1, F2, F3 float32 }"),
		alias(SIMDTypeF64, "struct{ D0, D1 float64 }"),
	}
}

// UsesSIMD reports whether n uses the SIMD form: an operation of it, or
// the SIMDType.
func UsesSIMD(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && (id.Name == SIMDType || strings.HasPrefix(id.Name, SIMDPrefix)) {
			found = true
		}
		return !found
	})
	return found
}

// SIMDVector reports whether e is a call of an operation of the SIMD form
// whose result is a vector: any but extract_lane, the tests (any_true,
// all_true, bitmask), and v128.bytes.
func SIMDVector(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || !IsSIMDOp(id.Name) {
		return false
	}
	for _, s := range []string{"extract_lane", "_true", "bitmask", "v128_bytes"} {
		if strings.Contains(id.Name, s) {
			return false
		}
	}
	return true
}

// IsSIMDOp reports whether name is an operation of the SIMD form, which
// neither calls module code nor changes the memory's slice.
func IsSIMDOp(name string) bool { return strings.HasPrefix(name, SIMDPrefix) }

func mustParseExpr(src string) ast.Expr {
	e, err := parser.ParseExpr(src)
	if err != nil {
		panic(fmt.Sprintf("%v: %s", err, src))
	}
	clearPositions(e)
	return e
}

// clearPositions zeroes the positions of the nodes of n, parsed from a
// source of its own: printed with the translation's positions, they would
// break its lines where they fell in that source. It knows the nodes the
// SIMD code is made of, and panics on another.
func clearPositions(n ast.Node) {
	ast.Inspect(n, func(n ast.Node) bool {
		switch n := n.(type) {
		case nil:
			return false
		case *ast.Ident:
			n.NamePos = token.NoPos
		case *ast.BasicLit:
			n.ValuePos = token.NoPos
		case *ast.CallExpr:
			n.Lparen, n.Rparen, n.Ellipsis = token.NoPos, token.NoPos, token.NoPos
		case *ast.BinaryExpr:
			n.OpPos = token.NoPos
		case *ast.UnaryExpr:
			n.OpPos = token.NoPos
		case *ast.ParenExpr:
			n.Lparen, n.Rparen = token.NoPos, token.NoPos
		case *ast.SelectorExpr:
		case *ast.IndexExpr:
			n.Lbrack, n.Rbrack = token.NoPos, token.NoPos
		case *ast.SliceExpr:
			n.Lbrack, n.Rbrack = token.NoPos, token.NoPos
		case *ast.CompositeLit:
			n.Lbrace, n.Rbrace = token.NoPos, token.NoPos
		case *ast.KeyValueExpr:
			n.Colon = token.NoPos
		case *ast.StarExpr:
			n.Star = token.NoPos
		case *ast.ArrayType:
			n.Lbrack = token.NoPos
		case *ast.AssignStmt:
			n.TokPos = token.NoPos
		case *ast.ExprStmt, *ast.Field:
		case *ast.StructType:
			n.Struct = token.NoPos
		case *ast.FieldList:
			n.Opening, n.Closing = token.NoPos, token.NoPos
		default:
			panic(fmt.Sprintf("clearPositions: %T", n))
		}
		return true
	})
}

func exprString(e ast.Expr) string {
	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), e); err != nil {
		panic(err)
	}
	return b.String()
}

// The names of the variables holding a SIMD memory access's array pointer,
// by its size, which LowerSIMD declares first in the function.
var simdPointerNames = map[int]string{1: "q1", 2: "q2", 4: "q4", 8: "q8", 16: "q16"}

// The size in bytes of the memory each SIMD memory operation accesses.
var simdAccessSizes = map[string]int{
	"v128_load": 16, "v128_store": 16,
	"v128_load8x8_s": 8, "v128_load8x8_u": 8, "v128_load16x4_s": 8, "v128_load16x4_u": 8,
	"v128_load32x2_s": 8, "v128_load32x2_u": 8,
	"v128_load8_splat": 1, "v128_load16_splat": 2, "v128_load32_splat": 4, "v128_load64_splat": 8,
	"v128_load32_zero": 4, "v128_load64_zero": 8,
	"v128_load8_lane": 1, "v128_load16_lane": 2, "v128_load32_lane": 4, "v128_load64_lane": 8,
	"v128_store8_lane": 1, "v128_store16_lane": 2, "v128_store32_lane": 4, "v128_store64_lane": 8,
}

// LowerSIMD rewrites the operations of the SIMD form in fn into target's
// code. capIsLen reports, as for Lower, that the memory's capacity is its
// length. A memory access's array pointer goes to a variable of the
// function, assigned before the statement that accesses memory, as
// hoistAccesses does for scalar accesses.
//
// # Why this preserves behavior
//
// Each operation's code computes what the WebAssembly specification
// defines for it, lane by lane, with the deterministic profile's
// canonicalization where the translator wrote simd_f32x4_canon or
// simd_f64x2_canon; the SIMD spec tests check every operation on every
// target. A memory access checks the bounds of all its bytes with the
// slice its array pointer is converted from, before it accesses any, as
// the specification's trap requires; its address and operands are
// variables and constants, which nothing changes between the pointer's
// assignment and the access.
func LowerSIMD(fn *ast.FuncDecl, target SIMDTarget, capIsLen bool) {
	if fn.Body == nil || !UsesSIMD(fn) {
		return
	}
	l := &simdLowerer{target: target, capIsLen: capIsLen, ptrs: map[int]bool{}}
	if target == SIMDPortable {
		l.domains = portableDomains(fn)
	}
	// Memory accesses, which are statements of their own (the translator
	// writes a load's result to a variable), with their pointers in
	// variables; stores become their statements.
	postApplyStmts(fn.Body, func(list []ast.Stmt) []ast.Stmt {
		var out []ast.Stmt
		for _, s := range list {
			out = append(out, l.hoist(s)...)
		}
		return out
	})
	l.rewrite(fn.Body)
	if target == SIMDPortable {
		l.portable().convertUses(fn)
	}
	var decls []ast.Stmt
	for _, size := range []int{1, 2, 4, 8, 16} {
		if l.ptrs[size] {
			decls = append(decls, varDecl(simdPointerNames[size], &ast.StarExpr{X: &ast.ArrayType{Len: lit(size), Elt: ast.NewIdent("byte")}}))
		}
	}
	fn.Body.List = append(decls, fn.Body.List...)
}

type simdLowerer struct {
	target   SIMDTarget
	capIsLen bool
	ptrs     map[int]bool
	domains  map[string]string // the portable code's (portableDomains)
}

// The statements s becomes: a statement whose value is a SIMD memory
// access gets its array pointer assigned before it, and a store becomes
// the statements that write its bytes.
func (l *simdLowerer) hoist(s ast.Stmt) []ast.Stmt {
	switch st := s.(type) {
	case *ast.LabeledStmt:
		parts := l.hoist(st.Stmt)
		st.Stmt = parts[0]
		return append([]ast.Stmt{st}, parts[1:]...)
	case *ast.ExprStmt:
		if call := simdAccess(st.X); call != nil {
			out := []ast.Stmt{l.pointer(call)}
			op := strings.TrimPrefix(call.Fun.(*ast.Ident).Name, SIMDPrefix)
			// The stored vector's code first: the store's reads its lanes.
			holder := &ast.CallExpr{Args: call.Args}
			l.rewrite(holder)
			for _, src := range l.store(op, call.Args) {
				out = append(out, mustParseStmt(src))
			}
			return out
		}
	case *ast.AssignStmt:
		if len(st.Rhs) == 1 {
			if call := simdAccess(st.Rhs[0]); call != nil {
				return []ast.Stmt{l.pointer(call), st}
			}
		}
	}
	return []ast.Stmt{s}
}

func mustParseStmt(src string) ast.Stmt {
	f, err := parser.ParseFile(token.NewFileSet(), "", "package p\nfunc _() {\n"+src+"\n}", 0)
	if err != nil {
		panic(fmt.Sprintf("%v: %s", err, src))
	}
	stmt := f.Decls[0].(*ast.FuncDecl).Body.List[0]
	clearPositions(stmt)
	return stmt
}

// The SIMD memory access e is, or nil.
func simdAccess(e ast.Expr) *ast.CallExpr {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return nil
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || simdAccessSizes[strings.TrimPrefix(id.Name, SIMDPrefix)] == 0 {
		return nil
	}
	return call
}

// The assignment of the array pointer of the access call to its variable;
// the call's memory and address operands become the variable.
func (l *simdLowerer) pointer(call *ast.CallExpr) ast.Stmt {
	op := strings.TrimPrefix(call.Fun.(*ast.Ident).Name, SIMDPrefix)
	size := simdAccessSizes[op]
	l.ptrs[size] = true
	mem := call.Args[0]
	switch mem.(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.ParenExpr:
	default:
		mem = &ast.ParenExpr{X: mem}
	}
	ptr := arrayPointer(mem, call.Args[1], size, l.capIsLen)
	name := simdPointerNames[size]
	call.Args = append([]ast.Expr{ast.NewIdent(name)}, call.Args[2:]...)
	return &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.ASSIGN, Rhs: []ast.Expr{ptr}}
}

// rewrite replaces, in n, every call of a SIMD operation with the target's
// code, operands first.
func (l *simdLowerer) rewrite(n ast.Node) {
	astutil.Apply(n, nil, func(c *astutil.Cursor) bool {
		if call, ok := c.Node().(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && IsSIMDOp(id.Name) {
				c.Replace(l.expr(call))
			}
		}
		return true
	})
}

// The target's code of a call of a SIMD operation other than a store,
// whose operands are already the target's code.
func (l *simdLowerer) expr(call *ast.CallExpr) ast.Expr {
	op := strings.TrimPrefix(call.Fun.(*ast.Ident).Name, SIMDPrefix)
	var src string
	if l.target != SIMDPortable {
		src = archsimdOp(l.target, op, call.Args)
	}
	if src == "" {
		src = l.portable().op(op, call.Args)
	}
	return mustParseExpr(src)
}

// The statements of a store, whose operands (the pointer variable first)
// are the translator's: variables and constants, and the lane.
func (l *simdLowerer) store(op string, args []ast.Expr) []string {
	if l.target != SIMDPortable {
		if s := archsimdStore(l.target, op, args); s != nil {
			return s
		}
	}
	return l.portable().store(op, args)
}
