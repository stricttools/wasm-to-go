package passes

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/ast/astutil"
)

// MemLocalName is the local variable MemLocal caches the memory in.
const MemLocalName = "mem"

// MemLocal caches the linear memory's slice in a local variable. In a
// method that reads the memory (the expression isMem recognizes, *m.memory
// or m.memory), it adds
//
//	mem := *m.memory
//
// as the first statement, replaces every read of the memory with mem, and
// after every statement whose call can grow memory (as grows reports) adds
//
//	mem = *m.memory
//
// memory is the expression to load. MemLocal reports whether it changed fn;
// it leaves fn alone if any call that can grow memory is not in a shape
// listed below, or if the memory is used other than by reading it (or by
// taking its address as the argument of a call that can grow memory).
//
// Why: *m.memory is two dependent loads (the field, then the slice header),
// and the Go compiler reloads them after every call and every store through
// unsafe.Pointer, since it cannot prove the header is unchanged. A local
// slice that is never address-taken lives in registers.
//
// # Why this preserves behavior
//
// Invariant: wherever mem is read, it equals the memory's slice (same data
// pointer, length, and capacity). It holds after the first statement. The
// slice changes only when memory grows: in this function, only through a
// call that can grow memory, since a Module is not safe for concurrent use
// (shared memories are left alone by the translator), and the host runs
// only between the module's calls or inside its imports, which the grows
// analysis treats as able to grow memory. Byte contents are shared between
// mem and the memory (the same backing array), so stores through either are
// visible through both; only the slice header can go stale.
//
// Every call that can grow memory must be the whole right-hand side of its
// statement, `call`, `x, y := call`, `x = T(call)` (identifiers only on the
// left), or `return call`, sitting directly in a statement list (possibly
// labeled). Then everything the statement reads from memory is read in the
// call's arguments, before the call, while mem is still valid; and the
// reload directly follows the statement, so no read of mem observes a stale
// header. A call that grows memory nested anywhere else (where Go leaves the
// order of evaluation of operands unspecified) makes MemLocal leave fn alone.
//
// Panics are unaffected: the same accesses are made in the same order, with
// the same bounds, against the same slice.
func MemLocal(fn *ast.FuncDecl, isMem func(ast.Expr) bool, memory ast.Expr, grows func(*ast.CallExpr) bool) bool {
	if fn.Recv == nil || fn.Body == nil {
		return false
	}

	// Check every use of the memory, and of the name mem.
	var reads int
	ok := true
	astutil.Apply(fn.Body, func(c *astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.Ident:
			if n.Name == MemLocalName {
				ok = false
			}
		case *ast.UnaryExpr:
			// &memory is allowed as the argument of a growing call (memory_grow).
			if n.Op == token.AND && isMem(n.X) {
				if call, isCall := c.Parent().(*ast.CallExpr); !isCall || !grows(call) {
					ok = false
				}
				return false
			}
		}
		if e, isExpr := c.Node().(ast.Expr); isExpr && isMem(e) {
			if !ReadOnly(e, c.Parent()) {
				ok = false
			}
			reads++
			return false
		}
		return ok
	}, nil)
	if !ok || reads == 0 {
		return false
	}

	// Find the statements of the calls that can grow memory.
	var growing int
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if call, isCall := n.(*ast.CallExpr); isCall && grows(call) {
			growing++
		}
		return true
	})
	reload := map[ast.Stmt]bool{} // statements to follow with a reload
	matched := 0
	forEachStmt(fn.Body, func(s ast.Stmt) {
		inner := s
		for {
			ls, isLabel := inner.(*ast.LabeledStmt)
			if !isLabel {
				break
			}
			inner = ls.Stmt
		}
		var call *ast.CallExpr
		switch st := inner.(type) {
		case *ast.ExprStmt:
			call, _ = st.X.(*ast.CallExpr)
		case *ast.ReturnStmt:
			if len(st.Results) == 1 {
				call = unconvert(st.Results[0])
			}
		case *ast.AssignStmt:
			if len(st.Rhs) != 1 || st.Tok != token.ASSIGN && st.Tok != token.DEFINE {
				return
			}
			for _, l := range st.Lhs {
				if !is[*ast.Ident](l) {
					return
				}
			}
			call = unconvert(st.Rhs[0])
		}
		if call == nil || !grows(call) {
			return
		}
		matched++
		if !is[*ast.ReturnStmt](inner) {
			reload[s] = true
		}
	})
	if matched != growing {
		return false
	}

	// Rewrite.
	mem := ast.NewIdent(MemLocalName)
	astutil.Apply(fn.Body, func(c *astutil.Cursor) bool {
		if u, isUnary := c.Node().(*ast.UnaryExpr); isUnary && u.Op == token.AND && isMem(u.X) {
			return false
		}
		if e, isExpr := c.Node().(ast.Expr); isExpr && isMem(e) {
			c.Replace(mem)
			return false
		}
		return true
	}, nil)
	postApplyStmts(fn.Body, func(stmts []ast.Stmt) []ast.Stmt {
		var out []ast.Stmt
		for _, s := range stmts {
			out = append(out, s)
			if reload[s] {
				out = append(out, &ast.AssignStmt{
					Lhs: []ast.Expr{mem}, Tok: token.ASSIGN, Rhs: []ast.Expr{memory}})
			}
		}
		return out
	})
	fn.Body.List = append([]ast.Stmt{&ast.AssignStmt{
		Lhs: []ast.Expr{mem}, Tok: token.DEFINE, Rhs: []ast.Expr{memory}}},
		fn.Body.List...)
	return true
}

// Returns the call e is, possibly under one conversion.
func unconvert(e ast.Expr) *ast.CallExpr {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return nil
	}
	if id, ok := call.Fun.(*ast.Ident); ok && safeConversions.has(id.Name) && len(call.Args) == 1 {
		if inner, ok := call.Args[0].(*ast.CallExpr); ok {
			return inner
		}
	}
	return call
}

// ReadOnly reports whether expr, a child of parent, is only read there:
// not assigned, incremented, ranged into, nor address-taken.
func ReadOnly(expr ast.Node, parent ast.Node) bool {
	switch p := parent.(type) {
	case *ast.UnaryExpr:
		return p.Op != token.AND
	case *ast.AssignStmt:
		for _, l := range p.Lhs {
			if l == expr {
				return false
			}
		}
	case *ast.IncDecStmt:
		return p.X != expr
	case *ast.RangeStmt:
		return p.Key != expr && p.Value != expr
	}
	return true
}

// Calls fn for every statement of every statement list in n.
func forEachStmt(n ast.Node, fn func(ast.Stmt)) {
	ast.Inspect(n, func(n ast.Node) bool {
		var list []ast.Stmt
		switch n := n.(type) {
		case *ast.BlockStmt:
			list = n.List
		case *ast.CaseClause:
			list = n.Body
		case *ast.CommClause:
			list = n.Body
		}
		for _, s := range list {
			fn(s)
		}
		return true
	})
}
