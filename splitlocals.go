package main

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"github.com/stricttools/wasm-to-go/internal/locals"
)

// splitLocals gives each web of the function's locals a variable of its own
// (internal/locals): a local's web that holds its value at entry (its
// parameter, or the zero its declaration gives it) keeps the local's
// variable, vN, as does the first web of a local without one; each other
// web gets a variable vN_K, declared by the statements it returns. code is
// the function's body after its local declarations, and types the value
// type of each local, parameters included.
//
// Why: a compiler reuses a local for unrelated values as it reuses a
// register. Split, each variable is assigned only the values one group of
// uses reads, which is what a pass reasoning from a variable's assignments
// needs: one unrelated assignment no longer spoils what it can prove of
// the variable.
//
// # Why this preserves behavior
//
// Every value a local.get can read is the value of a definition in its web
// (a local.set or local.tee, or the local's value at entry), and every
// definition of the web writes the web's variable, so the variable holds,
// at each local.get, the value the local holds: no definition of another
// web reaches it, and a web without the entry value has a definition on
// every path to each of its reads. The variables are declared, zero, at the
// function's start, as the locals are.
func (fn *funcCompiler) splitLocals(code []byte, types []byte) ([]ast.Stmt, error) {
	split, err := locals.Analyze(code, len(types))
	if err != nil {
		return nil, err
	}
	names := make([]*ast.Ident, len(split.Local))
	taken := make([]bool, len(types))
	for w, l := range split.Local {
		if split.Entry[w] {
			names[w] = localVar(l)
			taken[l] = true
		}
	}
	more := make([]int, len(types))
	var decls []ast.Stmt
	for w, l := range split.Local {
		if names[w] != nil {
			continue
		}
		if !taken[l] {
			names[w] = localVar(l)
			taken[l] = true
			continue
		}
		more[l]++
		names[w] = newID("v" + strconv.Itoa(l) + "_" + strconv.Itoa(more[l]))
		decls = append(decls, &ast.DeclStmt{Decl: &ast.GenDecl{
			Tok: token.VAR,
			Specs: []ast.Spec{&ast.ValueSpec{
				Names: []*ast.Ident{names[w]},
				Type:  wasmType(types[l]).ident()}}}})
	}
	fn.localRefs = make([]*ast.Ident, len(split.Web))
	for i, w := range split.Web {
		fn.localRefs[i] = names[w]
	}
	fn.localRef = 0
	return decls, nil
}

// The variable of the next local.get, local.set, or local.tee of the
// function, local i: its web's (splitLocals), or the local's own when the
// locals are not split (-noopt).
func (fn *funcCompiler) local(i uint64) *ast.Ident {
	if fn.localRefs == nil {
		return localVar(i)
	}
	id := fn.localRefs[fn.localRef]
	fn.localRef++
	return id
}

// Reports whether name is the variable of a web of a local past its first,
// vN_K, which splitLocals declares.
func isWebVar(name string) bool {
	rest, ok := strings.CutPrefix(name, "v")
	if !ok {
		return false
	}
	n, k, ok := strings.Cut(rest, "_")
	return ok && isDigits(n) && isDigits(k)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
