// Input to wasm2go -provided, which copies these declarations into its
// translation; this file is not built.

//go:build ignore

package wasm2go

// The provided import reads memory through load64, which the module's own
// code never emits (it has no i64.load); the translator must emit the helper
// because this file references it.
// Its receiver is named mod: in a module written as several packages,
// the function takes the module as a parameter of that name.
func (mod *Module) _peek(addr int32) int64 {
	return int64(load64(mod.memory, uint32(addr)))
}
