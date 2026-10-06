package wasmcode

import (
	"slices"
	"testing"
)

// Each instruction decodes with its immediates, and the reader ends at the
// end that closes the body.
func TestReader(t *testing.T) {
	code := []byte{
		0x02, 0x40, // block
		0x20, 0x03, // local.get 3
		0x0e, 0x02, 0x00, 0x01, 0x00, // br_table 0 1 0
		0x0b,       // end
		0x41, 0x7f, // i32.const -1
		0x42, 0x80, 0x01, // i64.const 128
		0x43, 0x00, 0x00, 0xc0, 0x7f, // f32.const nan
		0x28, 0x02, 0x08, // i32.load align=2 offset=8
		0x29, 0x43, 0x01, 0x80, 0x80, 0x04, // i64.load of memory 1, offset 65536
		0x11, 0x05, 0x01, // call_indirect type 5, table 1
		0xfc, 0x0a, 0x00, 0x00, // memory.copy
		0xfd, 0x0c, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, // v128.const
		0xfd, 0x15, 0x07, // i8x16.extract_lane_s 7
		0xfd, 0x56, 0x02, 0x10, 0x03, // v128.load32_lane offset 16 lane 3
		0xfd, 0xe4, 0x01, // f32x4.add
		0xfd, 0x80, 0x02, // i8x16.relaxed_swizzle
		0xfe, 0x03, 0x00, // atomic.fence
		0x1c, 0x01, 0x7b, // select (result v128)
		0x0b, // end of the body
	}
	var got []Instr
	r := NewReader(code)
	for !r.Ended() {
		in, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, in)
	}
	check := func(i int, ok bool, what string) {
		t.Helper()
		if !ok {
			t.Errorf("instruction %d: %s: got %+v", i, what, got[i])
		}
	}
	if len(got) != 19 {
		t.Fatalf("got %d instructions, want 19", len(got))
	}
	check(0, got[0].Is(0x02) && got[0].BlockType == BlockEmpty, "block")
	check(1, got[1].Is(0x20) && got[1].Index == 3, "local.get 3")
	check(2, got[2].Is(0x0e) && slices.Equal(got[2].Targets, []uint32{0, 1, 0}), "br_table")
	check(4, got[4].Is(0x41) && int64(got[4].Value) == -1, "i32.const -1")
	check(5, got[5].Is(0x42) && got[5].Value == 128, "i64.const 128")
	check(6, got[6].Is(0x43) && got[6].Value == 0x7fc00000, "f32.const")
	check(7, got[7].Is(0x28) && got[7].Align == 2 && got[7].Offset == 8, "i32.load")
	check(8, got[8].Is(0x29) && got[8].Align == 3 && got[8].Index == 1 && got[8].Offset == 65536, "i64.load of memory 1")
	check(9, got[9].Is(0x11) && got[9].Index == 5 && got[9].Index2 == 1, "call_indirect")
	check(10, got[10].Prefix == PrefixMisc && got[10].Op == 10, "memory.copy")
	check(11, got[11].Prefix == PrefixSIMD && got[11].Op == 0x0c && got[11].Bytes[15] == 16, "v128.const")
	check(12, got[12].Op == 0x15 && got[12].Lane == 7, "extract_lane")
	check(13, got[13].Op == 0x56 && got[13].Offset == 16 && got[13].Lane == 3, "load32_lane")
	check(14, got[14].Op == 0xe4, "f32x4.add")
	check(15, got[15].Op == 0x100, "relaxed_swizzle")
	check(16, got[16].Prefix == PrefixThreads && got[16].Op == 3, "atomic.fence")
	check(17, got[17].Is(0x1c) && slices.Equal(got[17].Types, []byte{V128}), "select")
	if r.Pos() != len(code) {
		t.Errorf("the reader stopped at %d of %d", r.Pos(), len(code))
	}
}

// The reader refuses what it does not decode and malformed code.
func TestReaderRefuses(t *testing.T) {
	for _, code := range [][]byte{
		{0xfb, 0x00, 0x0b},       // a garbage collection instruction
		{0x27, 0x0b},             // an unassigned opcode
		{0xfd, 0x9a, 0x01, 0x0b}, // an unassigned SIMD opcode
		{0xfd, 0x94, 0x02, 0x0b}, // past the relaxed SIMD opcodes
		{0x20},                   // a short instruction
		{0x41, 0x80, 0x80, 0x80, 0x80, 0x80, 0x00, 0x0b}, // an i32.const longer than 32 bits
		{0x0b, 0x01}, // code after the end
	} {
		r := NewReader(code)
		var err error
		for err == nil && r.Pos() < len(code) {
			_, err = r.Next()
		}
		if err == nil {
			t.Errorf("% x decoded without an error", code)
		}
	}
}
