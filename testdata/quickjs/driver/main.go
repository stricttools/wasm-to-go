// Command driver runs a JavaScript file on QuickJS translated by wasm2go
// and prints what the file's function result() returns. Test_quickjs
// builds it in a module of its own, beside the translation it writes at
// qjstest/qjs.
package main

import (
	"encoding/binary"
	"fmt"
	"os"

	"qjstest/qjs"
)

// The host of the module's imports: print and the WASI functions
// wasi-libc calls. The clock is fixed, so the output depends on nothing
// but the file.
type host struct{ m *qjs.Module }

func (h *host) Init(m any)             { h.m = m.(*qjs.Module) }
func (h *host) mem() []byte            { return *h.m.Xmemory().Slice() }
func (h *host) Xhost_print(p, n int32) { os.Stdout.Write(h.mem()[p : p+n]) }
func (h *host) Xclock_time_get(id int32, prec int64, out int32) int32 {
	binary.LittleEndian.PutUint64(h.mem()[out:], 0)
	return 0
}
func (h *host) Xfd_close(fd int32) int32                     { return 8 }
func (h *host) Xfd_fdstat_get(fd, p int32) int32             { return 8 }
func (h *host) Xfd_seek(fd int32, o int64, w, p int32) int32 { return 8 }
func (h *host) Xfd_write(fd, iovs, n, pn int32) int32 {
	mem := h.mem()
	total := 0
	for i := range n {
		b := binary.LittleEndian.Uint32(mem[iovs+8*i:])
		l := binary.LittleEndian.Uint32(mem[iovs+8*i+4:])
		os.Stderr.Write(mem[b : b+l])
		total += int(l)
	}
	binary.LittleEndian.PutUint32(mem[pn:], uint32(total))
	return 0
}

func main() {
	src, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	h := &host{}
	m := qjs.New(h, h)
	m.X_initialize()
	ctx := m.Xqb_new(-1)
	cstr := func(s string) int32 {
		p := m.Xqb_malloc(int32(len(s) + 1))
		mem := h.mem()
		copy(mem[p:], s)
		mem[int(p)+len(s)] = 0
		return p
	}
	out := func() string {
		p, n := m.Xqb_out(), m.Xqb_out_len()
		return string(h.mem()[p : p+n])
	}
	if m.Xqb_eval(ctx, cstr(string(src)), int32(len(src)), cstr(os.Args[1])) != 0 {
		fmt.Fprintln(os.Stderr, "eval:", out())
		os.Exit(1)
	}
	if m.Xqb_call(ctx, cstr("result")) != 0 {
		fmt.Fprintln(os.Stderr, "result:", out())
		os.Exit(1)
	}
	fmt.Println(out())
}
