package passes

import "testing"

func TestExpand(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		want  string
		sites int
	}{
		{
			name: "checked and unchecked",
			src: `func (m *Module) f(v0 int32) int64 {
				mem := *m.memory
				t0 := int32(load32(mem, uint64(uint32(v0))+8))
				t1 := int32(load16u(mem, uint32(v0)))
				store64(mem, uint32(v0), uint64(t0+t1))
			l0:
				store32u(mem, uint64(uint32(v0))+4, uint32(load32(mem, uint32(t0))))
				return int64(load64(mem, uint32(v0)))
			}`,
			want: `func (m *Module) f(v0 int32) int64 {
				mem := *m.memory
				t0 := int32(*(*uint32)(unsafe.Add(unsafe.Pointer(&mem[uint64(uint32(v0))+8+3]), -3)))
				t1 := int32(*(*uint16)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(mem)), uintptr(uint32(v0)))))
				*(*uint64)(unsafe.Add(unsafe.Pointer(&mem[uint64(uint32(v0))+7]), -7)) = uint64(t0 + t1)
			l0:
				*(*uint32)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(mem)), uintptr(uint64(uint32(v0))+4))) = uint32(*(*uint32)(unsafe.Add(unsafe.Pointer(&mem[uint64(uint32(t0))+3]), -3)))
				return int64(*(*uint64)(unsafe.Add(unsafe.Pointer(&mem[uint64(uint32(v0))+7]), -7)))
			}`,
			sites: 6,
		},
		{
			name: "not on the mem local",
			src: `func (m *Module) f(v0 int32) int32 {
				store32(*m.memory, uint32(v0), 1)
				return int32(load32(m.memory, uint32(v0)))
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
			if got := Expand(fn); got != tt.sites {
				t.Errorf("Expand = %d, want %d", got, tt.sites)
			}
			if got, want := formatFunc(t, fn), normalizeFunc(t, want); got != want {
				t.Errorf("got:\n%s\n\nwant:\n%s", got, want)
			}
		})
	}
}
