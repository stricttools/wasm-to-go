package main

import (
	"cmp"
	"go/ast"
	"slices"
	"strings"

	"github.com/stricttools/wasm-to-go/internal/passes"
)

// Facts about the whole module, for the passes that need them
// (see funcCompiler.optimizeModule).
type moduleFacts struct {
	t *translator
	// For every closed table (by index), its contents after New:
	// slot → function index; missing slots are null.
	closed map[int]map[uint64]int
	// Function index of each function's call expression.
	callee map[ast.Expr]int
	// Dispatch sites, by table and called Go signature.
	sites map[dispatchKey]*passes.DispatchSite
	// Function index by name.
	byName map[string]int
	// Which functions can grow memory (grow.go).
	grows []bool

	// Number of calls rewritten by the dispatch pass.
	dispatched int
	// Number of functions that cache the memory in a local.
	memLocals int
}

type dispatchKey struct {
	table int
	sig   string
}

func (t *translator) moduleFacts() *moduleFacts {
	k := &moduleFacts{
		t:      t,
		closed: map[int]map[uint64]int{},
		callee: map[ast.Expr]int{},
		sites:  map[dispatchKey]*passes.DispatchSite{},
	}
	for i, fn := range t.functions {
		k.callee[fn.call] = i
	}
	for i := range t.tables {
		if slots, ok := k.closedTable(i); ok {
			k.closed[i] = slots
		}
	}
	k.computeGrows()
	return k
}

// The closed-table rule.
//
// A table is closed when the module defines it (does not import it), does
// not export it, and no instruction mutates it (table.set, table.grow,
// table.fill, table.init, or table.copy into it): its contents are then
// what the active element segments New applies put there, and
// nothing, inside or outside the module, can change them afterwards.
//
// closedTable returns those contents, or false if the table is not closed,
// or its contents cannot be known: an element segment with a non-constant
// offset, or entries other than functions and nulls (a global.get), or a
// segment out of bounds (New traps; no Module ever exists).
func (k *moduleFacts) closedTable(i int) (map[uint64]int, bool) {
	t := k.t
	tab := t.tables[i]
	if tab.imported || tab.mutated {
		return nil, false
	}
	for _, exp := range t.exports {
		if exp.kind == externTable && exp.index == i {
			return nil, false
		}
	}

	slots := map[uint64]int{}
	for _, elem := range t.elements {
		if elem.passive || elem.declive || int(elem.index) != i {
			continue
		}
		off, ok := islit(elem.offset, "i32")
		if !ok {
			off, ok = islit(elem.offset, "i64")
		}
		if !ok || off < 0 || uint64(off)+uint64(len(elem.init)) > tab.min {
			return nil, false
		}
		for j, init := range elem.init {
			slot := uint64(off) + uint64(j)
			if fn, ok := k.callee[init]; ok {
				slots[slot] = fn
			} else if isNullRef(init) {
				delete(slots, slot)
			} else {
				return nil, false
			}
		}
	}
	return slots, true
}

// Reports whether expr is the translation of ref.null.
func isNullRef(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	fun, ok1 := call.Fun.(*ast.Ident)
	arg, ok2 := call.Args[0].(*ast.Ident)
	return ok1 && ok2 && fun.Name == "any" && arg.Name == "nil"
}

// The Go signature of a function type: Wasm types that translate to the
// same Go type (funcref and externref) are the same here too, as they are
// to a type assertion.
func goSignature(typ funcType) string {
	var buf strings.Builder
	for _, p := range []byte(typ.params) {
		buf.WriteString(wasmType(p).ident().Name)
		buf.WriteByte(',')
	}
	buf.WriteString("->")
	for _, r := range []byte(typ.results) {
		buf.WriteString(wasmType(r).ident().Name)
		buf.WriteByte(',')
	}
	return buf.String()
}

// indirectTargets returns the functions an indirect call can reach, with
// the slots holding each, if it goes through a closed table: those of the
// called Go signature.
func (k *moduleFacts) indirectTargets(ind indirectCall) (map[int][]uint64, bool) {
	slots, ok := k.closed[ind.table]
	if !ok {
		return nil, false
	}
	sig := goSignature(ind.typ)
	targets := map[int][]uint64{}
	for slot, fn := range slots {
		if goSignature(k.t.functions[fn].typ) == sig {
			targets[fn] = append(targets[fn], slot)
		}
	}
	return targets, true
}

// dispatchSite returns the direct calls an indirect call can make,
// if it goes through a closed table.
func (k *moduleFacts) dispatchSite(call *ast.CallExpr) *passes.DispatchSite {
	ind, ok := k.t.indirect[call]
	if !ok {
		return nil
	}
	key := dispatchKey{ind.table, goSignature(ind.typ)}
	if site, ok := k.sites[key]; ok {
		return site
	}
	targets, ok := k.indirectTargets(ind)
	if !ok {
		return nil
	}

	site := &passes.DispatchSite{}
	for fn, slots := range targets {
		slices.Sort(slots)
		site.Cases = append(site.Cases, passes.DispatchCase{
			Slots: slots,
			Fun:   k.t.functions[fn].call})
	}
	slices.SortFunc(site.Cases, func(a, b passes.DispatchCase) int {
		return cmp.Compare(a.Slots[0], b.Slots[0])
	})
	for _, r := range []byte(ind.typ.results) {
		site.Results = append(site.Results, wasmType(r).ident())
	}
	k.sites[key] = site
	return site
}
