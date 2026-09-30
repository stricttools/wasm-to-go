package main

import (
	"github.com/stricttools/wasm-to-go/internal/callgraph"
	"github.com/stricttools/wasm-to-go/internal/mangle"
	"go/ast"
	"go/token"
	"slices"
	"strconv"

	"golang.org/x/tools/go/ast/astutil"
)

// The Go stack bound. Every Wasm call is a Go call, and Go stops a program
// whose goroutine stack outgrows its maximum with a fatal error that no
// recover catches, so a module that recurses without end (or deeper than
// the host can afford) must trap before that. The recursive functions
// (those in a cycle of the call graph) count the Go stack they take in
// the Module's stackUsed field: each adds an estimate of its frame when it
// starts, traps with "call stack exhausted" when the sum exceeds maxStack,
// and subtracts the estimate when it returns. Functions outside every cycle
// add nothing: a chain of them is as deep as the call graph allows, at
// most. A trap leaves stackUsed counting the frames it unwound, like the
// rest of a trapped module's state.

// maxStack is the Go stack, in estimated bytes (frameEstimate), the
// recursive functions of a module may take together, and maxFrames the
// most frames of them that may be live: each frame is charged at least
// maxStack/maxFrames. See the README's section on the Go stack for how
// both were chosen.
const (
	maxStack  = 192 << 20
	maxFrames = 200_000
)

// The Module's field of the bound, and the trap.
const (
	stackUsedField = "stackUsed"
	stackTrap      = "call stack exhausted"
)

// frameEstimate estimates the Go stack frame of fn in bytes: 8 bytes for
// each parameter, result, and local variable, and 64 for the return
// address, frame pointer, and spill space. Every translated value is at
// most 8 bytes; the Go compiler can share slots between variables, so this
// is an upper estimate of what the variables take, not a measurement.
func frameEstimate(fn *ast.FuncDecl) int64 {
	n := int64(0)
	count := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			n += int64(max(len(f.Names), 1))
		}
	}
	count(fn.Type.Params)
	count(fn.Type.Results)
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		switch s := node.(type) {
		case *ast.ValueSpec:
			n += int64(len(s.Names))
		case *ast.AssignStmt:
			if s.Tok == token.DEFINE {
				n += int64(len(s.Lhs))
			}
		}
		return true
	})
	return 8*n + 64
}

// boundStack instruments the recursive functions (see above). It runs once
// every function is translated and optimized, before the Module type is
// created: it reports whether any function is recursive, so the Module
// needs the fields.
func (t *translator) boundStack() bool {
	// The functions with code, by their shared reference (fn.call), which
	// every call, table element, and ref.func of the function uses.
	byRef := map[ast.Expr]int{}
	for i := range t.functions {
		if fn := &t.functions[i]; fn.decl != nil && fn.decl.Body != nil && fn.call != nil {
			byRef[fn.call] = i
		}
	}
	// Functions a table may hold: those in element segments, and those a
	// function body refers to other than by calling them (ref.func).
	tabled := map[int]bool{}
	for _, seg := range t.elements {
		for _, e := range seg.init {
			if i, ok := byRef[e]; ok {
				tabled[i] = true
			}
		}
	}
	calls := make([][]int, len(t.functions))
	indirect := make([][]funcType, len(t.functions))
	for i := range t.functions {
		fn := &t.functions[i]
		if fn.decl == nil || fn.decl.Body == nil {
			continue
		}
		seen := map[int]bool{}
		astutil.Apply(fn.decl.Body, func(c *astutil.Cursor) bool {
			switch n := c.Node().(type) {
			case *ast.FuncLit:
				return false
			case *ast.CallExpr:
				if ic, ok := t.indirect[n]; ok {
					indirect[i] = append(indirect[i], ic.typ)
				}
			case *ast.ParenExpr:
				j, ok := byRef[n]
				if !ok {
					break
				}
				if !seen[j] {
					seen[j] = true
					calls[i] = append(calls[i], j)
				}
				// A reference other than a call puts the function
				// where an indirect call may find it (ref.func).
				if _, isCall := c.Parent().(*ast.CallExpr); !isCall || c.Name() != "Fun" {
					tabled[j] = true
				}
				return false
			}
			return true
		}, nil)
	}
	// An indirect call may reach every function of its type a table may hold.
	for i, types := range indirect {
		for _, typ := range types {
			for j := range t.functions {
				if tabled[j] && t.functions[j].typ == typ && !slices.Contains(calls[i], j) {
					calls[i] = append(calls[i], j)
				}
			}
		}
	}
	recursive := callgraph.Recursive(calls)
	if len(recursive) == 0 {
		return false
	}
	// A recursive function needs the Module, and so does every function
	// calling one: give back the receiver RemoveReceiver took. The shared
	// reference makes every call a method call again.
	needs := map[int]bool{}
	var mark func(i int)
	callers := make([][]int, len(t.functions))
	for i, cs := range calls {
		for _, j := range cs {
			callers[j] = append(callers[j], i)
		}
	}
	mark = func(i int) {
		if needs[i] {
			return
		}
		needs[i] = true
		for _, c := range callers[i] {
			mark(c)
		}
	}
	for i := range recursive {
		mark(i)
	}
	for i := range needs {
		fn := &t.functions[i]
		if fn.decl.Recv == nil {
			fn.decl.Recv = modRecvList
			fn.call.(*ast.ParenExpr).X = &ast.SelectorExpr{X: newID("m"), Sel: fn.decl.Name}
		}
	}
	// An export that reaches recursion restores the count when it returns,
	// normally or by a trap: a trap unwinds frames that never subtract
	// their charge, and the module stays usable after it, as a Wasm
	// instance does. An exported function the translator named after its
	// export gets its own name back, so the export is a wrapper.
	t.stackEntries = set[int]{}
	for name, exp := range t.exports {
		if exp.kind != externFunction || !needs[exp.index] {
			continue
		}
		fn := &t.functions[exp.index]
		if fn.decl.Name.Name == mangle.Name(name, mangle.Exported) {
			fn.decl.Name.Name = "fn" + strconv.Itoa(exp.index)
		}
		t.stackEntries.add(exp.index)
	}
	idx := make([]int, 0, len(recursive))
	for i := range recursive {
		idx = append(idx, i)
	}
	slices.Sort(idx)
	for _, i := range idx {
		fn := &t.functions[i]
		instrument(fn.decl, receiverName(fn.decl), max(frameEstimate(fn.decl), maxStack/maxFrames))
	}
	return true
}

// instrument adds the bound to fn, whose receiver (the Module) is named
// recv, charging it frame bytes.
func instrument(fn *ast.FuncDecl, recv string, frame int64) {
	used := func() ast.Expr {
		return &ast.SelectorExpr{X: newID(recv), Sel: newID(stackUsedField)}
	}
	size := &ast.BasicLit{Kind: token.INT, Value: strconv.FormatInt(frame, 10)}
	release := func() ast.Stmt {
		return &ast.AssignStmt{Lhs: []ast.Expr{used()}, Tok: token.SUB_ASSIGN, Rhs: []ast.Expr{size}}
	}
	var results []ast.Expr
	if fn.Type.Results != nil {
		for _, f := range fn.Type.Results.List {
			for range max(len(f.Names), 1) {
				results = append(results, f.Type)
			}
		}
	}
	astutil.Apply(fn.Body, func(c *astutil.Cursor) bool {
		// A return inside a function literal is not fn's.
		_, lit := c.Node().(*ast.FuncLit)
		return !lit
	}, func(c *astutil.Cursor) bool {
		ret, ok := c.Node().(*ast.ReturnStmt)
		if !ok {
			return true
		}
		if !simpleResults(ret.Results) {
			// Evaluate the results first: a call among them is fn's
			// frame still in use.
			var stmts []ast.Stmt
			var lhs []ast.Expr
			for k, typ := range results {
				id := newID("rs" + strconv.Itoa(k))
				stmts = append(stmts, &ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{
					&ast.ValueSpec{Names: []*ast.Ident{id}, Type: typ}}}})
				lhs = append(lhs, newID(id.Name))
			}
			stmts = append(stmts, &ast.AssignStmt{Lhs: lhs, Tok: token.ASSIGN, Rhs: ret.Results})
			var rets []ast.Expr
			for _, id := range lhs {
				rets = append(rets, newID(id.(*ast.Ident).Name))
			}
			stmts = append(stmts, release(), &ast.ReturnStmt{Results: rets})
			c.Replace(&ast.BlockStmt{List: stmts})
			return true
		}
		c.Replace(&ast.BlockStmt{List: []ast.Stmt{release(), ret}})
		return true
	})
	entry := &ast.IfStmt{
		Init: &ast.AssignStmt{Lhs: []ast.Expr{used()}, Tok: token.ADD_ASSIGN, Rhs: []ast.Expr{size}},
		Cond: &ast.BinaryExpr{X: used(), Op: token.GTR, Y: &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(maxStack)}},
		Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ExprStmt{X: &ast.CallExpr{
			Fun: newID("panic"), Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(stackTrap)}}}}}},
	}
	body := append([]ast.Stmt{entry}, fn.Body.List...)
	if len(results) == 0 {
		if n := len(body); n == 0 || !endsFunction(body[n-1]) {
			body = append(body, release())
		}
	}
	fn.Body.List = body
}

// simpleResults reports whether evaluating exprs calls no function, so the
// frame can be released before them.
func simpleResults(exprs []ast.Expr) bool {
	simple := true
	for _, e := range exprs {
		ast.Inspect(e, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.CallExpr:
				// A conversion is written as a call too; only a call of
				// something other than a type name costs a frame.
				call := n.(*ast.CallExpr)
				if id, ok := call.Fun.(*ast.Ident); ok && isConversion(id.Name) {
					return true
				}
				simple = false
			case *ast.FuncLit:
				simple = false
			}
			return simple
		})
	}
	return simple
}

func isConversion(name string) bool {
	switch name {
	case "int32", "int64", "uint32", "uint64", "float32", "float64", "int8", "int16", "uint8", "uint16", "byte", "bool", "uintptr", "int", "uint":
		return true
	}
	return false
}

// endsFunction reports whether s leaves the function: a return, or a panic.
func endsFunction(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.ExprStmt:
		if call, ok := s.X.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "panic" {
				return true
			}
		}
	case *ast.BlockStmt:
		return len(s.List) > 0 && endsFunction(s.List[len(s.List)-1])
	}
	return false
}

// restoreStackUsed is an export wrapper's first statement: it sets the
// count back to its value at the call when the call returns or traps.
//
//	defer func(used int64) { m.stackUsed = used }(m.stackUsed)
func restoreStackUsed() ast.Stmt {
	used := func() ast.Expr { return &ast.SelectorExpr{X: newID("m"), Sel: newID(stackUsedField)} }
	return &ast.DeferStmt{Call: &ast.CallExpr{
		Fun: &ast.FuncLit{
			Type: &ast.FuncType{Params: &ast.FieldList{List: []*ast.Field{{Names: []*ast.Ident{newID("used")}, Type: newID("int64")}}}},
			Body: &ast.BlockStmt{List: []ast.Stmt{&ast.AssignStmt{Lhs: []ast.Expr{used()}, Tok: token.ASSIGN, Rhs: []ast.Expr{newID("used")}}}},
		},
		Args: []ast.Expr{used()},
	}}
}
