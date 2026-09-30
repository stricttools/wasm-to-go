//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly)

package reserve

// No reservation: the memory stays in the Go heap. On js/wasm and wasip1
// the Go program's own linear memory is the only memory there is; on
// windows, reserving and committing on touch needs VirtualAlloc, which
// package syscall does not provide.
func reserve(int) []byte { return nil }

func release([]byte) {}
