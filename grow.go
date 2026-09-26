package main

import (
	"go/ast"
	"path"
	"strconv"

	"github.com/stricttools/wasm-to-go/internal/passes"
)

// Which functions can reach memory.grow, the only operation that replaces
// the linear memory's slice header (its length, and possibly its backing
// array).
//
// A function can grow memory if it executes memory.grow (or memory.size on
// an imported memory, which calls the host's Grow), if it is imported (the
// host is opaque), or if it calls a function that can: directly, or through
// a table. A call through a closed table can reach only the functions of the
// called signature the table holds; a call through any other table can reach
// anything. Provided functions are analyzed from their source; anything the
// analysis does not recognize can grow memory.

// Calls that can grow memory: functions it calls, and whether it can grow
// memory on its own.
type callSummary struct {
	always  bool
	callees []int
}

// Predeclared Go identifiers generated code calls: builtins and conversions.
var predeclared = set[string]{
	"append": {}, "cap": {}, "clear": {}, "copy": {}, "len": {}, "make": {},
	"max": {}, "min": {}, "new": {}, "panic": {},
	"any": {}, "bool": {}, "byte": {}, "float32": {}, "float64": {},
	"int": {}, "int8": {}, "int16": {}, "int32": {}, "int64": {},
	"uint": {}, "uint8": {}, "uint16": {}, "uint32": {}, "uint64": {}, "uintptr": {},
}

// Helpers that can replace the memory's slice header.
var growHelpers = set[string]{"memory_grow": {}, "atomic_memory_grow": {}}

func (k *moduleFacts) computeGrows() {
	t := k.t
	k.byName = map[string]int{}
	for i, fn := range t.functions {
		k.byName[fn.decl.Name.Name] = i
	}

	k.grows = make([]bool, len(t.functions))
	sums := make([]callSummary, len(t.functions))
	for i := range t.functions {
		fn := &t.functions[i]
		switch {
		case fn.host:
			sums[i].always = true
		case fn.provided:
			sums[i] = k.summarizeProvided(fn.decl.Name.Name)
		case fn.decl.Body != nil:
			ast.Inspect(fn.decl.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					k.summarizeCall(call, &sums[i])
				}
				return true
			})
		}
		k.grows[i] = sums[i].always
	}
	for changed := true; changed; {
		changed = false
		for i, sum := range sums {
			if k.grows[i] {
				continue
			}
			for _, c := range sum.callees {
				if k.grows[c] {
					k.grows[i] = true
					changed = true
					break
				}
			}
		}
	}
}

// Adds a call in translated code to a summary.
func (k *moduleFacts) summarizeCall(call *ast.CallExpr, sum *callSummary) {
	if fn, ok := k.callee[call.Fun]; ok {
		sum.callees = append(sum.callees, fn)
		return
	}
	if ind, ok := k.t.indirect[call]; ok {
		targets, ok := k.indirectTargets(ind)
		if !ok {
			sum.always = true
		}
		for fn := range targets {
			sum.callees = append(sum.callees, fn)
		}
		return
	}
	if !k.leafCall(call.Fun, nil) {
		sum.always = true
	}
}

// Reports whether fun is a call that cannot reach module code, nor change
// the memory's slice header: a builtin, a conversion, a helper other than
// the grow helpers, or a function of a standard library package generated
// code imports (or of a package in imports).
func (k *moduleFacts) leafCall(fun ast.Expr, imports set[string]) bool {
	switch f := fun.(type) {
	case *ast.Ident:
		if growHelpers.has(f.Name) {
			return false
		}
		return predeclared.has(f.Name) || k.t.helperNames.has(f.Name)
	case *ast.SelectorExpr:
		x := f.X
		if sel, ok := x.(*ast.SelectorExpr); ok {
			x = sel.X // binary.LittleEndian.Uint32
		}
		if id, ok := x.(*ast.Ident); ok {
			_, std := stdlib[id.Name]
			return std || imports.has(id.Name)
		}
	}
	return false
}

// callGrows reports whether a call in translated code can grow memory.
func (k *moduleFacts) callGrows(call *ast.CallExpr) bool {
	var sum callSummary
	k.summarizeCall(call, &sum)
	if sum.always {
		return true
	}
	for _, fn := range sum.callees {
		if k.grows[fn] {
			return true
		}
	}
	return false
}

// Summarizes a provided function from its source, conservatively:
// besides calls, any use of the receiver other than calling a module
// function, reading or writing a global, or reading the memory (never
// replacing it, nor taking its address) makes it able to grow memory.
func (k *moduleFacts) summarizeProvided(name string) (sum callSummary) {
	t := k.t
	decl := t.providedDecls[name]
	if decl == nil || decl.Body == nil || len(decl.Recv.List[0].Names) != 1 {
		sum.always = true
		return
	}
	recv := decl.Recv.List[0].Names[0].Name
	imports := t.providedImports[name]

	fields := set[string]{"maxMem": {}}
	for _, g := range t.globals {
		fields.add(g.id.Name)
	}
	memory := ""
	if t.memory != nil {
		memory = t.memory.id.Name
	}

	walkParents(decl.Body, func(n ast.Node, parents []ast.Node) {
		parent := func(i int) ast.Node {
			if i < len(parents) {
				return parents[len(parents)-1-i]
			}
			return nil
		}
		switch n := n.(type) {
		case *ast.CallExpr:
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && isIdent(sel.X, recv) {
				if fn, ok := k.byName[sel.Sel.Name]; ok {
					sum.callees = append(sum.callees, fn)
					return
				}
			}
			if !k.leafCall(n.Fun, imports) {
				sum.always = true
			}

		case *ast.Ident:
			if n.Name != recv {
				return
			}
			sel, ok := parent(0).(*ast.SelectorExpr)
			if !ok || sel.X != n {
				sum.always = true // the receiver escapes
				return
			}
			switch name := sel.Sel.Name; {
			case fields.has(name):
			case name == memory && memory != "":
				// The memory expression: *m.memory (imported) or m.memory.
				var expr ast.Node = sel
				up := 1
				if t.memory.imported {
					if _, ok := parent(1).(*ast.StarExpr); !ok {
						sum.always = true
						return
					}
					expr = parent(1)
					up = 2
				}
				if !passes.ReadOnly(expr, parent(up)) {
					sum.always = true
				}
			default:
				if _, ok := k.byName[name]; ok {
					if call, ok := parent(1).(*ast.CallExpr); ok && call.Fun == sel {
						return // a call, summarized above
					}
				}
				sum.always = true // a method value, a table, the host
			}
		}
	})
	return sum
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// Walks a tree, calling fn with every node and its ancestors.
func walkParents(root ast.Node, fn func(n ast.Node, parents []ast.Node)) {
	var stack []ast.Node
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		fn(n, stack)
		stack = append(stack, n)
		return true
	})
}

// The names a file's imports bind.
func importNames(f *ast.File) set[string] {
	names := set[string]{}
	for _, imp := range f.Imports {
		if imp.Name != nil {
			names.add(imp.Name.Name)
			continue
		}
		p, err := strconv.Unquote(imp.Path.Value)
		if err == nil {
			names.add(path.Base(p))
		}
	}
	return names
}
