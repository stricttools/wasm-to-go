package passes

import "testing"

func TestLower(t *testing.T) {
	src := `func (m *Module) f(v0 int32) int64 {
		mem := *m.memory
		t0 := int32(load32(mem, uint64(uint32(v0))+8))
		t1 := int32(load16u(mem, uint32(v0)))
		store64(mem, uint32(v0), uint64(t0+t1))
		store32u(*m.memory, uint64(uint32(v0))+4, uint32(load32(mem, uint32(t0))))
		m.f2(load64u(mem, uint32(v0)))
		return int64(load64(mem, uint32(v0)))
	}`
	want := `func (m *Module) f(v0 int32) int64 {
		mem := *m.memory
		t0 := int32(binary.LittleEndian.Uint32(mem[uint64(uint32(v0))+8:]))
		t1 := int32(binary.LittleEndian.Uint16(mem[uint32(v0):]))
		binary.LittleEndian.PutUint64(mem[uint32(v0):], uint64(t0+t1))
		binary.LittleEndian.PutUint32((*m.memory)[uint64(uint32(v0))+4:], uint32(binary.LittleEndian.Uint32(mem[uint32(t0):])))
		m.f2(binary.LittleEndian.Uint64(mem[uint32(v0):]))
		return int64(binary.LittleEndian.Uint64(mem[uint32(v0):]))
	}`

	fn := parseFunc(t, src)
	undo, sites := Lower(fn)
	if sites != 7 {
		t.Errorf("Lower = %d sites, want 7", sites)
	}
	if got, want := formatFunc(t, fn), normalizeFunc(t, want); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
	undo()
	if got, want := formatFunc(t, fn), normalizeFunc(t, src); got != want {
		t.Errorf("after undo, got:\n%s\n\nwant:\n%s", got, want)
	}
}

// Calls that are not the memory access helpers, or have another shape,
// are left alone.
func TestLowerOthers(t *testing.T) {
	src := `func (m *Module) f(v0 int32) int32 {
		t0 := load32x(m.memory, uint32(v0))
		t1 := i32(load8)
		return memory_grow(&m.memory, int64(t0), int64(t1))
	}`
	fn := parseFunc(t, src)
	if _, sites := Lower(fn); sites != 0 {
		t.Errorf("Lower = %d sites, want 0", sites)
	}
	if got, want := formatFunc(t, fn), normalizeFunc(t, src); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}
