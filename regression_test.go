//go:build !generator

package main

import (
	_ "embed"
	"os"
	"strings"
	"testing"

	bce_test "github.com/stricttools/wasm-to-go/testdata/regression/bce"
	dispatch_test "github.com/stricttools/wasm-to-go/testdata/regression/dispatch"
	split_locals_test "github.com/stricttools/wasm-to-go/testdata/regression/split_locals"
	memgrow_test "github.com/stricttools/wasm-to-go/testdata/regression/memgrow"
	oob_trap_test "github.com/stricttools/wasm-to-go/testdata/regression/oob_trap"
	oob_trap_imported_test "github.com/stricttools/wasm-to-go/testdata/regression/oob_trap_imported"
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
	testOOBTrap(t, oob_trap_test.New())
}

// The memory placed in a backing array far larger than itself: an access
// past the memory's end traps though the array holds bytes there, before
// and after the memory grows in place.
func Test_regression_oob_trap_use_memory(t *testing.T) {
	m := oob_trap_test.New()
	if !m.UseMemory(make([]byte, 0, 16<<16)) {
		t.Fatal("UseMemory refused an array of 16 pages")
	}
	testOOBTrap(t, m)
}

// An imported memory whose slice, as the host holds it, has spare
// capacity past its length.
func Test_regression_oob_trap_imported(t *testing.T) {
	testOOBTrap(t, oob_trap_imported_test.New(spareEnv{&spareMemory{make([]byte, 1<<16, 4<<16)}}))
}

type oobTrapModule interface {
	Xld16(addr int32) int32
	Xld32(addr int32) int32
	Xld64(addr int32) int64
	Xld32o(addr int32) int32
	Xst32(addr, v int32)
	Xst64(addr int32, v int64)
	Xgrow(pages int32) int32
}

func testOOBTrap(t *testing.T, m oobTrapModule) {

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

	// A second page: an owned memory's backing array then has room for a
	// fourth, and so does the host's slice of an imported memory here.
	if got := m.Xgrow(1); got != 2 {
		t.Fatalf("grow(1) = %d, want 2", got)
	}
	const end = 3 << 16
	m.Xst32(end-4, 7)
	if got := m.Xld32(end - 4); got != 7 {
		t.Errorf("ld32(%d) = %d after the second grow, want 7", end-4, got)
	}
	mustTrap("ld16 straddling the grown end", func() { m.Xld16(end - 1) })
	mustTrap("ld32 straddling the grown end", func() { m.Xld32(end - 2) })
	mustTrap("ld64 first past the grown end", func() { m.Xld64(end) })
	mustTrap("st32 straddling the grown end", func() { m.Xst32(end-3, 0) })
	mustTrap("st64 straddling the grown end", func() { m.Xst64(end-7, 0) })
	if got := m.Xld32(end - 8); got != 0 {
		t.Errorf("ld32(%d) = %d: a store that trapped wrote to the memory", end-8, got)
	}
}

func Test_regression_provided_helper(t *testing.T) {
	testProvidedHelper(t, provided_helper_test.New())
}

func testProvidedHelper(t *testing.T, m interface{ Xtest() int64 }) {
	if got := m.Xtest(); got != 0x0807060504030201 {
		t.Errorf("test() = %#x, want 0x0807060504030201 (provided import must be able to call load64)", got)
	}
}

// Indirect calls through a closed table become direct calls (dispatch);
// every slot the dispatch does not list still panics as before, and calls
// through tables that are exported or mutated, or that can reach more
// functions than maxDispatchTargets, are left alone.
func Test_regression_dispatch(t *testing.T) {
	src, err := os.ReadFile("testdata/regression/dispatch/dispatch.go")
	if err != nil {
		t.Fatal(err)
	}
	if !*noopt {
		if got := strings.Count(string(src), "switch "); got != 2 {
			t.Errorf("found %d dispatch switches, want 2 (the calls through the closed table of few functions)", got)
		}
	}

	testDispatch(t, dispatch_test.New())
}

type dispatchModule interface {
	Xcall(slot, v int32) int32
	Xcall0(slot int32) int32
	XcallExported(slot, v int32) int32
	Xexported() *[]any
	XcallMutated(slot, v int32) int32
	XsetMutated(slot int32)
	XcallMany(slot, v int32) int64
}

func testDispatch(t *testing.T, m dispatchModule) {
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

	// A closed table of more functions than the pass dispatches.
	for slot := int32(0); slot < 17; slot++ {
		if got, want := m.XcallMany(slot, 5), int64(5+100*slot); got != want {
			t.Errorf("callMany(%d, 5) = %d, want %d", slot, got, want)
		}
	}
	mustPanic("null slot of the table of many", func() { m.XcallMany(17, 5) })
	mustPanic("slot past the end of the table of many", func() { m.XcallMany(18, 5) })

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
	testMemgrow(t, m)
}

type memgrowModule interface {
	Xdirect() int32
	Xclosed(slot int32) int32
	Xtable() *[]any
	Xopen() int32
	Xhost() int32
	Xprovided() int32
	Xpeek(addr int32) int32
	Xmemory() interface {
		Slice() *[]byte
		Grow(delta, max int64) int64
	}
}

func testMemgrow(t *testing.T, m memgrowModule) {
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

// The host of the memgrow module: its import grows memory.
type memgrowEnv struct {
	m interface {
		Xmemory() interface {
			Slice() *[]byte
			Grow(delta, max int64) int64
		}
	}
}

func (e *memgrowEnv) Xgrow() int32 { return int32(e.m.Xmemory().Grow(1, 65536)) }

// With -unsafe, bounds checks an earlier check covers are removed
// (the sample is always translated with -unsafe); those nothing covers
// still panic.
func Test_regression_bce(t *testing.T) {
	generic, err := os.ReadFile("testdata/regression/bce/bce_generic.go")
	if err != nil {
		t.Fatal(err)
	}
	expanded, err := os.ReadFile("testdata/regression/bce/bce.go")
	if err != nil {
		t.Fatal(err)
	}
	if !*noopt {
		// The generic file accesses memory through the helpers' portable
		// bodies (passes.Lower), which check every access.
		if got := strings.Count(string(generic), "u(mem, "); got != 0 {
			t.Errorf("found %d unchecked helper calls in the generic file, want 0", got)
		}
		// The helper definitions have uintptr(addr); expansions, the address.
		if got := strings.Count(string(expanded), "unsafe.SliceData(mem)), uintptr(uint"); got != 4 {
			t.Errorf("found %d expanded unchecked accesses, want 4", got)
		}
	}

	testBCE(t, bce_test.New())
}

type bceModule interface {
	Xsum(addr int32) int32
	Xinc(addr int32)
	Xwiden(addr int32) int64
	Xwalk(addr, n int32) int32
	Xbranch(addr, c int32) int32
	Xcopy(addr int32) int32
	Xmemory() interface {
		Slice() *[]byte
		Grow(delta, max int64) int64
	}
}

func testBCE(t *testing.T, m bceModule) {
	mem := *m.Xmemory().Slice()

	mustPanic := func(name string, f func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected an out-of-bounds panic", name)
			}
		}()
		f()
	}

	for i := range 12 {
		mem[65524+i] = byte(i + 1)
	}
	want := int32(0x04030201 + 0x08070605 + 0x0c0b0a09)
	if got := m.Xsum(65524); got != want {
		t.Errorf("sum(65524) = %#x, want %#x", got, want)
	}
	mustPanic("sum(65525)", func() { m.Xsum(65525) })

	m.Xinc(65532)
	if got := m.Xsum(65524); got != want+1 {
		t.Errorf("sum(65524) = %#x after inc(65532), want %#x", got, want+1)
	}
	mustPanic("inc(65533)", func() { m.Xinc(65533) })

	mustPanic("widen(65534)", func() { m.Xwiden(65534) })
	mustPanic("walk(65528, 3)", func() { m.Xwalk(65528, 3) })
	if got := m.Xwalk(65528, 2); got != 0x0c0b0a0a+0x08070605 {
		t.Errorf("walk(65528, 2) = %#x", got)
	}
	mustPanic("branch(65532, 1)", func() { m.Xbranch(65532, 1) })
	mustPanic("branch(65532, 0)", func() { m.Xbranch(65532, 0) })
	if got := m.Xcopy(65528); got != 0x08070605 {
		t.Errorf("copy(65528) = %#x", got)
	}
	mustPanic("copy(65529)", func() { m.Xcopy(65529) })
}

// A local reused for unrelated values gets a variable per web
// (splitLocals), and the functions compute what they did.
func Test_regression_split_locals(t *testing.T) {
	src, err := os.ReadFile("testdata/regression/split_locals/split_locals.go")
	if err != nil {
		t.Fatal(err)
	}
	if !*noopt {
		for _, tt := range []struct{ fn, decls string }{
			{"XtwoSums", "var v2_1 int32"},
			{"XtwoSums", "var v0_1 int32"},
			{"Xloop", "var v2_1 int32"},
			{"Xbranch", "var v2_1 float64"},
		} {
			if body := funcBody(src, tt.fn); !strings.Contains(body, tt.decls) {
				t.Errorf("%s does not declare %s:\n%s", tt.fn, tt.decls, body)
			}
		}
		if body := funcBody(src, "Xloop"); strings.Contains(body, "v1_1") || strings.Contains(body, "v0_1") {
			t.Errorf("loop splits a local whose values meet at the loop:\n%s", body)
		}
	}
	m := split_locals_test.New()
	for _, tt := range []struct{ a, b, want int32 }{{1, 2, 26}, {-5, 7, 33}, {0, 0, 13}} {
		if got := m.XtwoSums(tt.a, tt.b); got != tt.want {
			t.Errorf("twoSums(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
	for _, tt := range []struct{ n, want int32 }{{3, 1014}, {1, 1001}, {0, 1000}, {10, 1385}} {
		if got := m.Xloop(tt.n); got != tt.want {
			t.Errorf("loop(%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
	if got := m.Xbranch(1, 2.5); got != 6 {
		t.Errorf("branch(1, 2.5) = %v, want 6", got)
	}
	if got := m.Xbranch(0, 2.5); got != 2.5 {
		t.Errorf("branch(0, 2.5) = %v, want 2.5", got)
	}
}

// The body of the translated function name in src.
func funcBody(src []byte, name string) string {
	_, body, _ := strings.Cut(string(src), "func (m *Module) "+name+"(")
	if body == "" {
		_, body, _ = strings.Cut(string(src), "func "+name+"(")
	}
	body, _, _ = strings.Cut(body, "\n}\n")
	return body
}
