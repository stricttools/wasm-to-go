package passes

import (
	"go/ast"
	"testing"
)

func TestMemLocal(t *testing.T) {
	grows := func(call *ast.CallExpr) bool {
		switch formatExpr(call.Fun) {
		case "m.grow", "memory_grow", "m.memImp.Grow":
			return true
		}
		return false
	}

	tests := []struct {
		name   string
		memory string // *m.memory (imported) or m.memory (owned)
		src    string
		want   string
	}{
		{
			name:   "imported",
			memory: "*m.memory",
			src: `func (m *Module) f(v0 int32) int32 {
				t0 := int32(load32(*m.memory, uint32(v0)))
				t1 := m.grow(t0)
				store32(*m.memory, uint32(t1), uint32(v0))
				m.grow(0)
				_ = int32(m.memImp.Grow(0, m.maxMem))
				t2 := (*m.memory)[uint32(v0)]
				t3 := m.other(v0)
				return int32(t2) + t3 + int32(len(*m.memory))
			}`,
			want: `func (m *Module) f(v0 int32) int32 {
				mem := *m.memory
				t0 := int32(load32(mem, uint32(v0)))
				t1 := m.grow(t0)
				mem = *m.memory
				store32(mem, uint32(t1), uint32(v0))
				m.grow(0)
				mem = *m.memory
				_ = int32(m.memImp.Grow(0, m.maxMem))
				mem = *m.memory
				t2 := (mem)[uint32(v0)] // the translator removes parentheses last
				t3 := m.other(v0)
				return int32(t2) + t3 + int32(len(mem))
			}`,
		},
		{
			name:   "owned, grown by the helper",
			memory: "m.memory",
			src: `func (m *Module) f(v0 int32) int32 {
				t0 := int32(memory_grow(&m.memory, &m.memBacking, int64(v0), m.maxMem))
				m.memory[uint32(t0)] = 1
				return m.grow(t0)
			}`,
			want: `func (m *Module) f(v0 int32) int32 {
				mem := m.memory
				t0 := int32(memory_grow(&m.memory, &m.memBacking, int64(v0), m.maxMem))
				mem = m.memory
				mem[uint32(t0)] = 1
				return m.grow(t0)
			}`,
		},
		{
			name:   "labeled, in a switch",
			memory: "*m.memory",
			src: `func (m *Module) f(v0 int32) {
			l0:
				m.grow(v0)
				switch v0 {
				case 1:
					v0 = m.grow(v0)
				default:
					store32(*m.memory, uint32(v0), 1)
					goto l0
				}
			}`,
			want: `func (m *Module) f(v0 int32) {
				mem := *m.memory
			l0:
				m.grow(v0)
				mem = *m.memory
				switch v0 {
				case 1:
					v0 = m.grow(v0)
					mem = *m.memory
				default:
					store32(mem, uint32(v0), 1)
					goto l0
				}
			}`,
		},
		{
			name:   "growing call nested in an expression",
			memory: "*m.memory",
			src: `func (m *Module) f(v0 int32) int32 {
				t0 := int32(load32(*m.memory, uint32(v0)))
				return t0 + m.grow(v0)
			}`,
		},
		{
			name:   "growing call in a condition",
			memory: "*m.memory",
			src: `func (m *Module) f(v0 int32) int32 {
				if m.grow(v0) != 0 {
					return 0
				}
				return int32(load32(*m.memory, uint32(v0)))
			}`,
		},
		{
			name:   "growing call assigned to a non-identifier",
			memory: "*m.memory",
			src: `func (m *Module) f(v0 int32) {
				m.g0 = m.grow(v0)
				store32(*m.memory, uint32(v0), 1)
			}`,
		},
		{
			name:   "memory address taken by a call that cannot grow",
			memory: "m.memory",
			src: `func (m *Module) f(v0 int32) {
				keep(&m.memory)
				m.memory[v0] = 1
			}`,
		},
		{
			name:   "memory assigned",
			memory: "m.memory",
			src: `func (m *Module) f(v0 int32) {
				m.memory = nil
				m.memory[v0] = 1
			}`,
		},
		{
			name:   "no reads",
			memory: "*m.memory",
			src: `func (m *Module) f(v0 int32) int32 {
				return m.grow(v0)
			}`,
		},
		{
			name:   "name in use",
			memory: "*m.memory",
			src: `func (m *Module) f(mem int32) int32 {
				return int32(load32(*m.memory, uint32(mem)))
			}`,
		},
		{
			name:   "not a method",
			memory: "*m.memory",
			src: `func f(m *Module, v0 int32) int32 {
				return int32(load32(*m.memory, uint32(v0)))
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := parseFunc(t, tt.src)
			isMem := func(e ast.Expr) bool { return formatExpr(e) == tt.memory }
			want, changed := tt.want, true
			if want == "" {
				want, changed = tt.src, false
			}
			if got := MemLocal(fn, isMem, mustParseExpr(tt.memory), grows); got != changed {
				t.Errorf("MemLocal = %v, want %v", got, changed)
			}
			if got, want := formatFunc(t, fn), normalizeFunc(t, want); got != want {
				t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
			}
		})
	}
}
