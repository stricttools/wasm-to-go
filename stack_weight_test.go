//go:build !generator

package main

import (
	"os"
	"testing"

	"github.com/stricttools/wasm-to-go/internal/stackweight"
	stack_weight_test "github.com/stricttools/wasm-to-go/testdata/regression/stack_weight"
)

// The stack-weight pass's output, translated, charges what the pass
// reported at every level of each form of recursion, gives it all back on
// every way out, and traps at its bound after the number of levels the
// bound allows: the same numbers a JavaScript engine running the pass's
// output gets, since the charges are part of the module.
func Test_stack_weight(t *testing.T) {
	in, err := os.ReadFile("testdata/regression/stack_weight/stack_weight.unweighted.wasm")
	if err != nil {
		t.Fatal(err)
	}
	res, err := stackweight.Weigh(in, stackWeightOptions)
	if err != nil {
		t.Fatal(err)
	}
	// The functions by export name, in the module's order: the text file
	// declares $leaf, $sp, callsLeaf, $down, $viaTable, $pair, $walk, walk,
	// $forever, and forever.
	charge := func(index int) int32 { return int32(res.Functions[index].Charge) }
	down, viaTable, pair, forever := charge(3), charge(4), charge(5), charge(8)
	for i, f := range res.Functions {
		want := i == 3 || i == 4 || i == 5 || i == 6 || i == 8
		if f.Recursive != want || (f.Charge > 0) != want {
			t.Fatalf("function %d: recursive %t, charge %d; want recursive %t and charged only if so", i, f.Recursive, f.Charge, want)
		}
	}
	const top = 65536
	m := stack_weight_test.New()
	check := func(what string, got, want int32) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %d, want %d", what, got, want)
		}
		if sp := m.Xsp(); sp != top {
			t.Errorf("after %s the stack pointer is %d, want %d: a charge was not given back", what, sp, top)
		}
	}
	check("callsLeaf(1)", m.XcallsLeaf(1), top)
	check("down(5)", m.Xdown(5), top-6*down)
	check("viaTable(5)", m.XviaTable(5), top-6*viaTable)
	sp, n := m.Xpair(5)
	check("pair(5)'s stack pointer", sp, top-6*pair)
	check("pair(5)'s count", n, 5)
	check("walk(5)", m.Xwalk(5), top)
	check("walk(5)'s levels", *m.Xcount(), 6)

	// Recursion without end traps once a charge would take the stack
	// pointer below the stack limit.
	func() {
		defer func() {
			if p := recover(); p != "unreachable" {
				t.Fatalf("forever() ended with %v, want the trap unreachable", p)
			}
		}()
		m.Xforever()
		t.Fatal("forever() returned")
	}()
	if got, want := *m.Xcount(), int32(stackWeightOptions.StackLimit)/forever; got != want {
		t.Errorf("forever() trapped after %d levels, want %d", got, want)
	}
	if got, want := res.Lowest, int64(top)-stackWeightOptions.StackLimit; got != want {
		t.Errorf("the pass's lowest stack pointer is %d, want %d", got, want)
	}
}
