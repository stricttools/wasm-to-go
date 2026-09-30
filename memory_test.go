//go:build !generator

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stricttools/wasm-to-go/reserve"
	packages_use_memory "github.com/stricttools/wasm-to-go/testdata/packages/use_memory"
	bulk_bounds_test "github.com/stricttools/wasm-to-go/testdata/regression/bulk_bounds"
	bulk_bounds_imported_test "github.com/stricttools/wasm-to-go/testdata/regression/bulk_bounds_imported"
	use_memory_test "github.com/stricttools/wasm-to-go/testdata/regression/use_memory"
)

type bulkModule interface {
	Xfill(dest, n int32)
	Xzero(dest, n int32)
	Xcopy(dest, src, n int32)
	Xinit(dest, n int32)
}

// Every bulk operation that reaches past the memory's end traps, and the
// same operation ending at the end does not.
func testBulkBounds(t *testing.T, m bulkModule, size int32) {
	t.Helper()
	ops := []struct {
		name string
		call func(dest int32)
	}{
		{"memory.fill", func(dest int32) { m.Xfill(dest, 16) }},
		{"memory.fill with zero", func(dest int32) { m.Xzero(dest, 16) }},
		{"memory.copy", func(dest int32) { m.Xcopy(dest, 0, 16) }},
		{"memory.copy from past the end", func(dest int32) { m.Xcopy(0, dest, 16) }},
		{"memory.init", func(dest int32) { m.Xinit(dest, 16) }},
	}
	traps := func(f func()) (trapped bool) {
		defer func() { trapped = recover() != nil }()
		f()
		return false
	}
	for _, op := range ops {
		if traps(func() { op.call(size - 16) }) {
			t.Errorf("%s of the memory's last 16 bytes (size %d) trapped", op.name, size)
		}
		if !traps(func() { op.call(size - 8) }) {
			t.Errorf("%s 8 bytes past the memory's end (size %d) did not trap", op.name, size)
		}
	}
}

// The memory's slice has spare capacity after it grows (it did when
// memory_grow appended to it); the operations trap at its length.
func Test_regression_bulk_bounds(t *testing.T) {
	m := bulk_bounds_test.New()
	testBulkBounds(t, m, 1<<16)
	if got := m.Xgrow(1); got != 1 {
		t.Fatalf("grow(1) = %d, want 1", got)
	}
	if mem := *m.Xmemory().Slice(); cap(mem) != len(mem) {
		t.Errorf("the memory's slice has capacity %d past its length %d", cap(mem), len(mem))
	}
	testBulkBounds(t, m, 2<<16)
}

// An imported memory whose slice, as the host holds it, has spare capacity.
type spareMemory struct{ buf []byte }

func (s *spareMemory) Slice() *[]byte { return &s.buf }
func (s *spareMemory) Grow(delta, max int64) int64 {
	old := int64(len(s.buf) >> 16)
	if delta < 0 || old+delta > max || int((old+delta)<<16) > cap(s.buf) {
		return -1
	}
	s.buf = s.buf[:(old+delta)<<16]
	return old
}

type spareEnv struct{ mem *spareMemory }

func (e spareEnv) Xmemory() bulk_bounds_imported_test.Memory { return e.mem }

func Test_regression_bulk_bounds_imported(t *testing.T) {
	env := spareEnv{&spareMemory{make([]byte, 1<<16, 4<<16)}}
	m := bulk_bounds_imported_test.New(env)
	testBulkBounds(t, m, 1<<16)
	if got := m.Xgrow(1); got != 1 {
		t.Fatalf("grow(1) = %d, want 1", got)
	}
	testBulkBounds(t, m, 2<<16)
}

type useMemoryModule interface {
	Xgrow_to(pages int32) int32
	Xgrow(delta int32) int32
	Xsum() int32
	MemoryMax() int64
	UseMemory(buf []byte) bool
	Xmemory() interface {
		Slice() *[]byte
		Grow(delta, max int64) int64
	}
}

// The memory's first byte (the memory is never empty here).
func memoryStart(m useMemoryModule) *byte {
	return &(*m.Xmemory().Slice())[0]
}

// Sums 0 + 1 + ... + pages-1, the pages' first words.
func pageSum(pages int32) int32 { return pages * (pages - 1) / 2 }

func Test_regression_use_memory(t *testing.T) {
	testUseMemory(t, use_memory_test.New(), use_memory_test.New())
}

func Test_regression_packages_use_memory(t *testing.T) {
	testUseMemory(t, packages_use_memory.New(), packages_use_memory.New())
}

// m is placed in a backing array of 16 pages; plain keeps the Go heap's.
// Both must give the same results; m must not move until it outgrows the
// array, and keep its contents when it does.
func testUseMemory(t *testing.T, m, plain useMemoryModule) {
	if got := m.MemoryMax(); got != 2048<<16 {
		t.Errorf("MemoryMax() = %d, want %d", got, 2048<<16)
	}
	if m.UseMemory(nil) || m.UseMemory(make([]byte, 0, 1<<16-1)) {
		t.Error("UseMemory took a backing array smaller than the memory")
	}
	for _, mod := range []useMemoryModule{m, plain} {
		if got := mod.Xgrow_to(4); got != 4 {
			t.Fatalf("grow_to(4) = %d, want 4", got)
		}
	}
	buf := make([]byte, 3<<16, 16<<16) // longer than the memory: its length is ignored
	if !m.UseMemory(buf) {
		t.Fatal("UseMemory refused a backing array of 16 pages")
	}
	if memoryStart(m) != &buf[0] {
		t.Fatal("the memory is not at the start of the backing array")
	}
	if mem := *m.Xmemory().Slice(); len(mem) != 4<<16 || cap(mem) != len(mem) {
		t.Errorf("the memory's slice has length %d and capacity %d, want %d twice", len(mem), cap(mem), 4<<16)
	}
	check := func(pages int32) {
		t.Helper()
		for _, mod := range []useMemoryModule{m, plain} {
			if got := mod.Xgrow_to(pages); got != pages {
				t.Fatalf("grow_to(%d) = %d", pages, got)
			}
			if got := mod.Xsum(); got != pageSum(pages) {
				t.Fatalf("sum after grow_to(%d) = %d, want %d", pages, got, pageSum(pages))
			}
		}
	}
	check(16)
	if memoryStart(m) != &buf[0] {
		t.Error("the memory moved while growing within its backing array")
	}
	check(17)
	if memoryStart(m) == &buf[0] {
		t.Error("the memory grew past its backing array without moving")
	}
	// The results of memory.grow, through the module and the host, match.
	for _, delta := range []int32{0, 3, 2048, -1} {
		if a, b := m.Xgrow(delta), plain.Xgrow(delta); a != b {
			t.Errorf("grow(%d) = %d, want %d as without UseMemory", delta, a, b)
		}
	}
	for _, g := range []struct{ delta, max, want int64 }{{2, 21, -1}, {1, 21, 20}, {0, 0, 21}} {
		if a, b := m.Xmemory().Grow(g.delta, g.max), plain.Xmemory().Grow(g.delta, g.max); a != g.want || b != g.want {
			t.Errorf("the host's Grow(%d, %d) = %d, and %d without UseMemory, want %d", g.delta, g.max, a, b, g.want)
		}
	}
	if got := m.Xsum(); got != pageSum(17) {
		t.Errorf("sum = %d after growing without writing, want %d (new pages are zero)", got, pageSum(17))
	}
}

// Peak memory of a process whose module's memory grows to 64 MiB one page
// at a time, filling each page, above an idle process's (the same program,
// its module created but not grown). With the memory in a reservation
// (reserve.Memory), the process holds the module's memory and nothing more
// (measured: 64.1 MiB); in the Go heap it holds the memory twice, the last
// move's old and new arrays (measured: 129.6 MiB; growing by append, before
// memory_grow doubled its backing array, took 270 to 290 MiB). The bounds
// sit just above the measurements.
const (
	peakPages     = 1024
	peakEnv       = "WASM2GO_MEMORY_PEAK"
	peakMarker    = "memory peak child: "
	reservedSlack = 1 << 20 // bytes over the module's memory, reserved
	heapSlack     = 4 << 20 // bytes over twice the module's memory, in the Go heap
)

func Test_memory_peak(t *testing.T) {
	if mode := os.Getenv(peakEnv); mode != "" {
		runPeakChild(mode)
		return
	}
	if runtime.GOOS != "linux" {
		t.Skip("the child reads its peak from /proc/self/status (VmHWM), which linux has")
	}
	peak := func(mode string) int64 {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^Test_memory_peak$")
		cmd.Env = append(os.Environ(), peakEnv+"="+mode, "GOGC=100", "GOMEMLIMIT=off")
		out, err := cmd.CombinedOutput()
		_, kb, ok := strings.Cut(string(out), peakMarker)
		kb, _, _ = strings.Cut(kb, " kB")
		n, perr := strconv.ParseInt(kb, 10, 64)
		if err != nil || !ok || perr != nil {
			t.Fatalf("the %s child failed: %v\n%s", mode, err, out)
		}
		return n << 10
	}
	idle := peak("idle")
	size := int64(peakPages << 16)
	reserved := peak("reserve") - idle
	heap := peak("heap") - idle
	t.Logf("above an idle child's %d MiB: %.1f MiB reserved, %.1f MiB in the Go heap, for %d MiB of module memory",
		idle>>20, float64(reserved)/(1<<20), float64(heap)/(1<<20), size>>20)
	if reserved > size+reservedSlack {
		t.Errorf("with the memory reserved, the process peaked %d bytes above idle, more than the module's %d and %d more", reserved, size, reservedSlack)
	}
	if heap > 2*size+heapSlack {
		t.Errorf("with the memory in the Go heap, the process peaked %d bytes above idle, more than twice the module's %d and %d more", heap, size, heapSlack)
	}
}

func runPeakChild(mode string) {
	m := use_memory_test.New()
	switch mode {
	case "idle":
	case "reserve":
		if !m.UseMemory(reserve.Memory(m, m.MemoryMax())) {
			fmt.Println("no reservation")
			os.Exit(1)
		}
		fallthrough
	case "heap":
		if got := m.Xgrow_to(peakPages); got != peakPages {
			fmt.Println("grow_to returned " + strconv.Itoa(int(got)))
			os.Exit(1)
		}
	}
	// The process's own peak resident memory: after exec, VmHWM is this
	// program's (a child's rusage can count its parent's pages from before
	// the exec).
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	for _, line := range strings.Split(string(status), "\n") {
		if v, ok := strings.CutPrefix(line, "VmHWM:"); ok {
			fmt.Println(peakMarker + strings.TrimSpace(v))
			os.Exit(0)
		}
	}
	fmt.Println("no VmHWM in /proc/self/status")
	os.Exit(1)
}
