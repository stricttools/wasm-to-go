// Input to wasm2go -provided, which copies these declarations into its
// translation; this file is not built.

//go:build ignore

package wasm2go

// Grows memory, through the helper the translator emits for memory.grow.
func (m *Module) _pgrow() int32 {
	return int32(memory_grow(&m.memory, 1, m.maxMem))
}

// Only reads memory.
func (m *Module) _ppeek(addr int32) int32 {
	return int32(m.memory[uint32(addr)])
}
