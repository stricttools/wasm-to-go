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
// portable body, and returns the number of calls it replaced. Each access
// goes through an array pointer converted from a slice of the memory,
// which holds its whole bounds check:
//
//	(*[4]byte)(mem[uint64(a) : uint64(a)+4])
//
// Without bytes, it writes calls of encoding/binary on that array's slice:
//
//	load32(mem, a)       → binary.LittleEndian.Uint32((*[4]byte)(mem[uint64(a) : uint64(a)+4])[:])
//	store32(mem, a, v)   → binary.LittleEndian.PutUint32((*[4]byte)(mem[uint64(a) : uint64(a)+4])[:], v)
//
// With bytes, it writes the byte operations those functions are, so no
// call is left for the Go compiler to inline (p is the array pointer
// above, written out at each byte):
//
//	load32(mem, a)       → uint32(p[3])<<24 | uint32(p[0]) | uint32(p[1])<<8 | uint32(p[2])<<16
//	store32(mem, a, v)   → { p[3] = byte(v >> 24); p[0] = byte(v); p[1] = byte(v >> 8); p[2] = byte(v >> 16) }
//
// and likewise for the 16- and 64-bit and the unchecked (-unsafe) helpers.
// Where the access is a statement of its own, the array pointer is a
// variable assigned once instead (hoistAccesses). A store whose value is
// not a variable, a constant, or an operation on them keeps the call. An
// address the translator already writes as a uint64 expression is not
// converted again.
//
// Slicing checks its bounds against the memory's capacity, so it needs
// the capacity to equal the length. capIsLen reports that the translator
// guarantees it (an owned memory: see the README's section on linear
// memory); otherwise (an imported or a shared memory, whose slice the host
// may hold with spare capacity) the memory is first cut to its length,
// mem[:len(mem):len(mem)], which never panics.
//
// Why: the Go compiler keeps two compares for the array pointer and none
// for the constant indexes into it. A slice of the memory from the address
// (mem[a:]) keeps a check of the start, a check of the last byte's index,
// and the masking of the new slice's pointer, and holds the memory's
// length and capacity live: written as bytes over it, QuickJS's game frame
// took about 1.5 times as long, and encoding/binary's call over it is no
// faster (see the README's section on memory accesses).
//
// Why not a helper function: the Go compiler gives every inlined call its
// own copies of the callee's parameters, named variables with DWARF
// location lists, and an inline mark; a translated module makes tens of
// thousands of memory accesses, and inlining a helper at each one more
// than doubled the compile memory of QuickJS (see the README's compile
// cost section). The call of encoding/binary is what the compiler inlines
// anyway. Into a function the compiler considers big, though, it inlines
// only callees of cost 20 or less, and the 32- and 64-bit functions cost
// more: there the translator writes the bytes.
//
// # Why this preserves behavior
//
// The checked helpers of helpers.go access mem[a:] through
// encoding/binary, which checks the last byte's index first, then accesses
// the bytes in little-endian order: they panic if and only if a+size >
// len(mem), before any byte is accessed. The address is a uint32, or a
// uint32 plus an offset below 2^32 as a uint64, so in uint64 a+size does
// not overflow, and the slice panics if and only if a > a+size (never) or
// a+size > cap(mem) = len(mem): for the same addresses, before
// any byte is accessed (a store's first byte statement makes the check,
// and the others repeat it). Converting a slice of length size to a
// pointer to an array of size bytes never panics, and the constant
// indexes are inside the array. The byte operations are the functions'
// own, which the compiler combines into one access. The -unsafe helpers
// (helpers_unsafe.go) are used only in the file built on the platforms
// outside passes.ExpandPlatforms (the expanded file is written by Expand,
// not by Lower): there the checked helpers perform one bounds check of the
// last byte and an unaligned access, which fails for the same addresses.
// An unchecked helper is used only where RemoveBoundsChecks proved an
// earlier check covers the access, so the check the body adds never
// fails. Evaluation order is unchanged: the address and the value are
// evaluated before the access, as the call's arguments are; repeating
// them evaluates the same operations on the same variables, which nothing
// assigns in between.
func Lower(fn *ast.FuncDecl, bytes, capIsLen bool) int {
	if fn.Body == nil {
		return 0
	}
	sites := 0
	if bytes {
		sites += hoistAccesses(fn, capIsLen)
	}
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
		addr := call.Args[1]
		ptr := func() ast.Expr { return arrayPointer(mem, addr, size, capIsLen) }
		sites++
		switch {
		case bytes && !store:
			c.Replace(loadBytes(ptr, size))
		case bytes && store && pureValue(call.Args[2]):
			c.Replace(storeBytes(ptr, call.Args[2], size))
		default:
			args := []ast.Expr{&ast.SliceExpr{X: ptr()}}
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

// (*[size]byte)(mem[a : a+size]), with a the address as a uint64, and mem
// cut to its length first unless capIsLen. A third index (a+size as the
// capacity too) would change nothing the conversion uses, and it made
// QuickJS's largest compile take a quarter more memory.
func arrayPointer(mem, addr ast.Expr, size int, capIsLen bool) ast.Expr {
	lo := func() ast.Expr {
		if isUint64(addr) {
			return addr
		}
		return &ast.CallExpr{Fun: ast.NewIdent("uint64"), Args: []ast.Expr{addr}}
	}
	hi := func() ast.Expr { return &ast.BinaryExpr{X: lo(), Op: token.ADD, Y: lit(size)} }
	if !capIsLen {
		n := func() ast.Expr { return &ast.CallExpr{Fun: ast.NewIdent("len"), Args: []ast.Expr{mem}} }
		mem = &ast.SliceExpr{X: mem, High: n(), Max: n(), Slice3: true}
	}
	return &ast.CallExpr{
		Fun:  &ast.ParenExpr{X: &ast.StarExpr{X: &ast.ArrayType{Len: lit(size), Elt: ast.NewIdent("byte")}}},
		Args: []ast.Expr{&ast.SliceExpr{X: mem, Low: lo(), High: hi()}}}
}

// Reports whether an address is written as a uint64: a conversion to
// uint64, or a sum whose first operand is one (uint64(uint32(v))+8).
func isUint64(e ast.Expr) bool {
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.ADD {
		e = b.X
	}
	c, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := c.Fun.(*ast.Ident)
	return ok && id.Name == "uint64"
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

// uintN(p[last])<<8*last | uintN(p[0]) | uintN(p[1])<<8 | ...
func loadBytes(ptr func() ast.Expr, size int) ast.Expr {
	var e ast.Expr
	for _, i := range byteOrder(size) {
		var b ast.Expr = &ast.CallExpr{Fun: ast.NewIdent(uintType(size)),
			Args: []ast.Expr{&ast.IndexExpr{X: ptr(), Index: lit(i)}}}
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

// { p[last] = byte(v >> 8*last); p[0] = byte(v); ... }
func storeBytes(ptr func() ast.Expr, v ast.Expr, size int) ast.Stmt {
	blk := &ast.BlockStmt{}
	for _, i := range byteOrder(size) {
		var b ast.Expr = v
		if i > 0 {
			b = &ast.BinaryExpr{X: v, Op: token.SHR, Y: lit(8 * i)}
		}
		blk.List = append(blk.List, &ast.AssignStmt{
			Lhs: []ast.Expr{&ast.IndexExpr{X: ptr(), Index: lit(i)}},
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
			if !ok || pkg.Name != "math" {
				return false
			}
			switch f.Sel.Name {
			case "Float32bits", "Float64bits", "Float32frombits", "Float64frombits":
				return true
			}
		}
	}
	return false
}

// The names of the variables hoistAccesses declares: a pointer to the
// array of each access size (aN), and the value of each store size (wN).
var hoistNames = map[int][2]string{2: {"a2", "w2"}, 4: {"a4", "w4"}, 8: {"a8", "w8"}}

// hoistAccesses writes, in a function whose accesses Lower writes as bytes,
// the array pointer of an access once, in a variable of the function,
// instead of at each byte, and returns the number of accesses it wrote:
//
//	t1 := int32(load32(mem, a))
//	store32(mem, b, uint32(t1+t2))
//
// becomes
//
//	var a4 *[4]byte
//	var w4 uint32
//	...
//	a4 = (*[4]byte)(mem[uint64(a) : uint64(a)+4])
//	t1 := int32(uint32(a4[3])<<24 | uint32(a4[0]) | uint32(a4[1])<<8 | uint32(a4[2])<<16)
//	a4 = (*[4]byte)(mem[uint64(b) : uint64(b)+4])
//	w4 = uint32(t1 + t2)
//	a4[3] = byte(w4 >> 24)
//	...
//
// It writes the access of a statement of a statement list (a label stays
// on the statement's first part) that is an assignment of one variable
// whose value holds one load and is otherwise a pureValue, or a store
// whose address and value are pureValues; every other access, and every
// access of a function already using one of the names, is left to Lower's
// form. A store's value goes to the variable unless it is a variable or a
// constant.
//
// Why: Lower's form repeats the slice expression at each byte, and the
// value at each byte of a store, which made the translated source of
// onnxruntime's largest functions several times larger than their
// operations. The compiler combines the bytes as it did, since they index
// one pointer.
//
// # Why this preserves behavior
//
// The statement evaluates the address, the value of a store, and the
// operands around a load before the access, and none of them can panic or
// has an effect (pureValue: no call but conversions, no division, no
// memory access); the assignment of the pointer performs the access's
// whole bounds check, as the first byte's slice did, before any byte is
// accessed. So the statements panic for the same addresses before
// changing anything, and otherwise compute the same values. The variables
// are declared first in the function, before any label, so no goto jumps
// over their declarations.
func hoistAccesses(fn *ast.FuncDecl, capIsLen bool) int {
	used := false
	ast.Inspect(fn, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			for _, names := range hoistNames {
				if id.Name == names[0] || id.Name == names[1] {
					used = true
				}
			}
		}
		return !used
	})
	if used {
		return 0
	}
	h := &hoister{capIsLen: capIsLen, ptrs: map[int]bool{}, vals: map[int]bool{}}
	postApplyStmts(fn.Body, func(list []ast.Stmt) []ast.Stmt {
		var out []ast.Stmt
		for _, s := range list {
			out = append(out, h.stmt(s)...)
		}
		return out
	})
	var decls []ast.Stmt
	for _, size := range []int{2, 4, 8} {
		names := hoistNames[size]
		if h.ptrs[size] {
			decls = append(decls, varDecl(names[0], &ast.StarExpr{X: &ast.ArrayType{Len: lit(size), Elt: ast.NewIdent("byte")}}))
		}
		if h.vals[size] {
			decls = append(decls, varDecl(names[1], ast.NewIdent(uintType(size))))
		}
	}
	fn.Body.List = append(decls, fn.Body.List...)
	return h.sites
}

func varDecl(name string, typ ast.Expr) ast.Stmt {
	return &ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{
		&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent(name)}, Type: typ}}}}
}

type hoister struct {
	capIsLen   bool
	ptrs, vals map[int]bool // the sizes whose variables are used
	sites      int
}

// The statements s becomes.
func (h *hoister) stmt(s ast.Stmt) []ast.Stmt {
	switch s := s.(type) {
	case *ast.LabeledStmt:
		parts := h.stmt(s.Stmt)
		s.Stmt = parts[0]
		return append([]ast.Stmt{s}, parts[1:]...)
	case *ast.ExprStmt:
		call, ok := s.X.(*ast.CallExpr)
		if !ok {
			break
		}
		size, store, ok := accessCall(call)
		if !ok || !store || !pureValue(call.Args[1]) || !pureValue(call.Args[2]) {
			break
		}
		names := hoistNames[size]
		out := []ast.Stmt{h.pointer(call, size)}
		v := call.Args[2]
		switch v.(type) {
		case *ast.Ident, *ast.BasicLit:
		default:
			h.vals[size] = true
			out = append(out, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(names[1])}, Tok: token.ASSIGN, Rhs: []ast.Expr{v}})
			v = ast.NewIdent(names[1])
		}
		p := func() ast.Expr { return ast.NewIdent(names[0]) }
		return append(out, storeBytes(p, v, size).(*ast.BlockStmt).List...)
	case *ast.AssignStmt:
		if len(s.Lhs) != 1 || len(s.Rhs) != 1 || (s.Tok != token.ASSIGN && s.Tok != token.DEFINE) {
			break
		}
		if _, ok := s.Lhs[0].(*ast.Ident); !ok {
			break
		}
		var load *ast.CallExpr
		loads := 0
		ast.Inspect(s.Rhs[0], func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if _, store, ok := accessCall(call); ok && !store {
					load = call
					loads++
				}
			}
			return true
		})
		if loads != 1 || !pureValue(load.Args[1]) {
			break
		}
		size := lowerSizes[load.Fun.(*ast.Ident).Name]
		hole := ast.NewIdent(hoistNames[size][0])
		rhs := replaceExpr(s.Rhs[0], load, hole)
		if !pureValue(rhs) {
			s.Rhs[0] = replaceExpr(rhs, hole, load)
			break
		}
		ptr := h.pointer(load, size)
		s.Rhs[0] = replaceExpr(rhs, hole, loadBytes(func() ast.Expr { return ast.NewIdent(hoistNames[size][0]) }, size))
		return []ast.Stmt{ptr, s}
	}
	return []ast.Stmt{s}
}

// The assignment of the array pointer of the access call, of size bytes.
func (h *hoister) pointer(call *ast.CallExpr, size int) ast.Stmt {
	h.sites++
	h.ptrs[size] = true
	mem := call.Args[0]
	switch mem.(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.ParenExpr:
	default:
		mem = &ast.ParenExpr{X: mem}
	}
	return &ast.AssignStmt{
		Lhs: []ast.Expr{ast.NewIdent(hoistNames[size][0])},
		Tok: token.ASSIGN,
		Rhs: []ast.Expr{arrayPointer(mem, call.Args[1], size, h.capIsLen)}}
}

// Reports the size of the access a call of a memory access helper makes,
// and whether it is a store; ok is false for any other call.
func accessCall(call *ast.CallExpr) (size int, store, ok bool) {
	id, isID := call.Fun.(*ast.Ident)
	if !isID || lowerFuncs[id.Name] == "" {
		return 0, false, false
	}
	store = id.Name[0] == 's'
	if len(call.Args) != 2+btoi(store) {
		return 0, false, false
	}
	return lowerSizes[id.Name], store, true
}

// e with the node old replaced by repl (e itself if it is old).
func replaceExpr(e, old, repl ast.Expr) ast.Expr {
	if e == old {
		return repl
	}
	astutil.Apply(e, func(c *astutil.Cursor) bool {
		if c.Node() == old {
			c.Replace(repl)
			return false
		}
		return true
	}, nil)
	return e
}
