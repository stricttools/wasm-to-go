package stackweight

import (
	"errors"
	"fmt"
)

// The subset of the binary format the pass reads: the sections it needs
// decoded, and every instruction of a function body skipped by its
// immediates. What it does not decode (exceptions, tail calls, function
// references, SIMD, and garbage collection) it refuses by name.

const (
	secCustom   = 0
	secType     = 1
	secImport   = 2
	secFunction = 3
	secGlobal   = 6
	secExport   = 7
	secElement  = 9
	secCode     = 10
	secTag      = 13
)

// Value types.
const (
	i32       = 0x7f
	i64       = 0x7e
	f32       = 0x7d
	f64       = 0x7c
	v128      = 0x7b
	funcref   = 0x70
	externref = 0x6f
)

type reader struct {
	b   []byte
	pos int
	err error
}

var errShort = errors.New("the module ends inside a section or an instruction")

func (r *reader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
	r.pos = len(r.b)
}

func (r *reader) done() bool { return r.pos >= len(r.b) || r.err != nil }

func (r *reader) byte() byte {
	if r.pos >= len(r.b) {
		r.fail(errShort)
		return 0
	}
	c := r.b[r.pos]
	r.pos++
	return c
}

func (r *reader) bytes(n int) []byte {
	if n < 0 || r.pos+n > len(r.b) {
		r.fail(errShort)
		return nil
	}
	s := r.b[r.pos : r.pos+n]
	r.pos += n
	return s
}

// u32 reads an unsigned LEB128 of at most 32 bits.
func (r *reader) u32() uint32 {
	var v uint32
	for shift := uint(0); ; shift += 7 {
		c := r.byte()
		if r.err != nil {
			return 0
		}
		if shift >= 32 {
			r.fail(errors.New("an unsigned LEB128 is longer than 32 bits"))
			return 0
		}
		v |= uint32(c&0x7f) << shift
		if c&0x80 == 0 {
			return v
		}
	}
}

// count reads a vector's length.
func (r *reader) count() int { return int(r.u32()) }

// skipLEB skips a LEB128 of any width.
func (r *reader) skipLEB() {
	for r.byte()&0x80 != 0 && r.err == nil {
	}
}

// s32 reads a signed LEB128 of at most 32 bits.
func (r *reader) s32() int32 {
	var v int64
	var shift uint
	for {
		c := r.byte()
		if r.err != nil {
			return 0
		}
		v |= int64(c&0x7f) << shift
		shift += 7
		if c&0x80 == 0 {
			if shift < 64 && c&0x40 != 0 {
				v |= -1 << shift
			}
			return int32(v)
		}
		if shift >= 35 {
			r.fail(errors.New("a signed LEB128 is longer than 32 bits"))
			return 0
		}
	}
}

func uleb(v uint32) []byte {
	var o []byte
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v == 0 {
			return append(o, c)
		}
		o = append(o, c|0x80)
	}
}

func sleb(v int32) []byte {
	var o []byte
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && c&0x40 == 0) || (v == -1 && c&0x40 != 0) {
			return append(o, c)
		}
		o = append(o, c|0x80)
	}
}

// valueType reads a value type the pass knows.
func (r *reader) valueType() byte {
	t := r.byte()
	switch t {
	case i32, i64, f32, f64, v128, funcref, externref:
		return t
	}
	if r.err == nil {
		r.fail(fmt.Errorf("value type 0x%02x is not one the pass decodes (typed function references and garbage collection are refused)", t))
	}
	return 0
}

// A function type.
type funcType struct{ params, results []byte }

func (t funcType) key() string { return string(t.params) + "|" + string(t.results) }

// blockType skips a block type: empty, one value type, or a type index.
func (r *reader) blockType() {
	if r.pos >= len(r.b) {
		r.fail(errShort)
		return
	}
	switch c := r.b[r.pos]; c {
	case 0x40, i32, i64, f32, f64, v128, funcref, externref:
		r.pos++
		return
	default:
		if c&0x40 != 0 && c&0x80 == 0 {
			r.fail(fmt.Errorf("block type 0x%02x is not one the pass decodes", c))
			return
		}
		r.skipLEB() // a type index, as a signed 33-bit LEB128
	}
}

// memarg skips a memory access's alignment (with a memory index when its
// bit 6 is set) and offset.
func (r *reader) memarg() {
	if r.u32()&0x40 != 0 {
		r.u32()
	}
	r.skipLEB()
}

// An instruction the rewrite needs to know about.
type instr struct {
	op     byte
	at     int    // offset of the opcode in the body
	callee uint32 // call: the function; call_indirect: the type
}

// instructions walks a function body's code from r's position to the end
// of the body, reporting calls, indirect calls, returns, and references to
// functions (ref.func), and checking that the body's blocks close where it
// ends.
func (r *reader) instructions(visit func(instr)) {
	depth := 0
	for !r.done() {
		at := r.pos
		op := r.byte()
		switch op {
		case 0x00, 0x01, 0x05, 0x1a, 0x1b, 0xd1: // unreachable, nop, else, drop, select, ref.is_null
		case 0x02, 0x03, 0x04: // block, loop, if
			r.blockType()
			depth++
		case 0x0b: // end
			depth--
			if depth < 0 {
				if r.pos != len(r.b) {
					r.fail(errors.New("a function body's code ends before the body does"))
				}
				return
			}
		case 0x0c, 0x0d: // br, br_if
			r.u32()
		case 0x0e: // br_table
			n := r.count()
			for i := 0; i <= n && r.err == nil; i++ {
				r.u32()
			}
		case 0x0f: // return
			visit(instr{op: op, at: at})
		case 0x10: // call
			visit(instr{op: op, at: at, callee: r.u32()})
		case 0x11: // call_indirect
			typ := r.u32()
			r.u32()
			visit(instr{op: op, at: at, callee: typ})
		case 0x12, 0x13, 0x15:
			r.fail(errors.New("the module has tail calls (return_call), which the pass refuses: a tail call would need its caller's charge given back first; build without -mtail-call"))
		case 0x14:
			r.fail(errors.New("the module has call_ref (typed function references), which the pass refuses"))
		case 0x06, 0x07, 0x08, 0x09, 0x0a, 0x18, 0x19, 0x1f:
			r.fail(errors.New("the module uses exception handling, which the pass refuses: an exception unwinds frames without giving back their charge; build without -fwasm-exceptions"))
		case 0x1c: // select with types
			n := r.count()
			for i := 0; i < n && r.err == nil; i++ {
				r.valueType()
			}
		case 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26: // locals, globals, table.get, table.set
			r.u32()
		case 0x3f, 0x40: // memory.size, memory.grow
			r.u32()
		case 0x41:
			r.s32()
		case 0x42:
			r.skipLEB()
		case 0x43:
			r.bytes(4)
		case 0x44:
			r.bytes(8)
		case 0xd0: // ref.null
			r.valueType()
		case 0xd2: // ref.func
			visit(instr{op: op, at: at, callee: r.u32()})
		case 0xfc:
			switch sub := r.u32(); {
			case sub <= 7, sub >= 19 && sub <= 22: // saturating conversions, wide arithmetic
			case sub == 8, sub == 10, sub == 12, sub == 14: // memory.init, memory.copy, table.init, table.copy
				r.u32()
				r.u32()
			case sub == 9, sub == 11, sub == 13, sub == 15, sub == 16, sub == 17:
				r.u32()
			default:
				r.fail(fmt.Errorf("instruction 0xfc %d is not one the pass decodes", sub))
			}
		case 0xfe: // threads
			switch sub := r.u32(); {
			case sub == 3: // atomic.fence
				r.byte()
			case sub <= 2, sub >= 0x10 && sub <= 0x4e:
				r.memarg()
			default:
				r.fail(fmt.Errorf("instruction 0xfe %d is not one the pass decodes", sub))
			}
		case 0xfd:
			r.fail(errors.New("the module uses SIMD, which the pass does not decode; build without -msimd128"))
		case 0xfb:
			r.fail(errors.New("the module uses garbage collection instructions, which the pass refuses"))
		default:
			switch {
			case op >= 0x28 && op <= 0x3e:
				r.memarg()
			case op >= 0x45 && op <= 0xc4:
			default:
				r.fail(fmt.Errorf("instruction 0x%02x is not one the pass decodes", op))
			}
		}
	}
	if r.err == nil {
		r.fail(errors.New("a function body's code does not end"))
	}
}

// constExpr skips a constant expression (with its end), reporting the
// functions it refers to, and returns the value of i32.const when that is
// the whole expression.
func (r *reader) constExpr(refs func(uint32)) (value int32, plain bool) {
	start := r.pos
	for !r.done() {
		switch op := r.byte(); op {
		case 0x0b:
			if r.pos-start >= 2 && r.b[start] == 0x41 {
				c := &reader{b: r.b[:r.pos-1], pos: start + 1}
				if v := c.s32(); c.err == nil && c.pos == r.pos-1 {
					return v, true
				}
			}
			return 0, false
		case 0x41:
			r.s32()
		case 0x42:
			r.skipLEB()
		case 0x43:
			r.bytes(4)
		case 0x44:
			r.bytes(8)
		case 0x23: // global.get
			r.u32()
		case 0xd0: // ref.null
			r.valueType()
		case 0xd2: // ref.func
			refs(r.u32())
		case 0x6a, 0x6b, 0x6c, 0x7c, 0x7d, 0x7e: // extended constant arithmetic
		default:
			r.fail(fmt.Errorf("instruction 0x%02x in a constant expression is not one the pass decodes", op))
		}
	}
	r.fail(errShort)
	return 0, false
}
