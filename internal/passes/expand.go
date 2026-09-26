package passes

import (
	"go/ast"
	"go/token"
	"strconv"

	"golang.org/x/tools/go/ast/astutil"
)

// ExpandPlatforms is the build constraint of the expanded file: the
// little-endian platforms with unaligned access (in the helpers' terms,
// unalignedOK && !big).
const ExpandPlatforms = "386 || amd64 || arm64 || loong64 || ppc64le || wasm"

// The type each load and store helper accesses memory as.
var expandTypes = map[string]string{
	"load16": "uint16", "load32": "uint32", "load64": "uint64",
	"store16": "uint16", "store32": "uint32", "store64": "uint64",
	"load16u": "uint16", "load32u": "uint32", "load64u": "uint64",
	"store16u": "uint16", "store32u": "uint32", "store64u": "uint64",
}

// Expand replaces calls of the -unsafe load and store helpers on mem (the
// local MemLocal caches the memory in) with their bodies, written as
// expressions, and returns the number of calls it replaced:
//
//	load32(mem, a)      → *(*uint32)(unsafe.Add(unsafe.Pointer(&mem[uint64(a)+3]), -3))
//	store32(mem, a, v)  → *(*uint32)(unsafe.Add(unsafe.Pointer(&mem[uint64(a)+3]), -3)) = v
//	load32u(mem, a)     → *(*uint32)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(mem)), uintptr(a)))
//	store32u(mem, a, v) → *(*uint32)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(mem)), uintptr(a))) = v
//
// and likewise for 16- and 64-bit accesses.
//
// Why: the Go compiler gives every inlined call an inline mark, and emits a
// one-byte NOP for a mark it cannot attach to an instruction on the call's
// own line; for these helpers, whose only instructions (the check, the load
// or store) carry the helper's positions, that is almost every call site.
// Expanded, there is no call and no NOP.
//
// # Why this preserves behavior
//
// On the platforms of ExpandPlatforms (little-endian, with unaligned access:
// unalignedOK && !big, as the helpers define them), the helpers' bodies are
// these expressions: the checked helper performs `_ = mem[uint64(addr)+S-1]`
// and then the access at addr; &mem[uint64(a)+S-1] performs the same bounds
// check (same index, so the same panic), and unsafe.Add(..., -(S-1)) yields
// the same address, inside the same slice. The translator writes the
// expanded code only in a file restricted to those platforms, beside the
// unexpanded code in a file for every other platform.
//
// Evaluation order: a helper call evaluates mem, a, and v, then checks, then
// accesses. An expanded load is evaluated as an operand of its enclosing
// expression; an expanded store is an assignment whose left operand and
// right-hand side are evaluated before the store. Translated code never
// changes mem or a local inside a statement's operands (no closures, no
// address-taken locals; MemLocal reloads mem only as a statement of its own),
// so every value is the same. What Go leaves unordered is which of two
// failing checks in one statement fires first: the statement panics either
// way, before any of its side effects (a store is the last thing its
// statement does), and only the reported index may differ.
func Expand(fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 0
	}
	var count int
	// Post-order: a load in a store's value is expanded first.
	astutil.Apply(fn.Body, nil, func(c *astutil.Cursor) bool {
		if repl := expandNode(c.Node(), &count); repl != nil {
			c.Replace(repl)
		}
		return true
	})
	return count
}

// Returns the expansion of n, a helper call or the statement of a store
// helper call, or nil.
func expandNode(n ast.Node, count *int) ast.Node {
	var call *ast.CallExpr
	store := false
	switch n := n.(type) {
	case *ast.ExprStmt:
		call, _ = n.X.(*ast.CallExpr)
		store = true
	case *ast.CallExpr:
		call = n
	}
	if call == nil {
		return nil
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || expandTypes[id.Name] == "" || len(call.Args) < 2 || !isIdent(call.Args[0], MemLocalName) {
		return nil
	}
	isStore := id.Name[0] == 's'
	if isStore != store || len(call.Args) != 2+btoi(isStore) {
		return nil
	}

	typ := expandTypes[id.Name]
	size := map[string]int{"uint16": 2, "uint32": 4, "uint64": 8}[typ]
	mem, addr := call.Args[0], call.Args[1]
	var ptr ast.Expr
	if id.Name[len(id.Name)-1] == 'u' {
		ptr = unsafeCall("Add",
			unsafeCall("Pointer", unsafeCall("SliceData", mem)),
			&ast.CallExpr{Fun: ast.NewIdent("uintptr"), Args: []ast.Expr{addr}})
	} else {
		last := &ast.IndexExpr{X: mem, Index: &ast.BinaryExpr{
			Op: token.ADD,
			X:  toUint64(addr),
			Y:  &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(size - 1)}}}
		ptr = unsafeCall("Add",
			unsafeCall("Pointer", &ast.UnaryExpr{Op: token.AND, X: last}),
			&ast.UnaryExpr{Op: token.SUB, X: &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(size - 1)}})
	}
	access := &ast.StarExpr{X: &ast.CallExpr{
		Fun:  &ast.ParenExpr{X: &ast.StarExpr{X: ast.NewIdent(typ)}},
		Args: []ast.Expr{ptr}}}

	*count++
	if isStore {
		return &ast.AssignStmt{Lhs: []ast.Expr{access}, Tok: token.ASSIGN, Rhs: []ast.Expr{call.Args[2]}}
	}
	return access
}

func unsafeCall(name string, args ...ast.Expr) ast.Expr {
	return &ast.CallExpr{
		Fun:  &ast.SelectorExpr{X: ast.NewIdent("unsafe"), Sel: ast.NewIdent(name)},
		Args: args}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Converts an address to uint64, unless it is uint64 already:
// uint64(x), or an operation on it (uint64(uint32(v0))+8).
func toUint64(addr ast.Expr) ast.Expr {
	e := addr
	for {
		switch x := e.(type) {
		case *ast.BinaryExpr:
			e = x.X
			continue
		case *ast.CallExpr:
			if isIdent(x.Fun, "uint64") {
				return addr
			}
		}
		return &ast.CallExpr{Fun: ast.NewIdent("uint64"), Args: []ast.Expr{addr}}
	}
}
