// Package stackweight is the stack-weight pass: it rewrites a WebAssembly
// module so that each function on a cycle of its call graph charges an
// estimate of its native frame to the module's own stack in linear memory,
// the stack clang's C code keeps through the exported __stack_pointer
// global. A limit the module's code puts on that stack (QuickJS's, for
// example) then also bounds the native stack of whatever engine runs the
// module, and it is crossed at the same call on every engine, since the
// charges are part of the module. Past a bound of its own, the pass traps.
//
// The README's section on the stack-weight pass states what this
// guarantees and on which measurements it rests.
package stackweight

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/stricttools/wasm-to-go/internal/callgraph"
)

// SectionName names the custom section the pass adds, which records its
// options; the pass refuses a module that has it already.
const SectionName = "stack-weight"

// Options are what the module's build declares.
type Options struct {
	// StackLimit is how far, in bytes, the charges and the module's own
	// frames may take __stack_pointer below its initial value before the
	// pass traps. The module's own limit, when it has one, must be closer
	// to the top, so its error comes first.
	StackLimit int64
	// NativeStack is the native stack, in bytes, estimated frames may
	// take together at StackLimit: each function charges its estimate
	// times StackLimit / NativeStack.
	NativeStack int64
}

// Function is what the pass decided about one function of the module.
type Function struct {
	Index     int  // in the function index space (imports first)
	Recursive bool // on a cycle of the call graph
	Params    []byte
	Locals    []byte // the declared locals, one value type each
	Estimate  int64  // the estimated native frame, in bytes
	Charge    int64  // what it charges (0 when it is not recursive)
}

// Result is the rewritten module and what the pass did.
type Result struct {
	Module    []byte
	Functions []Function // the functions with code, in index order
	// Floor is the lowest value __stack_pointer may take before a
	// charging function traps: its initial value less StackLimit.
	Floor int64
}

type section struct {
	id   byte
	body []byte
}

type module struct {
	sections      []section
	types         []funcType
	importedFuncs int
	importedGlobs int
	funcTypes     []uint32 // of the defined functions
	sp            int      // __stack_pointer's global index
	spInit        int32
	tabled        map[int]bool // functions a table may hold
}

// Weigh runs the pass over a module.
func Weigh(wasm []byte, opt Options) (*Result, error) {
	if opt.StackLimit <= 0 || opt.NativeStack <= 0 {
		return nil, errors.New("the stack limit and the native stack must both be positive")
	}
	m, err := parse(wasm)
	if err != nil {
		return nil, err
	}
	floor := int64(m.spInit) - opt.StackLimit
	if floor < 0 {
		return nil, fmt.Errorf("__stack_pointer starts at %d, so a stack limit of %d bytes would reach below address 0; lower the stack limit or give the module a larger stack", m.spInit, opt.StackLimit)
	}
	code, err := m.bodies()
	if err != nil {
		return nil, err
	}
	fns := make([]Function, len(code))
	calls := make([][]int, len(code))
	for i, body := range code {
		ft := m.types[m.funcTypes[i]]
		f := &fns[i]
		f.Index = m.importedFuncs + i
		f.Params = ft.params
		if f.Locals, err = locals(body.body); err != nil {
			return nil, fmt.Errorf("function %d: %w", f.Index, err)
		}
		f.Estimate = estimate(ft, f.Locals)
		cs, err := m.callees(body.body)
		if err != nil {
			return nil, fmt.Errorf("function %d: %w", f.Index, err)
		}
		calls[i] = cs
	}
	for i := range callgraph.Recursive(calls) {
		fns[i].Recursive = true
		// The charge is rounded up to 16 bytes, the stack's alignment in
		// clang's C ABI, so the module's own frames stay aligned.
		fns[i].Charge = (fns[i].Estimate*opt.StackLimit/opt.NativeStack + 15) &^ 15
		if fns[i].Charge > floor || fns[i].Charge >= 1<<31 {
			return nil, fmt.Errorf("function %d would charge %d bytes, more than the %d bytes below the stack limit; lower the stack limit or raise the native stack", fns[i].Index, fns[i].Charge, floor)
		}
	}
	out, err := m.rewrite(code, fns, floor, opt)
	if err != nil {
		return nil, err
	}
	return &Result{Module: out, Functions: fns, Floor: floor}, nil
}

func parse(b []byte) (*module, error) {
	if len(b) < 8 || !bytes.Equal(b[:4], []byte("\x00asm")) {
		return nil, errors.New("the input is not a WebAssembly module")
	}
	if !bytes.Equal(b[4:8], []byte{1, 0, 0, 0}) {
		return nil, errors.New("the input is not a WebAssembly module of binary format version 1")
	}
	m := &module{sp: -1, tabled: map[int]bool{}}
	r := &reader{b: b, pos: 8}
	for !r.done() {
		id := r.byte()
		body := r.bytes(r.count())
		m.sections = append(m.sections, section{id, body})
	}
	if r.err != nil {
		return nil, r.err
	}
	refs := func(f uint32) { m.tabled[int(f)] = true }
	for _, s := range m.sections {
		sr := &reader{b: s.body}
		switch s.id {
		case secCustom:
			if name := string(sr.bytes(sr.count())); name == SectionName {
				return nil, fmt.Errorf("the module has a %q section already: the pass ran on it before, and charging twice would double every charge", SectionName)
			}
			continue
		case secType:
			for range sr.count() {
				if form := sr.byte(); form != 0x60 && sr.err == nil {
					return nil, fmt.Errorf("type form 0x%02x is not a function type (garbage collection types are refused)", form)
				}
				var t funcType
				for range sr.count() {
					t.params = append(t.params, sr.valueType())
				}
				for range sr.count() {
					t.results = append(t.results, sr.valueType())
				}
				m.types = append(m.types, t)
			}
		case secImport:
			for range sr.count() {
				sr.bytes(sr.count())
				sr.bytes(sr.count())
				switch kind := sr.byte(); kind {
				case 0: // function
					sr.u32()
					m.importedFuncs++
				case 1: // table
					sr.valueType()
					sr.limits()
				case 2: // memory
					sr.limits()
				case 3: // global
					sr.valueType()
					sr.byte()
					m.importedGlobs++
				default:
					if sr.err == nil {
						return nil, fmt.Errorf("import kind %d is not one the pass decodes (exception tags are refused)", kind)
					}
				}
			}
		case secFunction:
			for range sr.count() {
				m.funcTypes = append(m.funcTypes, sr.u32())
			}
		case secExport:
			for range sr.count() {
				name := string(sr.bytes(sr.count()))
				kind := sr.byte()
				idx := int(sr.u32())
				if name == "__stack_pointer" && kind == 3 {
					m.sp = idx
				}
			}
		case secElement:
			sr.elements(refs)
		case secTag:
			return nil, errors.New("the module declares exception tags, which the pass refuses: an exception unwinds frames without giving back their charge; build without -fwasm-exceptions")
		default:
			continue
		}
		if sr.err != nil {
			return nil, fmt.Errorf("section %d: %w", s.id, sr.err)
		}
		if !sr.done() {
			return nil, fmt.Errorf("section %d has bytes after its contents", s.id)
		}
	}
	for _, t := range m.funcTypes {
		if int(t) >= len(m.types) {
			return nil, fmt.Errorf("a function has type %d, and the module has %d types", t, len(m.types))
		}
	}
	if m.sp < 0 {
		return nil, errors.New("the module does not export __stack_pointer, the global its C code keeps its stack in; link it with -Wl,--export=__stack_pointer")
	}
	if m.sp < m.importedGlobs {
		return nil, errors.New("__stack_pointer is imported; the pass needs the module to define it, with its initial value")
	}
	// The globals section: __stack_pointer's type and initial value, and
	// the functions constant expressions refer to.
	found := false
	for _, s := range m.sections {
		if s.id != secGlobal {
			continue
		}
		sr := &reader{b: s.body}
		for i := range sr.count() {
			typ := sr.valueType()
			mut := sr.byte()
			v, plain := sr.constExpr(refs)
			if m.importedGlobs+i != m.sp {
				continue
			}
			if typ != i32 || mut != 1 {
				return nil, errors.New("__stack_pointer is not a mutable i32 global (a 64-bit memory's stack is not supported)")
			}
			if !plain || v <= 0 {
				return nil, errors.New("__stack_pointer's initial value is not a positive i32.const, which the pass needs to place its bound")
			}
			m.spInit, found = v, true
		}
		if sr.err != nil {
			return nil, fmt.Errorf("section %d: %w", s.id, sr.err)
		}
	}
	if !found {
		return nil, errors.New("__stack_pointer's global has no definition")
	}
	return m, nil
}

// limits skips a table's or memory's limits.
func (r *reader) limits() {
	flags := r.byte()
	r.skipLEB()
	if flags&1 != 0 {
		r.skipLEB()
	}
}

// elements reads the element section, reporting every function a segment
// names: any of them may be in a table.
func (r *reader) elements(refs func(uint32)) {
	for range r.count() {
		flags := r.u32()
		if flags > 7 {
			r.fail(fmt.Errorf("element segment kind %d is not one the pass decodes", flags))
			return
		}
		if flags&3 == 2 {
			r.u32() // table index
		}
		if flags&1 == 0 {
			r.constExpr(func(uint32) {}) // offset
		}
		if flags&3 != 0 {
			r.byte() // element kind or reference type
		}
		n := r.count()
		for range n {
			if flags&4 != 0 {
				r.constExpr(refs)
			} else {
				refs(r.u32())
			}
		}
	}
}

type body struct {
	body []byte
}

// bodies returns the code section's function bodies.
func (m *module) bodies() ([]body, error) {
	for _, s := range m.sections {
		if s.id != secCode {
			continue
		}
		sr := &reader{b: s.body}
		n := sr.count()
		if n != len(m.funcTypes) {
			return nil, fmt.Errorf("the code section has %d bodies for %d functions", n, len(m.funcTypes))
		}
		out := make([]body, n)
		for i := range out {
			out[i].body = sr.bytes(sr.count())
		}
		if sr.err != nil {
			return nil, fmt.Errorf("the code section: %w", sr.err)
		}
		return out, nil
	}
	if len(m.funcTypes) > 0 {
		return nil, errors.New("the module declares functions but has no code section")
	}
	return nil, nil
}

// locals returns a body's declared locals and leaves nothing else read.
func locals(b []byte) ([]byte, error) {
	r := &reader{b: b}
	var out []byte
	for range r.count() {
		n := r.count()
		t := r.valueType()
		if r.err != nil {
			break
		}
		if n > 50000 || len(out)+n > 50000 {
			return nil, errors.New("the function declares more than 50,000 locals, more than engines accept")
		}
		for range n {
			out = append(out, t)
		}
	}
	return out, r.err
}

// code returns a reader positioned at a body's code, after its locals.
func code(b []byte) *reader {
	r := &reader{b: b}
	for range r.count() {
		r.u32()
		r.valueType()
	}
	return r
}

// callees returns the defined functions a body may call, by their index
// among the defined functions: its direct calls, and for each indirect
// call every function of the called type that a table may hold.
func (m *module) callees(b []byte) ([]int, error) {
	r := code(b)
	var out []int
	add := func(defined int) {
		if !slices.Contains(out, defined) {
			out = append(out, defined)
		}
	}
	r.instructions(func(in instr) {
		switch in.op {
		case 0x10:
			if i := int(in.callee) - m.importedFuncs; i >= 0 && i < len(m.funcTypes) {
				add(i)
			}
		case 0x11:
			if int(in.callee) >= len(m.types) {
				r.fail(fmt.Errorf("call_indirect names type %d, and the module has %d types", in.callee, len(m.types)))
				return
			}
			key := m.types[in.callee].key()
			for f := range m.tabled {
				if i := f - m.importedFuncs; i >= 0 && i < len(m.funcTypes) && m.types[m.funcTypes[i]].key() == key {
					add(i)
				}
			}
		case 0xd2:
			m.tabled[int(in.callee)] = true
		}
	})
	slices.Sort(out)
	return out, r.err
}

// rewrite adds the charges and the custom section.
func (m *module) rewrite(code []body, fns []Function, floor int64, opt Options) ([]byte, error) {
	var extra []funcType // block types appended to the type section
	blockType := func(results []byte) []byte {
		switch len(results) {
		case 0:
			return []byte{0x40}
		case 1:
			return []byte{results[0]}
		}
		want := funcType{results: results}.key()
		for i, t := range m.types {
			if t.key() == want {
				return sleb(int32(i))
			}
		}
		for i, t := range extra {
			if t.key() == want {
				return sleb(int32(len(m.types) + i))
			}
		}
		extra = append(extra, funcType{results: results})
		return sleb(int32(len(m.types) + len(extra) - 1))
	}
	var codeSec bytes.Buffer
	codeSec.Write(uleb(uint32(len(code))))
	for i, c := range code {
		b := c.body
		if fns[i].Recursive {
			var err error
			if b, err = m.charge(b, fns[i].Charge, floor, blockType(m.types[m.funcTypes[i]].results)); err != nil {
				return nil, fmt.Errorf("function %d: %w", fns[i].Index, err)
			}
		}
		codeSec.Write(uleb(uint32(len(b))))
		codeSec.Write(b)
	}
	var out bytes.Buffer
	out.Write([]byte("\x00asm\x01\x00\x00\x00"))
	write := func(id byte, body []byte) {
		out.WriteByte(id)
		out.Write(uleb(uint32(len(body))))
		out.Write(body)
	}
	for _, s := range m.sections {
		switch s.id {
		case secType:
			var t bytes.Buffer
			t.Write(uleb(uint32(len(m.types) + len(extra))))
			t.Write(s.body[len(uleb(uint32(len(m.types)))):])
			for _, e := range extra {
				t.WriteByte(0x60)
				t.Write(uleb(uint32(len(e.params))))
				t.Write(e.params)
				t.Write(uleb(uint32(len(e.results))))
				t.Write(e.results)
			}
			write(s.id, t.Bytes())
		case secCode:
			write(s.id, codeSec.Bytes())
		default:
			write(s.id, s.body)
		}
	}
	var note strings.Builder
	name := SectionName
	note.WriteString(string(uleb(uint32(len(name)))) + name)
	note.WriteString("stack-limit " + strconv.FormatInt(opt.StackLimit, 10) + "\nnative-stack " + strconv.FormatInt(opt.NativeStack, 10) + "\nestimate " + EstimateVersion + "\n")
	write(secCustom, []byte(note.String()))
	return out.Bytes(), nil
}

// charge returns a recursive function's body charging w bytes: at entry it
// lowers __stack_pointer by w and traps below floor; before every return,
// and at the end of the code, which a block wrapping the code makes the
// target of every branch to the function's own label, it raises it by w.
func (m *module) charge(b []byte, w, floor int64, blockType []byte) ([]byte, error) {
	r := code(b)
	start := r.pos
	var returns []int
	r.instructions(func(in instr) {
		if in.op == 0x0f {
			returns = append(returns, in.at)
		}
	})
	if r.err != nil {
		return nil, r.err
	}
	sp := uleb(uint32(m.sp))
	adjust := func(op byte) []byte { // global.get sp; i32.const w; op; global.set sp
		o := append([]byte{0x23}, sp...)
		o = append(o, 0x41)
		o = append(o, sleb(int32(w))...)
		o = append(o, op, 0x24)
		return append(o, sp...)
	}
	var o bytes.Buffer
	o.Write(b[:start])
	o.Write(adjust(0x6b)) // i32.sub
	// global.get sp; i32.const floor; i32.lt_s; if; unreachable; end:
	// signed, so a stack pointer that wrapped below 0 traps too.
	o.WriteByte(0x23)
	o.Write(sp)
	o.WriteByte(0x41)
	o.Write(sleb(int32(floor)))
	o.Write([]byte{0x48, 0x04, 0x40, 0x00, 0x0b})
	o.WriteByte(0x02)
	o.Write(blockType)
	prev := start
	for _, at := range returns {
		o.Write(b[prev:at])
		o.Write(adjust(0x6a)) // i32.add
		prev = at
	}
	o.Write(b[prev : len(b)-1]) // the code up to its end, which now ends the block
	o.WriteByte(0x0b)
	o.Write(adjust(0x6a))
	o.WriteByte(0x0b)
	return o.Bytes(), nil
}
