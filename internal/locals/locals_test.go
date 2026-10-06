package locals

import (
	"slices"
	"testing"
)

// Opcodes of the test functions.
const (
	blockOp  = 0x02
	loop     = 0x03
	ifOp     = 0x04
	elseOp   = 0x05
	end      = 0x0b
	br       = 0x0c
	brIf     = 0x0d
	ret      = 0x0f
	drop     = 0x1a
	get      = 0x20
	set      = 0x21
	tee      = 0x22
	i32const = 0x41
	empty    = 0x40
)

func TestAnalyze(t *testing.T) {
	for _, tt := range []struct {
		name      string
		code      []byte
		numLocals int
		web       []int  // the web of each local instruction
		local     []int  // each web's local
		entry     []bool // whether each web holds the entry value
	}{{
		// Two unrelated values of one local are two webs.
		name: "straight line",
		code: []byte{
			i32const, 1, set, 0, get, 0, drop,
			i32const, 2, set, 0, get, 0, drop, end},
		numLocals: 1,
		web:       []int{0, 0, 1, 1},
		local:     []int{0, 0},
		entry:     []bool{false, false},
	}, {
		// A read before any definition reads the entry value.
		name: "entry",
		code: []byte{
			get, 0, drop, i32const, 1, set, 0, get, 0, drop, end},
		numLocals: 1,
		web:       []int{0, 1, 1},
		local:     []int{0, 0},
		entry:     []bool{true, false},
	}, {
		// Definitions on both arms of an if reach the read after it.
		name: "if else",
		code: []byte{
			get, 1, ifOp, empty,
			i32const, 1, set, 0,
			elseOp,
			i32const, 2, set, 0,
			end,
			get, 0, drop, end},
		numLocals: 2,
		web:       []int{0, 1, 1, 1},
		local:     []int{1, 0},
		entry:     []bool{true, false},
	}, {
		// Without an else, the entry value reaches the read too.
		name: "if",
		code: []byte{
			get, 1, ifOp, empty,
			i32const, 1, set, 0,
			end,
			get, 0, drop, end},
		numLocals: 2,
		web:       []int{0, 1, 1},
		local:     []int{1, 0},
		entry:     []bool{true, true},
	}, {
		// A value carried around a loop joins the entry value.
		name: "loop carried",
		code: []byte{
			loop, empty,
			get, 0, i32const, 1, 0x6a, set, 0,
			get, 0, brIf, 0,
			end, end},
		numLocals: 1,
		web:       []int{0, 0, 0},
		local:     []int{0},
		entry:     []bool{true},
	}, {
		// A value defined before every read in a loop's body does not
		// join the entry value, nor the value after the loop.
		name: "loop temporary",
		code: []byte{
			loop, empty,
			i32const, 1, set, 0,
			get, 0, brIf, 0,
			end,
			i32const, 2, set, 0, get, 0, drop, end},
		numLocals: 1,
		web:       []int{0, 0, 1, 1},
		local:     []int{0, 0},
		entry:     []bool{false, false},
	}, {
		// A branch out of a block carries its definition to the block's
		// end, where it joins the fallthrough's.
		name: "branch",
		code: []byte{
			blockOp, empty,
			i32const, 1, set, 0,
			get, 1, brIf, 0,
			i32const, 2, set, 0,
			end,
			get, 0, drop, end},
		numLocals: 2,
		web:       []int{0, 1, 0, 0},
		local:     []int{0, 1},
		entry:     []bool{false, true},
	}, {
		// Code after a return reaches nothing.
		name: "unreachable",
		code: []byte{
			i32const, 1, set, 0, get, 0, drop,
			ret,
			get, 0, drop, end},
		numLocals: 1,
		web:       []int{0, 0, 1},
		local:     []int{0, 0},
		entry:     []bool{false, false},
	}, {
		// A tee defines the value it leaves on the stack.
		name: "tee",
		code: []byte{
			i32const, 1, tee, 0, drop, get, 0, drop, end},
		numLocals: 1,
		web:       []int{0, 0},
		local:     []int{0},
		entry:     []bool{false},
	}} {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Analyze(tt.code, tt.numLocals)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(s.Web, tt.web) || !slices.Equal(s.Local, tt.local) || !slices.Equal(s.Entry, tt.entry) {
				t.Errorf("got webs %v, locals %v, entry %v; want %v, %v, %v", s.Web, s.Local, s.Entry, tt.web, tt.local, tt.entry)
			}
		})
	}
}

// Analyze refuses what it does not decode, and a body that goes on after
// its end.
func TestAnalyzeRefuses(t *testing.T) {
	for _, code := range [][]byte{
		{0x1f, empty, 0, end, end}, // try_table
		{get, 3, drop, end},        // a local out of range
		{end, 0x01},                // code after the end
		{get},                      // a short instruction
	} {
		if _, err := Analyze(code, 1); err == nil {
			t.Errorf("Analyze(% x) = nil error", code)
		}
	}
}
