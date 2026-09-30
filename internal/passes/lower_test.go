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
	tests := []struct {
		name  string
		bytes bool
		want  string
	}{{
		name: "calls",
		want: `func (m *Module) f(v0 int32) int64 {
			mem := *m.memory
			t0 := int32(binary.LittleEndian.Uint32(mem[uint64(uint32(v0))+8:]))
			t1 := int32(binary.LittleEndian.Uint16(mem[uint32(v0):]))
			binary.LittleEndian.PutUint64(mem[uint32(v0):], uint64(t0+t1))
			binary.LittleEndian.PutUint32((*m.memory)[uint64(uint32(v0))+4:], uint32(binary.LittleEndian.Uint32(mem[uint32(t0):])))
			m.f2(binary.LittleEndian.Uint64(mem[uint32(v0):]))
			return int64(binary.LittleEndian.Uint64(mem[uint32(v0):]))
		}`,
	}, {
		// A store of a value read from memory keeps the call.
		name:  "bytes",
		bytes: true,
		want: `func (m *Module) f(v0 int32) int64 {
			mem := *m.memory
			t0 := int32(uint32(mem[uint64(uint32(v0))+8:][3])<<24 | uint32(mem[uint64(uint32(v0))+8:][0]) | uint32(mem[uint64(uint32(v0))+8:][1])<<8 | uint32(mem[uint64(uint32(v0))+8:][2])<<16)
			t1 := int32(uint16(mem[uint32(v0):][1])<<8 | uint16(mem[uint32(v0):][0]))
			{
				mem[uint32(v0):][7] = byte(uint64(t0+t1) >> 56)
				mem[uint32(v0):][0] = byte(uint64(t0 + t1))
				mem[uint32(v0):][1] = byte(uint64(t0+t1) >> 8)
				mem[uint32(v0):][2] = byte(uint64(t0+t1) >> 16)
				mem[uint32(v0):][3] = byte(uint64(t0+t1) >> 24)
				mem[uint32(v0):][4] = byte(uint64(t0+t1) >> 32)
				mem[uint32(v0):][5] = byte(uint64(t0+t1) >> 40)
				mem[uint32(v0):][6] = byte(uint64(t0+t1) >> 48)
			}
			binary.LittleEndian.PutUint32((*m.memory)[uint64(uint32(v0))+4:], uint32(uint32(mem[uint32(t0):][3])<<24|uint32(mem[uint32(t0):][0])|uint32(mem[uint32(t0):][1])<<8|uint32(mem[uint32(t0):][2])<<16))
			m.f2(uint64(mem[uint32(v0):][7])<<56 | uint64(mem[uint32(v0):][0]) | uint64(mem[uint32(v0):][1])<<8 | uint64(mem[uint32(v0):][2])<<16 | uint64(mem[uint32(v0):][3])<<24 | uint64(mem[uint32(v0):][4])<<32 | uint64(mem[uint32(v0):][5])<<40 | uint64(mem[uint32(v0):][6])<<48)
			return int64(uint64(mem[uint32(v0):][7])<<56 | uint64(mem[uint32(v0):][0]) | uint64(mem[uint32(v0):][1])<<8 | uint64(mem[uint32(v0):][2])<<16 | uint64(mem[uint32(v0):][3])<<24 | uint64(mem[uint32(v0):][4])<<32 | uint64(mem[uint32(v0):][5])<<40 | uint64(mem[uint32(v0):][6])<<48)
		}`,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := parseFunc(t, src)
			if sites := Lower(fn, tt.bytes); sites != 7 {
				t.Errorf("Lower = %d sites, want 7", sites)
			}
			if got, want := formatFunc(t, fn), normalizeFunc(t, tt.want); got != want {
				t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
			}
		})
	}
}

// Calls that are not the memory access helpers, or have another shape,
// are left alone.
func TestLowerOthers(t *testing.T) {
	src := `func (m *Module) f(v0 int32) int32 {
		t0 := load32x(m.memory, uint32(v0))
		t1 := i32(load8)
		t2 := load32(m.memory)
		return memory_grow(&m.memory, int64(t0), int64(t1))
	}`
	fn := parseFunc(t, src)
	if sites := Lower(fn, true); sites != 0 {
		t.Errorf("Lower = %d sites, want 0", sites)
	}
	if got, want := formatFunc(t, fn), normalizeFunc(t, src); got != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
	}
}

// Clone copies every node: changing the copy leaves the original alone.
func TestClone(t *testing.T) {
	src := `func (m *Module) f(v0 int32) int32 {
		return int32(load32(m.memory, uint32(v0)))
	}`
	fn := parseFunc(t, src)
	c := Clone(fn)
	Lower(c, true)
	if got, want := formatFunc(t, fn), normalizeFunc(t, src); got != want {
		t.Errorf("the original changed:\n%s", got)
	}
}
