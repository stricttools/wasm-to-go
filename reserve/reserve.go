// Package reserve gives a translated module's memory address space that the
// operating system commits page by page, when each page is first touched, so
// the memory grows in place up to its maximum: no copy, no allocation, and
// no memory held beyond the pages the module has used.
//
// A host places a module's memory in a reservation with the translation's
// UseMemory method, sized by its MemoryMax method:
//
//	m := translated.New()
//	m.UseMemory(reserve.Memory(m, m.MemoryMax()))
//
// Memory returns nil where it cannot reserve (see Memory), and UseMemory
// then leaves the memory in the Go heap, so the same host code runs
// everywhere.
//
// The translation itself does not reserve: it uses only packages strictgo
// accepts, and reserving needs package syscall. This package is for hosts
// outside strictgo's checks.
package reserve

import "runtime"

// Memory reserves size bytes of address space, readable, writable, and
// zero, which the operating system commits page by page on first touch, and
// returns them as a slice (length and capacity size). The reservation is
// released when owner becomes unreachable, which must be when nothing uses
// the memory any longer: owner is the module whose memory it holds, and no
// slice of the memory may outlive it.
//
// Memory returns nil, and reserves nothing, for a size of zero or less, a
// size larger than the platform's int, where the platform has no way to
// reserve address space in pure Go (js/wasm, wasip1, windows, plan9), and
// when the operating system refuses the reservation (for example a 32-bit
// process's address space, or vm.overcommit_memory=2 on Linux).
func Memory[T any](owner *T, size int64) []byte {
	if owner == nil || size <= 0 || uint64(size) > uint64(^uint(0)>>1) {
		return nil
	}
	b := reserve(int(size))
	if b == nil {
		return nil
	}
	runtime.AddCleanup(owner, release, b)
	return b
}
