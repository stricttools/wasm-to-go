package passes

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// The size of the access of each checked load and store helper.
var accessSizes = map[string]int64{
	"load16": 2, "load32": 4, "load64": 8,
	"store16": 2, "store32": 4, "store64": 8,
}

// UncheckedHelper names the helper that performs the access of a checked
// load or store helper without its bounds check.
func UncheckedHelper(name string) string { return name + "u" }

// RemoveBoundsChecks replaces load and store helper calls whose bounds
// check an earlier check always covers with their unchecked variants
// (load32 becomes load32u, and so on), and returns the names of the
// unchecked helpers it used. It runs on functions MemLocal rewrote: the
// first statement defines mem, the local every access goes through.
//
// A call such as load32(mem, uint64(uint32(v0))+32) checks that
// uint64(uint32(v0))+32+4 <= len(mem), and panics otherwise. If every path
// to it has already executed a check of uint64(uint32(v0))+K+S <= len(mem)
// with K+S >= 32+4, on the same mem and the same value of v0, its check can
// never fail.
//
// # Why this preserves behavior
//
// A removed check is one that cannot fail, so every execution performs the
// same accesses, the same writes, and the same panics as before. The pass
// proves "cannot fail" with a forward dataflow analysis over the syntax tree:
//
//   - A fact (E, n) states that uint64(uint32(E))+n <= len(mem) holds, where
//     E is a TrapFree expression (no side effects, no panics, reading only
//     locals). A check of address uint64(uint32(E))+K (or uint32(E), K = 0)
//     with access size S establishes (E, K+S) once it has executed; byte
//     accesses mem[addr] establish facts too.
//   - Facts are established only by checks that execute unconditionally once
//     their statement runs (not in the right operand of && or ||). A check is
//     removed only when a fact from an earlier statement, or from a helper
//     call earlier in the same statement, covers it: Go evaluates the calls
//     of a statement in lexical left-to-right order, arguments first.
//   - Copies: after `t := v` or `t = v` the pass records that t equals v
//     until either is assigned, and reads t as v when building E, so an
//     access through the copy t uses and establishes facts about v. A key
//     built through a copy also depends on t, so assigning t kills it.
//   - Assigning any variable of E, or assigning mem (the reload after a call
//     that can grow memory), kills the fact. Variables are identified by
//     name: the pass refuses a function that declares a name twice, and one
//     that takes the address of a local, so only assignments change them.
//   - Control flow: Go forbids a goto from outside a block to a label inside
//     it, so every block is entered only at its start, and any label inside a
//     block is reached only from within that block. The analysis builds the
//     control-flow graph of statements (fall-through, gotos, if, switch,
//     break, and fallthrough), iterates to a fixed point, and keeps, at every
//     statement, only the facts true on all its paths. Loops are gotos; the
//     pass refuses for, range, select, and type switch statements, and
//     labeled break and continue.
//   - A call node the tree reaches more than once (shared by an earlier
//     pass) is rewritten only if its check is covered at every occurrence.
func RemoveBoundsChecks(fn *ast.FuncDecl) []string {
	if fn.Body == nil || len(fn.Body.List) == 0 {
		return nil
	}
	first, ok := fn.Body.List[0].(*ast.AssignStmt)
	if !ok || first.Tok != token.DEFINE || len(first.Lhs) != 1 || !isIdent(first.Lhs[0], MemLocalName) {
		return nil
	}
	if !uniqueLocals(fn) {
		return nil
	}

	a := &analyzer{hits: map[*ast.CallExpr]int{}}
	occurrences := map[*ast.CallExpr]int{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ForStmt, *ast.RangeStmt, *ast.SelectStmt, *ast.TypeSwitchStmt, *ast.FuncLit,
			*ast.GoStmt, *ast.DeferStmt:
			a.fail(fmt.Errorf("%T", n))
		case *ast.UnaryExpr:
			if n.Op == token.AND && is[*ast.Ident](n.X) {
				a.fail(fmt.Errorf("address of a local"))
			}
		case *ast.CallExpr:
			occurrences[n]++
		}
		return a.err == nil
	})
	if a.err != nil {
		return nil
	}
	a.analyze(fn.Body)
	if a.err != nil {
		return nil
	}

	used := set[string]{}
	for call, hits := range a.hits {
		if hits != occurrences[call] {
			continue
		}
		name := UncheckedHelper(call.Fun.(*ast.Ident).Name)
		call.Fun = ast.NewIdent(name)
		used.add(name)
	}
	var names []string
	for name := range used {
		names = append(names, name)
	}
	return names
}

// Reports whether fn declares each name at most once
// (parameters, results, var declarations, and := definitions).
func uniqueLocals(fn *ast.FuncDecl) bool {
	seen := set[string]{}
	ok := true
	declare := func(id *ast.Ident) {
		if id.Name != "_" && !seen.add(id.Name) {
			ok = false
		}
	}
	for _, list := range []*ast.FieldList{fn.Recv, fn.Type.Params, fn.Type.Results} {
		if list != nil {
			for _, f := range list.List {
				for _, id := range f.Names {
					declare(id)
				}
			}
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ValueSpec:
			for _, id := range n.Names {
				declare(id)
			}
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for _, l := range n.Lhs {
					if id, isID := l.(*ast.Ident); isID {
						declare(id)
					}
				}
			}
		}
		return ok
	})
	return ok
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// A fact: uint64(uint32(E))+end <= len(mem), for the E of its key;
// it depends on vars. For copy facts (key "=" + variable), the variable
// holds the same value as rep, and end is unused.
type fact struct {
	end  int64
	vars []string
	rep  string
}

type facts map[string]fact

func (f facts) copy() facts {
	g := make(facts, len(f))
	for k, v := range f {
		g[k] = v
	}
	return g
}

func (f facts) kill(k set[string]) facts {
	if len(k) == 0 {
		return f
	}
	g := make(facts, len(f))
outer:
	for key, v := range f {
		for _, name := range v.vars {
			if k.has(name) {
				continue outer
			}
		}
		g[key] = v
	}
	return g
}

type analyzer struct {
	hits map[*ast.CallExpr]int // redundant occurrences of each check
	cur  facts                 // the facts in force while computing keys
	mark bool                  // the final, marking pass
	err  error
}

func (a *analyzer) fail(err error) {
	if a.err == nil {
		a.err = err
	}
}

// copyOf returns the variable name is a live copy of, if any.
func (a *analyzer) copyOf(name string) string {
	return a.cur["="+name].rep
}

// key canonicalizes a TrapFree expression.
func (a *analyzer) key(e ast.Expr) (string, []string, bool) {
	if !TrapFree(e) {
		return "", nil, false
	}
	var vars []string
	var b strings.Builder
	var walk func(e ast.Expr)
	walk = func(e ast.Expr) {
		switch e := e.(type) {
		case *ast.Ident:
			name := e.Name
			if r := a.copyOf(name); r != "" {
				vars = append(vars, name) // the key depends on the copy fact too
				name = r
			}
			vars = append(vars, name)
			b.WriteString("$" + name)
		case *ast.BasicLit:
			b.WriteString(e.Value)
		case *ast.ParenExpr:
			walk(e.X)
		case *ast.UnaryExpr:
			b.WriteString(e.Op.String() + "(")
			walk(e.X)
			b.WriteString(")")
		case *ast.BinaryExpr:
			b.WriteString("(")
			walk(e.X)
			b.WriteString(e.Op.String())
			walk(e.Y)
			b.WriteString(")")
		case *ast.CallExpr:
			b.WriteString(e.Fun.(*ast.Ident).Name + "(")
			walk(e.Args[0])
			b.WriteString(")")
		}
	}
	walk(e)
	return b.String(), vars, true
}

// addr decomposes an address, uint64(uint32(E))+K or uint32(E),
// into the key of E and K.
func (a *analyzer) addr(e ast.Expr) (string, []string, int64, bool) {
	conv := func(e ast.Expr, to string) (ast.Expr, bool) {
		c, ok := e.(*ast.CallExpr)
		if !ok || len(c.Args) != 1 || !isIdent(c.Fun, to) {
			return nil, false
		}
		return c.Args[0], true
	}
	var k int64
	if be, ok := e.(*ast.BinaryExpr); ok && be.Op == token.ADD {
		lit, ok := be.Y.(*ast.BasicLit)
		if !ok || lit.Kind != token.INT {
			return "", nil, 0, false
		}
		v, err := strconv.ParseInt(lit.Value, 0, 64)
		if err != nil || v < 0 || v > 1<<40 {
			return "", nil, 0, false
		}
		inner, ok := conv(be.X, "uint64")
		if !ok {
			return "", nil, 0, false
		}
		e, k = inner, v
	}
	x, ok := conv(e, "uint32")
	if !ok {
		return "", nil, 0, false
	}
	key, vars, ok := a.key(x)
	return key, vars, k, ok
}

type access struct {
	call *ast.CallExpr // nil for byte accesses
	key  string
	vars []string
	end  int64
	cond bool // evaluated only conditionally within the statement
}

// accesses lists the memory accesses in e (not descending into statements),
// in evaluation order for calls.
func (a *analyzer) accesses(e ast.Node, cond bool, out *[]access) {
	if e == nil {
		return
	}
	switch n := e.(type) {
	case *ast.BinaryExpr:
		a.accesses(n.X, cond, out)
		a.accesses(n.Y, cond || n.Op == token.LAND || n.Op == token.LOR, out)
	case *ast.CallExpr:
		a.accesses(n.Fun, cond, out)
		for _, arg := range n.Args {
			a.accesses(arg, cond, out)
		}
		id, ok := n.Fun.(*ast.Ident)
		if !ok || accessSizes[id.Name] == 0 || len(n.Args) < 2 || !isIdent(n.Args[0], MemLocalName) {
			return
		}
		if key, vars, k, ok := a.addr(n.Args[1]); ok {
			*out = append(*out, access{call: n, key: key, vars: append(vars, MemLocalName), end: k + accessSizes[id.Name], cond: cond})
		}
	case *ast.IndexExpr:
		a.accesses(n.X, cond, out)
		a.accesses(n.Index, cond, out)
		if isIdent(ast.Unparen(n.X), MemLocalName) {
			if key, vars, k, ok := a.addr(n.Index); ok {
				*out = append(*out, access{key: key, vars: append(vars, MemLocalName), end: k + 1, cond: cond})
			}
		}
	case *ast.ParenExpr:
		a.accesses(n.X, cond, out)
	case *ast.UnaryExpr:
		a.accesses(n.X, cond, out)
	case *ast.StarExpr:
		a.accesses(n.X, cond, out)
	case *ast.SelectorExpr:
		a.accesses(n.X, cond, out)
	case *ast.TypeAssertExpr:
		a.accesses(n.X, cond, out)
	case *ast.SliceExpr:
		a.accesses(n.X, cond, out)
		a.accesses(n.Low, cond, out)
		a.accesses(n.High, cond, out)
		a.accesses(n.Max, cond, out)
	case *ast.Ident, *ast.BasicLit, *ast.FuncType, *ast.ArrayType, *ast.InterfaceType:
	default:
		a.fail(fmt.Errorf("unexpected expression %T", e))
	}
}

// A node of the control-flow graph is one statement-level step: it
// evaluates exprs (in order), then assigns the variables in kill.
// succ are the nodes control can reach next.
type node struct {
	exprs []ast.Node
	kill  set[string]
	succ  []int
	// copyDst, copySrc: the node is `dst := src` or `dst = src`,
	// after which dst holds the value of src.
	copyDst, copySrc string
}

type cfg struct {
	a      *analyzer
	nodes  []*node
	labels map[string]int
}

func (g *cfg) add(n *node) int {
	g.nodes = append(g.nodes, n)
	return len(g.nodes) - 1
}

// assigned returns the variables a simple statement assigns or declares
// (a := or var executed again in a loop gives the variable a new value too).
func assigned(s ast.Stmt) set[string] {
	k := set[string]{}
	switch s := s.(type) {
	case *ast.AssignStmt:
		for _, l := range s.Lhs {
			if id, ok := l.(*ast.Ident); ok {
				k.add(id.Name)
			}
		}
	case *ast.IncDecStmt:
		if id, ok := s.X.(*ast.Ident); ok {
			k.add(id.Name)
		}
	case *ast.DeclStmt:
		for _, sp := range s.Decl.(*ast.GenDecl).Specs {
			if vs, ok := sp.(*ast.ValueSpec); ok {
				for _, id := range vs.Names {
					k.add(id.Name)
				}
			}
		}
	}
	return k
}

type jumps struct{ brk, ft int }

func (g *cfg) list(stmts []ast.Stmt, next int, j jumps) int {
	for i := len(stmts) - 1; i >= 0; i-- {
		next = g.stmt(stmts[i], next, j)
	}
	return next
}

func (g *cfg) stmt(s ast.Stmt, next int, j jumps) int {
	a := g.a
	switch s := s.(type) {
	case *ast.AssignStmt:
		var ex []ast.Node
		for _, l := range s.Lhs {
			ex = append(ex, l)
		}
		for _, r := range s.Rhs {
			ex = append(ex, r)
		}
		n := &node{exprs: ex, kill: assigned(s), succ: []int{next}}
		if len(s.Lhs) == 1 && len(s.Rhs) == 1 {
			dst, ok1 := s.Lhs[0].(*ast.Ident)
			src, ok2 := s.Rhs[0].(*ast.Ident)
			if ok1 && ok2 && dst.Name != src.Name && dst.Name != "_" {
				n.copyDst, n.copySrc = dst.Name, src.Name
			}
		}
		return g.add(n)
	case *ast.ExprStmt:
		return g.add(&node{exprs: []ast.Node{s.X}, succ: []int{next}})
	case *ast.IncDecStmt:
		return g.add(&node{exprs: []ast.Node{s.X}, kill: assigned(s), succ: []int{next}})
	case *ast.DeclStmt:
		for _, sp := range s.Decl.(*ast.GenDecl).Specs {
			if vs, ok := sp.(*ast.ValueSpec); ok && len(vs.Values) > 0 {
				a.fail(fmt.Errorf("var with initializer"))
			}
		}
		return g.add(&node{kill: assigned(s), succ: []int{next}})
	case *ast.ReturnStmt:
		var ex []ast.Node
		for _, r := range s.Results {
			ex = append(ex, r)
		}
		return g.add(&node{exprs: ex})
	case *ast.EmptyStmt:
		return next
	case *ast.BranchStmt:
		if s.Label != nil && s.Tok != token.GOTO {
			a.fail(fmt.Errorf("labeled %s", s.Tok))
			return next
		}
		switch s.Tok {
		case token.GOTO:
			return g.add(&node{succ: []int{g.label(s.Label.Name)}})
		case token.BREAK:
			if j.brk < 0 {
				a.fail(fmt.Errorf("break outside switch"))
			}
			return g.add(&node{succ: []int{j.brk}})
		case token.FALLTHROUGH:
			if j.ft < 0 {
				a.fail(fmt.Errorf("misplaced fallthrough"))
			}
			return g.add(&node{succ: []int{j.ft}})
		}
		a.fail(fmt.Errorf("%s", s.Tok))
		return next
	case *ast.BlockStmt:
		return g.list(s.List, next, j)
	case *ast.LabeledStmt:
		l := g.label(s.Label.Name)
		g.nodes[l].succ = []int{g.stmt(s.Stmt, next, j)}
		return l
	case *ast.IfStmt:
		els := next
		if s.Else != nil {
			els = g.stmt(s.Else, next, j)
		}
		body := g.list(s.Body.List, next, j)
		entry := g.add(&node{exprs: []ast.Node{s.Cond}, succ: []int{body, els}})
		if s.Init != nil {
			entry = g.stmt(s.Init, entry, j)
		}
		return entry
	case *ast.SwitchStmt:
		clauses := s.Body.List
		entries := make([]int, len(clauses))
		ft := -1
		hasDefault := false
		for i := len(clauses) - 1; i >= 0; i-- {
			cc := clauses[i].(*ast.CaseClause)
			if cc.List == nil {
				hasDefault = true
			}
			entries[i] = g.list(cc.Body, next, jumps{brk: next, ft: ft})
			ft = entries[i]
		}
		// Case expressions are evaluated only until one matches;
		// the generated code has only literals there.
		for _, c := range clauses {
			for _, e := range c.(*ast.CaseClause).List {
				if !is[*ast.BasicLit](e) {
					a.fail(fmt.Errorf("non-literal case"))
				}
			}
		}
		tag := &node{succ: entries}
		if s.Tag != nil {
			tag.exprs = []ast.Node{s.Tag}
		}
		if !hasDefault {
			tag.succ = append(tag.succ, next)
		}
		entry := g.add(tag)
		if s.Init != nil {
			entry = g.stmt(s.Init, entry, j)
		}
		return entry
	}
	a.fail(fmt.Errorf("unsupported statement %T", s))
	return next
}

func (g *cfg) label(name string) int {
	if l, ok := g.labels[name]; ok {
		return l
	}
	l := g.add(&node{})
	g.labels[name] = l
	return l
}

// transfer applies node n to the facts in; in the marking pass, it counts
// the checks those facts cover.
func (a *analyzer) transfer(n *node, in facts) facts {
	var acc []access
	a.cur = in
	for _, e := range n.exprs {
		a.accesses(e, false, &acc)
	}
	a.cur = nil
	// Helper calls run in lexical left-to-right order, arguments before the
	// call (Go spec, Order of evaluation), which is the order accesses lists
	// them in; so a call may rely on the unconditional calls before it in the
	// same statement. Byte accesses have no specified order relative to
	// calls, so they only contribute facts for later statements.
	out := in.copy()
	for _, x := range acc {
		if x.call == nil {
			continue
		}
		if f, ok := out[x.key]; ok && f.end >= x.end && a.mark {
			a.hits[x.call]++
		}
		if !x.cond {
			if f, ok := out[x.key]; !ok || f.end < x.end {
				out[x.key] = fact{end: x.end, vars: x.vars}
			}
		}
	}
	for _, x := range acc {
		if x.cond || x.call != nil {
			continue
		}
		if f, ok := out[x.key]; !ok || f.end < x.end {
			out[x.key] = fact{end: x.end, vars: x.vars}
		}
	}
	out = out.kill(n.kill)
	if n.copyDst != "" {
		src := n.copySrc
		if r, ok := out["="+src]; ok {
			src = r.rep // a copy of a copy: point at the original
		}
		out["="+n.copyDst] = fact{end: -1, vars: []string{n.copyDst, src}, rep: src}
	}
	return out
}

// meet intersects two fact sets (nil is the universal set, "not yet reached").
func meet(a, b facts) (facts, bool) {
	if b == nil {
		return a, false
	}
	if a == nil {
		return b.copy(), true
	}
	changed := false
	for k, fa := range a {
		fb, ok := b[k]
		if !ok || fb.rep != fa.rep {
			delete(a, k)
			changed = true
		} else if fb.end < fa.end {
			a[k] = fact{end: fb.end, vars: fa.vars}
			changed = true
		}
	}
	return a, changed
}

// analyze computes, for every statement, the facts that hold on all paths
// into it (a must-analysis iterated to a fixed point, so loops formed by
// backward gotos are handled), then counts the checks those facts cover.
func (a *analyzer) analyze(body *ast.BlockStmt) {
	g := &cfg{a: a, labels: map[string]int{}}
	exit := g.add(&node{})
	entry := g.list(body.List, exit, jumps{brk: -1, ft: -1})
	if a.err != nil {
		return
	}
	for name, l := range g.labels {
		if g.nodes[l].succ == nil {
			a.fail(fmt.Errorf("goto to unknown label %s", name))
			return
		}
	}
	in := make([]facts, len(g.nodes))
	in[entry] = facts{}
	work := []int{entry}
	queued := make([]bool, len(g.nodes))
	queued[entry] = true
	for len(work) > 0 {
		n := work[len(work)-1]
		work = work[:len(work)-1]
		queued[n] = false
		out := a.transfer(g.nodes[n], in[n])
		if a.err != nil {
			return
		}
		for _, s := range g.nodes[n].succ {
			var changed bool
			in[s], changed = meet(in[s], out)
			if changed && !queued[s] {
				queued[s] = true
				work = append(work, s)
			}
		}
	}
	a.mark = true
	for i, n := range g.nodes {
		if in[i] != nil { // unreached nodes decide nothing
			a.transfer(n, in[i])
		}
	}
}
