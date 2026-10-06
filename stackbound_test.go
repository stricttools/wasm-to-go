//go:build !generator

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"runtime"
	"runtime/debug"
	"testing"

	packages_stack_bound "github.com/stricttools/wasm-to-go/testdata/packages/stack_bound"
	stack_bound_test "github.com/stricttools/wasm-to-go/testdata/regression/stack_bound"
)

type stackBoundModule interface {
	Xdown(n int32) int32
	Xdepth(n int32) int32
	Xwalk(n int32) int32
	Xpingpong(n int32) int32
	Xpure(n int32) int32
}

// The modules of the stack bound test, by name.
var stackBoundModules = map[string]func() stackBoundModule{
	"one package":      func() stackBoundModule { return stack_bound_test.New() },
	"several packages": func() stackBoundModule { return packages_stack_bound.New() },
}

// goMaxStack32 is the smallest maximum Go stack of any platform (a
// goroutine's, on 32-bit platforms): the bound must trap before it.
const goMaxStack32 = 250_000_000

// Unbounded recursion traps with a recoverable panic before the Go stack
// grows past the bound; bounded recursion gives back what it took, so it
// runs any number of times. The test lowers the maximum Go stack to the
// smallest any platform has: a call that outgrew it, without the bound or
// with too large a bound, would make the Go runtime end the test binary.
func Test_regression_stack_bound(t *testing.T) {
	defer debug.SetMaxStack(debug.SetMaxStack(goMaxStack32))
	for name, newModule := range stackBoundModules {
		m := newModule()
		for i := 0; i < 1000; i++ {
			if got := m.Xdepth(2000); got != 2000 {
				t.Fatalf("%s: depth(2000) = %d, want 2000", name, got)
			}
			if got := m.Xwalk(2000); got != 2001 {
				t.Fatalf("%s: walk(2000) = %d, want 2001", name, got)
			}
		}
		exports := map[string]func(int32) int32{"down": m.Xdown, "pingpong": m.Xpingpong, "pure": m.Xpure}
		for _, export := range []string{"down", "pingpong", "pure", "down", "pingpong", "pure"} {
			// Each call runs on a goroutine of its own, whose stack
			// starts small.
			done := make(chan struct{})
			var trap any
			go func() {
				defer close(done)
				defer func() { trap = recover() }()
				exports[export](0)
			}()
			<-done
			if trap != stackTrap {
				t.Errorf("%s: %s(0) ended with %v, want the trap %q", name, export, trap, stackTrap)
			}
			// The module stays usable after the trap, as a Wasm instance
			// does: the trap gave back what the unwound frames took.
			if got := m.Xdepth(2000); got != 2000 {
				t.Errorf("%s: after %s's trap, depth(2000) = %d, want 2000", name, export, got)
			}
			// Give the grown stack back before the next call.
			runtime.GC()
		}
	}
}

// The variables of a local's webs past its first (splitLocals) leave the
// frame estimate, and so the depth at which recursion traps, as the
// module's locals set it.
func Test_frameEstimate_webs(t *testing.T) {
	parse := func(src string) *ast.FuncDecl {
		f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n"+src, 0)
		if err != nil {
			t.Fatal(err)
		}
		return f.Decls[0].(*ast.FuncDecl)
	}
	whole := parse(`func f(v0 int32) int32 {
		var v1 int32
		v1 = v0 + 1
		v1 = v1 * 2
		return v1
	}`)
	split := parse(`func f(v0 int32) int32 {
		var v1 int32
		var v1_1 int32
		v1 = v0 + 1
		v1_1 = v1 * 2
		return v1_1
	}`)
	if a, b := frameEstimate(whole), frameEstimate(split); a != b {
		t.Errorf("frameEstimate = %d with the local split, %d without", b, a)
	}
	for name, want := range map[string]bool{"v1_1": true, "v12_30": true, "v1": false, "v_1": false, "v1_": false, "t1_1": false, "v1_x": false} {
		if got := isWebVar(name); got != want {
			t.Errorf("isWebVar(%q) = %v, want %v", name, got, want)
		}
	}
}
