package passes

import "testing"

func TestLiterals(t *testing.T) {
	tests := []struct {
		name, src, want string
		sites           int
	}{
		{
			name: "operands, arguments, and assignments",
			src: `func (m *Module) f(v0 int32, v1 int64) int64 {
				var v2 int32
				store32(mem, uint32(v0), uint32(i32(9)))
				v2 = i32(7)
				t0 := v0 - i32(304)
				t1 := v1 & i64(-0x100000000)
				m.g(v0, i32(1), load32(mem, uint32(v0)))
				if v2 <= i32(1) {
					return i64(0x600000000)
				}
				return int64(t0) + t1
			}`,
			want: `func (m *Module) f(v0 int32, v1 int64) int64 {
				var v2 int32
				store32(mem, uint32(v0), uint32(int32(9)))
				v2 = int32(7)
				t0 := v0 - int32(304)
				t1 := v1 & int64(-0x100000000)
				m.g(v0, int32(1), load32(mem, uint32(v0)))
				if v2 <= int32(1) {
					return int64(0x600000000)
				}
				return int64(t0) + t1
			}`,
			sites: 7,
		},
		{
			name: "integer conversions that keep the value",
			src: `func (m *Module) f(v0 int32) int32 {
				if uint32(v0) < uint32(i32(4)) {
					return int32(int64(v0) + int64(i32(-5)))
				}
				return int32(uint8(v0)) + int32(uint16(i32(0xffff)))
			}`,
			want: `func (m *Module) f(v0 int32) int32 {
				if uint32(v0) < uint32(int32(4)) {
					return int32(int64(v0) + int64(int32(-5)))
				}
				return int32(uint8(v0)) + int32(uint16(int32(0xffff)))
			}`,
			sites: 3,
		},
		{
			// A Go constant expression would not compile, or would not
			// be evaluated as WebAssembly evaluates it.
			name: "kept",
			src: `func (m *Module) f(v0 int32, v1 uint64) int32 {
				t0 := i32(1) + i32(2)
				t1 := uint32(i32(-1))
				t2 := v0 / i32(0)
				t3 := v0 % i32(0)
				t4 := v0 << i32(-1)
				t5 := float32(i32(16777217))
				t6 := mem[i32(3)]
				t7 := uint32(i32(5)) + uint32(i32(6))
				t8 := -i32(8)
				t9 := v0 + len(data0)*i32(2)
				t10 := x0 + i32(1)
				t11 := load32(mem, uint32(i32(5)))
				store32(mem, i32(8), uint32(t11))
				return t0 + t2 + t3 + t4
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
			if got := Literals(fn); got != tt.sites {
				t.Errorf("Literals = %d, want %d", got, tt.sites)
			}
			if got, want := formatFunc(t, fn), normalizeFunc(t, want); got != want {
				t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
			}
		})
	}
}
