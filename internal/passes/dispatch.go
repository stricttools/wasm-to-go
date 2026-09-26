package passes

import (
	"go/ast"
	"go/token"
	"strconv"
)

// A DispatchSite describes an indirect call through a closed table:
// the direct calls it can make, and the Go types of its results.
type DispatchSite struct {
	Cases   []DispatchCase
	Results []ast.Expr
}

// A DispatchCase is one function of a closed table:
// the slots that hold it, and the expression that calls it directly.
type DispatchCase struct {
	Slots []uint64
	Fun   ast.Expr
}

// Dispatch replaces indirect calls through closed tables,
//
//	t1 := m.t0[uint(v0)].(func(int32) int32)(v1)
//
// with a switch over the slots holding a function of the asserted type,
// each calling that function directly, and the original call as default:
//
//	var t1 int32
//	switch t2 := uint(v0); t2 {
//	case 2, 7:
//		t1 = m._f(v1)
//	case 5:
//		t1 = _g(v1)
//	default:
//		t1 = m.t0[t2].(func(int32) int32)(v1)
//	}
//
// site reports the cases of an indirect call, or nil to leave it alone;
// newTemp returns a fresh variable name. Dispatch returns the number of
// calls rewritten.
//
// # Why this preserves behavior
//
//  1. A closed table (see the translator's closed-table rule) is defined by
//     the module, neither imported nor exported, and no instruction mutates
//     it: after New applies the active element segments, slot k holds the
//     function the segments put there, forever. For each of those, site
//     lists the slots whose function has the asserted Go type.
//  2. For a listed slot, the original call finds that function's value
//     (a method value bound to the Module New created, or a plain
//     function), passes the type assertion, and calls it. Calling the
//     function directly on the receiver m is the same call, provided m is
//     the Module New bound the method value to: Modules must be created by
//     New and never copied, a precondition the generated package documents.
//  3. Every other slot (a null slot, one of another type, an index out of
//     range) takes the default branch, which is the original expression, and
//     so panics as before.
//  4. Evaluation order: only calls whose index and arguments are TrapFree
//     (no side effects, no panics, reading only locals) are rewritten, so
//     evaluating the index once in the switch header and the arguments in
//     the chosen case cannot be told apart from the original order.
//  5. Statement shape: only calls that are a whole statement,
//     `call`, `x, y := call`, `x, y = call`, or `return call`, sitting
//     directly in a statement list (possibly labeled), are rewritten.
//     `x := call` becomes `var x T` at the same position, which has the same
//     scope and the same goto restrictions; a label moves to that
//     first statement.
//  6. A call node reached twice in the tree (shared by an earlier pass)
//     is left alone, since the default branch reuses the original node.
func Dispatch(fn *ast.FuncDecl, site func(*ast.CallExpr) *DispatchSite, newTemp func() *ast.Ident) int {
	if fn.Body == nil {
		return 0
	}
	seen := map[*ast.CallExpr]int{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			seen[call]++
		}
		return true
	})

	var count int
	postApplyStmts(fn.Body, func(stmts []ast.Stmt) []ast.Stmt {
		var out []ast.Stmt
		for i, s := range stmts {
			repl := dispatchStmt(s, seen, site, newTemp)
			if repl == nil {
				if out != nil {
					out = append(out, s)
				}
				continue
			}
			if out == nil {
				out = append(out, stmts[:i]...)
			}
			out = append(out, repl...)
			count++
		}
		if out == nil {
			return stmts
		}
		return out
	})
	return count
}

// Returns the statements replacing s, or nil to leave s alone.
func dispatchStmt(s ast.Stmt, seen map[*ast.CallExpr]int, site func(*ast.CallExpr) *DispatchSite, newTemp func() *ast.Ident) []ast.Stmt {
	var label *ast.LabeledStmt
	inner := s
	for {
		ls, ok := inner.(*ast.LabeledStmt)
		if !ok {
			break
		}
		label, inner = ls, ls.Stmt
	}

	var call *ast.CallExpr
	var wrap func(*ast.CallExpr) ast.Stmt
	var define *ast.AssignStmt
	switch st := inner.(type) {
	case *ast.ExprStmt:
		call, _ = st.X.(*ast.CallExpr)
		wrap = func(c *ast.CallExpr) ast.Stmt { return &ast.ExprStmt{X: c} }
	case *ast.ReturnStmt:
		if len(st.Results) == 1 {
			call, _ = st.Results[0].(*ast.CallExpr)
		}
		wrap = func(c *ast.CallExpr) ast.Stmt { return &ast.ReturnStmt{Results: []ast.Expr{c}} }
	case *ast.AssignStmt:
		if len(st.Rhs) != 1 || st.Tok != token.ASSIGN && st.Tok != token.DEFINE {
			return nil
		}
		for _, l := range st.Lhs {
			if !is[*ast.Ident](l) {
				return nil
			}
		}
		call, _ = st.Rhs[0].(*ast.CallExpr)
		if st.Tok == token.DEFINE {
			define = st
		}
		wrap = func(c *ast.CallExpr) ast.Stmt {
			return &ast.AssignStmt{Lhs: append([]ast.Expr(nil), st.Lhs...), Tok: token.ASSIGN, Rhs: []ast.Expr{c}}
		}
	}
	if call == nil || seen[call] != 1 || call.Ellipsis.IsValid() {
		return nil
	}
	assert, ok := call.Fun.(*ast.TypeAssertExpr)
	if !ok {
		return nil
	}
	index, ok := assert.X.(*ast.IndexExpr)
	if !ok || !TrapFree(index.Index) {
		return nil
	}
	for _, arg := range call.Args {
		if !TrapFree(arg) {
			return nil
		}
	}
	ds := site(call)
	if ds == nil || len(ds.Cases) == 0 {
		return nil
	}

	var stmts []ast.Stmt
	if define != nil {
		if len(define.Lhs) != len(ds.Results) {
			return nil
		}
		for i, l := range define.Lhs {
			if id := l.(*ast.Ident); id.Name != "_" {
				stmts = append(stmts, &ast.DeclStmt{Decl: &ast.GenDecl{
					Tok: token.VAR,
					Specs: []ast.Spec{&ast.ValueSpec{
						Names: []*ast.Ident{id},
						Type:  ds.Results[i]}}}})
			}
		}
	}

	tmp := newTemp()
	sw := &ast.SwitchStmt{
		Init: &ast.AssignStmt{Lhs: []ast.Expr{tmp}, Tok: token.DEFINE, Rhs: []ast.Expr{index.Index}},
		Tag:  tmp,
		Body: &ast.BlockStmt{},
	}
	for _, c := range ds.Cases {
		clause := &ast.CaseClause{}
		for _, slot := range c.Slots {
			clause.List = append(clause.List, &ast.BasicLit{Kind: token.INT, Value: strconv.FormatUint(slot, 10)})
		}
		args := make([]ast.Expr, len(call.Args))
		for i, arg := range call.Args {
			args[i] = cloneTrapFree(arg)
		}
		clause.Body = []ast.Stmt{wrap(&ast.CallExpr{Fun: c.Fun, Args: args})}
		sw.Body.List = append(sw.Body.List, clause)
	}
	index.Index = tmp
	sw.Body.List = append(sw.Body.List, &ast.CaseClause{Body: []ast.Stmt{wrap(call)}})
	stmts = append(stmts, sw)

	if label != nil {
		// Keep the label chain, moving it onto the first statement.
		label.Stmt = stmts[0]
		stmts[0] = s
	}
	return stmts
}
