package passes

import (
	"slices"
	"testing"
)

func TestRemoveBoundsChecks(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string   // the output, when different from src
		used []string // the unchecked helpers used
	}{
		{
			name: "covered by a wider check",
			src: `func (m *Module) f(v0 int32) int32 {
				mem := *m.memory
				t0 := int32(load32(mem, uint64(uint32(v0))+8))
				t1 := int32(load16(mem, uint64(uint32(v0))+4))
				t2 := int32(load64(mem, uint32(v0)))
				store32(mem, uint64(uint32(v0))+8, uint32(t0+t1+t2))
				return t0
			}`,
			want: `func (m *Module) f(v0 int32) int32 {
				mem := *m.memory
				t0 := int32(load32(mem, uint64(uint32(v0))+8))
				t1 := int32(load16u(mem, uint64(uint32(v0))+4))
				t2 := int32(load64u(mem, uint32(v0)))
				store32u(mem, uint64(uint32(v0))+8, uint32(t0+t1+t2))
				return t0
			}`,
			used: []string{"load16u", "load64u", "store32u"},
		},
		{
			name: "earlier in the same statement, and a byte access",
			src: `func (m *Module) f(v0 int32) int32 {
				mem := *m.memory
				_ = mem[uint64(uint32(v0))+7]
				return int32(load32(mem, uint32(v0))) + int32(load32(mem, uint32(v0)))
			}`,
			want: `func (m *Module) f(v0 int32) int32 {
				mem := *m.memory
				_ = mem[uint64(uint32(v0))+7]
				return int32(load32u(mem, uint32(v0))) + int32(load32u(mem, uint32(v0)))
			}`,
			used: []string{"load32u"},
		},
		{
			name: "narrower check",
			src: `func (m *Module) f(v0 int32) int64 {
				mem := *m.memory
				_ = load16(mem, uint32(v0))
				return int64(load64(mem, uint32(v0)))
			}`,
		},
		{
			name: "address assigned",
			src: `func (m *Module) f(v0 int32) int32 {
				mem := *m.memory
				_ = load32(mem, uint32(v0))
				v0 = v0 + 4
				return int32(load32(mem, uint32(v0)))
			}`,
		},
		{
			name: "memory reloaded",
			src: `func (m *Module) f(v0 int32) int32 {
				mem := *m.memory
				_ = load32(mem, uint32(v0))
				m.g(v0)
				mem = *m.memory
				return int32(load32(mem, uint32(v0)))
			}`,
		},
		{
			name: "only on one branch, or conditionally",
			src: `func (m *Module) f(v0, v1 int32) int32 {
				mem := *m.memory
				if v1 != 0 {
					_ = load32(mem, uint32(v0))
				}
				if v1 == 1 && load32(mem, uint32(v1)) != 0 {
					return 0
				}
				return int32(load32(mem, uint32(v0))) + int32(load32(mem, uint32(v1)))
			}`,
		},
		{
			name: "on both branches",
			src: `func (m *Module) f(v0, v1 int32) int32 {
				mem := *m.memory
				if v1 != 0 {
					_ = load32(mem, uint32(v0))
				} else {
					_ = load64(mem, uint32(v0))
				}
				return int32(load32(mem, uint32(v0)))
			}`,
			want: `func (m *Module) f(v0, v1 int32) int32 {
				mem := *m.memory
				if v1 != 0 {
					_ = load32(mem, uint32(v0))
				} else {
					_ = load64(mem, uint32(v0))
				}
				return int32(load32u(mem, uint32(v0)))
			}`,
			used: []string{"load32u"},
		},
		{
			name: "loop through a label",
			src: `func (m *Module) f(v0, v1 int32) int32 {
				mem := *m.memory
				_ = load32(mem, uint32(v0))
			l0:
				v1 = v1 + int32(load32(mem, uint32(v0)))
				v0 = v0 + 4
				if v1 != 0 {
					goto l0
				}
				return v1
			}`,
		},
		{
			name: "loop that keeps the address",
			src: `func (m *Module) f(v0, v1 int32) int32 {
				mem := *m.memory
				_ = load32(mem, uint32(v0))
			l0:
				v1 = v1 + int32(load32(mem, uint32(v0)))
				if v1 != 0 {
					goto l0
				}
				return v1
			}`,
			want: `func (m *Module) f(v0, v1 int32) int32 {
				mem := *m.memory
				_ = load32(mem, uint32(v0))
			l0:
				v1 = v1 + int32(load32u(mem, uint32(v0)))
				if v1 != 0 {
					goto l0
				}
				return v1
			}`,
			used: []string{"load32u"},
		},
		{
			name: "through a copy, until it is assigned",
			src: `func (m *Module) f(v0 int32) int32 {
				mem := *m.memory
				var v1 int32
				_ = load32(mem, uint32(v0))
				v1 = v0
				t0 := int32(load32(mem, uint32(v1)))
				v1 = t0
				return int32(load32(mem, uint32(v1)))
			}`,
			want: `func (m *Module) f(v0 int32) int32 {
				mem := *m.memory
				var v1 int32
				_ = load32(mem, uint32(v0))
				v1 = v0
				t0 := int32(load32u(mem, uint32(v1)))
				v1 = t0
				return int32(load32(mem, uint32(v1)))
			}`,
			used: []string{"load32u"},
		},
		{
			name: "switch cases, fallthrough",
			src: `func (m *Module) f(v0, v1 int32) int32 {
				mem := *m.memory
				switch v1 {
				case 0:
					_ = load32(mem, uint32(v0))
					fallthrough
				case 1:
					return int32(load32(mem, uint32(v0)))
				}
				return 0
			}`,
		},
		{
			name: "no mem local",
			src: `func (m *Module) f(v0 int32) int32 {
				_ = load32(*m.memory, uint32(v0))
				return int32(load32(*m.memory, uint32(v0)))
			}`,
		},
		{
			name: "name declared twice",
			src: `func (m *Module) f(v0 int32) int32 {
				mem := *m.memory
				_ = load32(mem, uint32(v0))
				{
					v0 := v0 + 4
					_ = v0
				}
				return int32(load32(mem, uint32(v0)))
			}`,
		},
		{
			name: "address of a local",
			src: `func (m *Module) f(v0 int32) int32 {
				mem := *m.memory
				_ = load32(mem, uint32(v0))
				keep(&v0)
				return int32(load32(mem, uint32(v0)))
			}`,
		},
		{
			name: "a for statement",
			src: `func (m *Module) f(v0 int32) int32 {
				mem := *m.memory
				_ = load32(mem, uint32(v0))
				for {
					return int32(load32(mem, uint32(v0)))
				}
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := parseFunc(t, tt.src)
			want := tt.want
			if want == "" {
				want = tt.src
			}
			used := RemoveBoundsChecks(fn)
			slices.Sort(used)
			if !slices.Equal(used, tt.used) {
				t.Errorf("used %v, want %v", used, tt.used)
			}
			if got, want := formatFunc(t, fn), normalizeFunc(t, want); got != want {
				t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
			}
		})
	}
}
