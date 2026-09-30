// Input to wasm2go -provided, which copies these declarations into its
// translation; this file is not built.

//go:build ignore

package wasm2go

// The provided import reads memory through load64, which the module's own
// code never emits (it has no i64.load); the translator must emit the helper
// because this file references it.
func (m *Module) _peek(addr int32) int64 {
	return int64(load64(m.memory, uint32(addr)))
}
