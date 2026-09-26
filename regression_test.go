//go:build !generator

package main

import (
	_ "embed"
	"os"
	"strings"
	"testing"

	dispatch_test "github.com/stricttools/wasm-to-go/testdata/regression/dispatch"
	memgrow_test "github.com/stricttools/wasm-to-go/testdata/regression/memgrow"
	oob_trap_test "github.com/stricttools/wasm-to-go/testdata/regression/oob_trap"
	provided_helper_test "github.com/stricttools/wasm-to-go/testdata/regression/provided_helper"
	select_test "github.com/stricttools/wasm-to-go/testdata/regression/select_effect"
	store_grow_test "github.com/stricttools/wasm-to-go/testdata/regression/store_grow"
)

func Test_regression_select_effect(t *testing.T) {
	m := select_test.New()

	if got := m.Xtest(0); got != 5 {
		t.Errorf("test(0) = %d, want 5", got)
	}
	if got := m.Xcounter(); got != 1 {
		t.Errorf("counter = %d after test(0), want 1 (operand must run unconditionally)", got)
	}

	if got := m.Xtest(1); got != 100 {
		t.Errorf("test(1) = %d, want 100", got)
	}
	if got := m.Xcounter(); got != 2 {
		t.Errorf("counter = %d after test(1), want 2", got)
	}
}

func Test_regression_store_grow(t *testing.T) {
	m := store_grow_test.New()

	if got := m.Xtest(); got != 12345 {
		t.Errorf("test() = %d, want 12345 (store must use memory slice after memory.grow)", got)
	}
	if got := m.Xsize(); got != 2 {
		t.Errorf("size = %d, want 2", got)
	}
}

func Test_regression_oob_trap(t *testing.T) {
	m := oob_trap_test.New()

	mustTrap := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected out-of-bounds trap", name)
			}
		}()
		f()
	}

	// In-bounds at the very edge of the 64 KiB memory.
	m.Xst32(65532, -1)
	if got := m.Xld32(65532); got != -1 {
		t.Errorf("ld32(65532) = %d, want -1", got)
	}
	if got := m.Xld16(65534); got != 0xffff {
		t.Errorf("ld16(65534) = %d, want 65535", got)
	}

	// First byte past the end, and accesses straddling the end.
	mustTrap("ld32 first past end", func() { m.Xld32(65536) })
	mustTrap("ld32 straddling end", func() { m.Xld32(65533) })
	mustTrap("ld16 straddling end", func() { m.Xld16(65535) })
	mustTrap("ld64 straddling end", func() { m.Xld64(65529) })
	mustTrap("st32 straddling end", func() { m.Xst32(65533, 0) })
	mustTrap("st64 first past end", func() { m.Xst64(65536, 0) })

	// Huge static offset: effective address up to 2^33-2 must trap, not wrap.
	mustTrap("static offset 0xffffffff", func() { m.Xld32o(0) })
	mustTrap("static offset at max address", func() { m.Xld32o(-1) })

	// After growth, formerly out-of-bounds addresses become valid,
	// and the trap boundary moves to the new end.
	if got := m.Xgrow(1); got != 1 {
		t.Fatalf("grow(1) = %d, want 1", got)
	}
	m.Xst32(65536, 42)
	if got := m.Xld32(65536); got != 42 {
		t.Errorf("ld32(65536) = %d after grow, want 42", got)
	}
	mustTrap("ld32 past grown end", func() { m.Xld32(131073) })
}

func Test_regression_provided_helper(t *testing.T) {
	m := provided_helper_test.New()

	if got := m.Xtest(); got != 0x0807060504030201 {
		t.Errorf("test() = %#x, want 0x0807060504030201 (provided import must be able to call load64)", got)
	}
}

// Indirect calls through a closed table become direct calls (dispatch);
// every slot the dispatch does not list still panics as before, and calls
// through tables that are exported or mutated are left alone.
func Test_regression_dispatch(t *testing.T) {
	src, err := os.ReadFile("testdata/regression/dispatch/dispatch.go")
	if err != nil {
		t.Fatal(err)
	}
	if !*noopt {
		if got := strings.Count(string(src), "switch "); got != 2 {
			t.Errorf("found %d dispatch switches, want 2 (the calls through the closed table)", got)
		}
	}

	m := dispatch_test.New()

	mustPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected a panic", name)
			}
		}()
		f()
	}

	for slot, want := range map[int32]int32{1: 10, 2: 15, 4: 10} {
		if got := m.Xcall(slot, 5); got != want {
			t.Errorf("call(%d, 5) = %d, want %d", slot, got, want)
		}
	}
	if got := m.Xcall0(3); got != 42 {
		t.Errorf("call0(3) = %d, want 42", got)
	}
	mustPanic("null slot", func() { m.Xcall(0, 5) })
	mustPanic("null slot after the segment", func() { m.Xcall(5, 5) })
	mustPanic("slot of another type", func() { m.Xcall(3, 5) })
	mustPanic("slot of another type, other signature", func() { m.Xcall0(1) })
	mustPanic("slot past the end", func() { m.Xcall(8, 5) })
	mustPanic("negative slot", func() { m.Xcall(-1, 5) })

	// An exported table can be changed by the host.
	if got := m.XcallExported(1, 5); got != 10 {
		t.Errorf("callExported(1, 5) = %d, want 10", got)
	}
	(*m.Xexported())[1] = func(v int32) int32 { return v + 100 }
	if got := m.XcallExported(1, 5); got != 105 {
		t.Errorf("callExported(1, 5) = %d after the host replaced slot 1, want 105", got)
	}

	// A table mutated by table.set.
	if got := m.XcallMutated(1, 5); got != 10 {
		t.Errorf("callMutated(1, 5) = %d, want 10", got)
	}
	m.XsetMutated(1)
	if got := m.XcallMutated(1, 5); got != 15 {
		t.Errorf("callMutated(1, 5) = %d after table.set, want 15", got)
	}
}

// A function that caches the memory in a local (memlocal) reloads it after
// every call that can grow memory, however the call reaches memory.grow;
// each export stores to and loads from the page its call added, which
// panics if the cached memory is stale.
func Test_regression_memgrow(t *testing.T) {
	src, err := os.ReadFile("testdata/regression/memgrow/memgrow.go")
	if err != nil {
		t.Fatal(err)
	}
	if !*noopt {
		_, peek, _ := strings.Cut(string(src), "func (m *Module) Xpeek(")
		peek, _, _ = strings.Cut(peek, "\n}\n")
		if !strings.Contains(peek, "mem := ") || strings.Contains(peek, "mem = ") {
			t.Errorf("peek should cache the memory, and never reload it:\n%s", peek)
		}
	}

	env := &memgrowEnv{}
	m := memgrow_test.New(env)
	env.m = m

	tests := []struct {
		name string
		call func() int32
	}{
		{"direct", m.Xdirect},
		{"closed table, slot 0", func() int32 { return m.Xclosed(0) }},
		{"closed table, slot 1 (no growth)", func() int32 { return m.Xclosed(1) }},
		{"closed table, slot 2", func() int32 { return m.Xclosed(2) }},
		{"exported table, changed by the host", func() int32 {
			(*m.Xtable())[0] = func() int32 { return int32(m.Xmemory().Grow(1, 65536)) }
			return m.Xopen()
		}},
		{"host", m.Xhost},
		{"provided", m.Xprovided},
	}
	for _, tt := range tests {
		if got := tt.call(); got != 99 {
			t.Errorf("%s = %d, want 99", tt.name, got)
		}
	}
	if got := m.Xpeek(0); got != 2 {
		t.Errorf("peek(0) = %d, want 2", got)
	}
}

type memgrowEnv struct{ m *memgrow_test.Module }

func (e *memgrowEnv) Xgrow() int32 { return int32(e.m.Xmemory().Grow(1, 65536)) }
