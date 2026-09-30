//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package reserve

import "syscall"

// Maps private anonymous memory: zero, committed page by page on first
// touch (with noreserveFlag, not counted against the commit limit either).
func reserve(size int) []byte {
	b, err := syscall.Mmap(-1, 0, size, syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_ANON|noreserveFlag)
	if err != nil {
		return nil
	}
	return b
}

func release(b []byte) { syscall.Munmap(b) }
