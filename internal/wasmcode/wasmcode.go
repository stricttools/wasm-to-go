// Package wasmcode decodes the instructions of WebAssembly function bodies:
// each instruction's opcode and immediates, for the passes that read code
// without translating it (the translator's analysis of a function's
// locals, and the stack-weight pass).
//
// It decodes every instruction of the core specification with its
// proposals the translator reads (bulk memory, reference types,
// non-trapping conversions, sign extension, multi-value, tail calls,
// threads, wide arithmetic, fixed-width and relaxed SIMD) and the
// exception handling and typed function reference instructions; it
// refuses garbage collection instructions and unknown opcodes. It checks
// the encoding, not the types: a body that decodes may still be invalid.
package wasmcode

import (
	"errors"
	"fmt"
)

// Prefixes of the instructions whose opcode is a prefix and a LEB128.
const (
	PrefixGC      = 0xfb
	PrefixMisc    = 0xfc
	PrefixSIMD    = 0xfd
	PrefixThreads = 0xfe
)

// Value types, as one byte of the binary format.
const (
	I32       = 0x7f
	I64       = 0x7e
	F32       = 0x7d
	F64       = 0x7c
	V128      = 0x7b
	FuncRef   = 0x70
	ExternRef = 0x6f
)

// BlockEmpty is the block type of a block without parameters or results.
const BlockEmpty = -64

// An Instr is a decoded instruction.
type Instr struct {
	At     int    // the offset of the instruction in the code
	Prefix byte   // 0, or the prefix of a prefixed instruction
	Op     uint32 // the opcode, after the prefix for a prefixed instruction

	// Immediates, where the instruction has them.
	Index     uint32   // the first index: local, global, function, type, label, table, memory, segment, or tag
	Index2    uint32   // the second index: call_indirect's table, the source of a copy, the table or memory of an init
	Targets   []uint32 // br_table's labels, the default last
	BlockType int64    // block, loop, if, try_table: BlockEmpty, a value type as a negative number, or a type index
	Align     uint32   // a memory access's alignment exponent
	Offset    uint64   // a memory access's offset
	Lane      byte     // a lane index
	Value     uint64   // i32.const and i64.const (sign-extended), f32.const and f64.const (their bits)
	Bytes     []byte   // v128.const's and i8x16.shuffle's 16 bytes
	Types     []byte   // the value types of a select with types
}

// Is reports whether i is the unprefixed instruction op.
func (i *Instr) Is(op byte) bool { return i.Prefix == 0 && i.Op == uint32(op) }

// A Reader decodes the instructions of a function's code.
type Reader struct {
	b     []byte
	pos   int
	depth int
	err   error
	ended bool
}

// ErrShort reports code that ends inside an instruction.
var ErrShort = errors.New("the code ends inside an instruction")

// NewReader returns a Reader of code, a function body after its locals.
func NewReader(code []byte) *Reader { return &Reader{b: code} }

// Pos returns the offset of the next instruction.
func (r *Reader) Pos() int { return r.pos }

// Ended reports whether the reader has decoded the end of the function:
// the end instruction that closes its body.
func (r *Reader) Ended() bool { return r.ended }

// Next decodes the next instruction. After the end that closes the
// function's body, it fails if anything follows.
func (r *Reader) Next() (Instr, error) {
	if r.err != nil {
		return Instr{}, r.err
	}
	if r.ended {
		return Instr{}, r.fail(errors.New("the function's code goes on after its body ends"))
	}
	in := Instr{At: r.pos}
	op := r.byte()
	in.Op = uint32(op)
	switch op {
	case 0x00, 0x01, 0x0f, 0x1a, 0x1b, 0xd1, 0xd3, 0xd4: // unreachable, nop, return, drop, select, ref.is_null, ref.eq, ref.as_non_null
	case 0x02, 0x03, 0x04: // block, loop, if
		in.BlockType = r.blockType()
		r.depth++
	case 0x05: // else
	case 0x0b: // end
		if r.depth == 0 {
			r.ended = true
		}
		r.depth--
	case 0x0c, 0x0d, 0xd5, 0xd6: // br, br_if, br_on_null, br_on_non_null
		in.Index = r.u32()
	case 0x0e: // br_table
		n := r.count()
		for i := 0; i <= n && r.err == nil; i++ {
			in.Targets = append(in.Targets, r.u32())
		}
	case 0x10, 0x12, 0x14, 0x15, 0xd2: // call, return_call, call_ref, return_call_ref, ref.func
		in.Index = r.u32()
	case 0x11, 0x13: // call_indirect, return_call_indirect
		in.Index = r.u32()
		in.Index2 = r.u32()
	case 0x08: // throw
		in.Index = r.u32()
	case 0x0a: // throw_ref
	case 0x1f: // try_table
		in.BlockType = r.blockType()
		r.depth++
		n := r.count()
		for i := 0; i < n && r.err == nil; i++ {
			switch c := r.byte(); c {
			case 0x00, 0x01: // catch, catch_ref: a tag and a label
				r.u32()
				r.u32()
			case 0x02, 0x03: // catch_all, catch_all_ref: a label
				r.u32()
			default:
				r.fail(fmt.Errorf("try_table has a catch clause 0x%02x the decoder does not know", c))
			}
		}
	case 0x1c: // select with types
		n := r.count()
		for i := 0; i < n && r.err == nil; i++ {
			in.Types = append(in.Types, r.valueType())
		}
	case 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26: // local.get/set/tee, global.get/set, table.get/set
		in.Index = r.u32()
	case 0x3f, 0x40: // memory.size, memory.grow
		in.Index = r.u32()
	case 0x41:
		in.Value = uint64(r.s64(32))
	case 0x42:
		in.Value = uint64(r.s64(64))
	case 0x43:
		in.Value = uint64(le(r.bytes(4)))
	case 0x44:
		in.Value = le(r.bytes(8))
	case 0xd0: // ref.null
		r.heapType()
	case PrefixMisc:
		r.misc(&in)
	case PrefixSIMD:
		r.simd(&in)
	case PrefixThreads:
		r.threads(&in)
	case PrefixGC:
		in.Prefix, in.Op = op, r.u32()
		r.fail(fmt.Errorf("instruction 0xfb %d is a garbage collection instruction, which the decoder refuses", in.Op))
	default:
		switch {
		case op >= 0x28 && op <= 0x3e: // loads and stores
			r.memarg(&in)
		case op >= 0x45 && op <= 0xc4: // numeric instructions
		default:
			r.fail(fmt.Errorf("instruction 0x%02x is not one the decoder knows", op))
		}
	}
	if r.err != nil {
		return Instr{}, r.err
	}
	return in, nil
}

func (r *Reader) misc(in *Instr) {
	in.Prefix, in.Op = PrefixMisc, r.u32()
	switch op := in.Op; {
	case op <= 7, op >= 19 && op <= 22: // saturating conversions, wide arithmetic
	case op == 8, op == 12: // memory.init, table.init: a segment and a memory or table
		in.Index = r.u32()
		in.Index2 = r.u32()
	case op == 10, op == 14: // memory.copy, table.copy: the destination and the source
		in.Index = r.u32()
		in.Index2 = r.u32()
	case op == 9, op == 11, op == 13, op >= 15 && op <= 17: // data.drop, memory.fill, elem.drop, table.grow/size/fill
		in.Index = r.u32()
	default:
		r.fail(fmt.Errorf("instruction 0xfc %d is not one the decoder knows", op))
	}
}

func (r *Reader) threads(in *Instr) {
	in.Prefix, in.Op = PrefixThreads, r.u32()
	switch op := in.Op; {
	case op == 3: // atomic.fence
		r.byte()
	case op <= 2, op >= 0x10 && op <= 0x4e:
		r.memarg(in)
	default:
		r.fail(fmt.Errorf("instruction 0xfe %d is not one the decoder knows", op))
	}
}

func (r *Reader) simd(in *Instr) {
	in.Prefix, in.Op = PrefixSIMD, r.u32()
	switch op := in.Op; {
	case op <= 0x0b, op == 0x5c, op == 0x5d: // loads and stores
		r.memarg(in)
	case op == 0x0c, op == 0x0d: // v128.const, i8x16.shuffle
		in.Bytes = r.bytes(16)
	case op >= 0x15 && op <= 0x22: // extract_lane, replace_lane
		in.Lane = r.byte()
	case op >= 0x54 && op <= 0x5b: // load_lane, store_lane
		r.memarg(in)
		in.Lane = r.byte()
	default: // the other fixed-width and the relaxed instructions
		if !simdOp(op) {
			r.fail(fmt.Errorf("instruction 0xfd %d is not one the decoder knows", op))
		}
	}
}

// Reports whether op, past the ones with immediates, is a SIMD instruction:
// the fixed-width opcodes are all of 0x0e to 0xff but those the
// specification leaves unassigned, and the relaxed ones 0x100 to 0x113.
func simdOp(op uint32) bool {
	switch op {
	case 0x9a, 0xa2, 0xa5, 0xa6, 0xaf, 0xb0, 0xb2, 0xb3, 0xb4, 0xbb, 0xc2, 0xc5, 0xc6, 0xcf, 0xd0, 0xd2, 0xd3, 0xd4, 0xe2, 0xee:
		return false
	}
	return op >= 0x0e && op <= 0x113
}

func (r *Reader) fail(err error) error {
	if r.err == nil {
		r.err = err
	}
	r.pos = len(r.b)
	return r.err
}

func (r *Reader) byte() byte {
	if r.pos >= len(r.b) {
		r.fail(ErrShort)
		return 0
	}
	c := r.b[r.pos]
	r.pos++
	return c
}

func (r *Reader) bytes(n int) []byte {
	if r.pos+n > len(r.b) {
		r.fail(ErrShort)
		return nil
	}
	s := r.b[r.pos : r.pos+n]
	r.pos += n
	return s
}

func le(b []byte) uint64 {
	var v uint64
	for i := len(b) - 1; i >= 0; i-- {
		v = v<<8 | uint64(b[i])
	}
	return v
}

// u32 reads an unsigned LEB128 of at most 32 bits.
func (r *Reader) u32() uint32 {
	var v uint64
	for shift := uint(0); ; shift += 7 {
		c := r.byte()
		if r.err != nil {
			return 0
		}
		v |= uint64(c&0x7f) << shift
		if c&0x80 == 0 {
			if v > 1<<32-1 {
				r.fail(errors.New("an unsigned LEB128 is longer than 32 bits"))
				return 0
			}
			return uint32(v)
		}
		if shift >= 28 {
			r.fail(errors.New("an unsigned LEB128 is longer than 32 bits"))
			return 0
		}
	}
}

func (r *Reader) count() int { return int(r.u32()) }

// s64 reads a signed LEB128 of at most bits bits, sign-extended.
func (r *Reader) s64(bits uint) int64 {
	var v int64
	var shift uint
	for {
		c := r.byte()
		if r.err != nil {
			return 0
		}
		if shift < 64 {
			v |= int64(c&0x7f) << shift
		}
		shift += 7
		if c&0x80 == 0 {
			if shift < 64 && c&0x40 != 0 {
				v |= -1 << shift
			}
			return v
		}
		if shift >= (bits+6)/7*7 {
			r.fail(fmt.Errorf("a signed LEB128 is longer than %d bits", bits))
			return 0
		}
	}
}

func (r *Reader) valueType() byte {
	switch t := r.byte(); t {
	case I32, I64, F32, F64, V128, FuncRef, ExternRef:
		return t
	default:
		if r.err == nil {
			r.fail(fmt.Errorf("value type 0x%02x is not one the decoder knows", t))
		}
		return 0
	}
}

// heapType skips the heap type of ref.null: an abstract heap type (one
// byte) or a type index (a signed LEB128).
func (r *Reader) heapType() { r.s64(33) }

// blockType reads a block type: empty, a value type, or a type index.
func (r *Reader) blockType() int64 {
	t := r.s64(33)
	if t < 0 {
		switch byte(t & 0x7f) {
		case 0x40, I32, I64, F32, F64, V128, FuncRef, ExternRef:
		default:
			r.fail(fmt.Errorf("block type %d is not one the decoder knows", t))
		}
	}
	return t
}

// memarg reads a memory access's alignment (with a memory index when its
// bit 6 is set) and offset.
func (r *Reader) memarg(in *Instr) {
	a := r.u32()
	if a&0x40 != 0 {
		in.Index = r.u32()
		a &^= 0x40
	}
	in.Align = a
	in.Offset = r.u64()
}

// u64 reads an unsigned LEB128 of at most 64 bits.
func (r *Reader) u64() uint64 {
	var v uint64
	for shift := uint(0); ; shift += 7 {
		c := r.byte()
		if r.err != nil {
			return 0
		}
		if shift == 63 && c > 1 {
			r.fail(errors.New("an unsigned LEB128 is longer than 64 bits"))
			return 0
		}
		v |= uint64(c&0x7f) << shift
		if c&0x80 == 0 {
			return v
		}
	}
}
