// Package locals splits a function's WebAssembly locals into their webs:
// the sets of definitions of a local that reach common uses. Each web can
// live in a variable of its own, so a local the code reuses for unrelated
// values (as compilers reuse registers) becomes several variables, each
// assigned only the values one group of uses reads.
//
// The webs come from SSA construction on the function's control flow
// graph (Braun et al., "Simple and Efficient Construction of Static Single
// Assignment Form", 2013): every read of a local finds the definitions
// reaching it, merging at a block with several predecessors through a phi,
// which is created only where a read needs it, so only where the local is
// live. A web is a definition with every phi and every other definition
// joined to it through a phi, so the definitions reaching any read are in
// one web, and a read's web holds every value the read can see.
package locals

import (
	"fmt"

	"github.com/stricttools/wasm-to-go/internal/wasmcode"
)

// A Split is the webs of a function's locals.
type Split struct {
	// Web is the web of each local.get, local.set, and local.tee of the
	// function, in the order of the code.
	Web []int
	// Local is each web's local.
	Local []int
	// Entry reports whether each web holds the local's value at the
	// function's entry: a parameter, or the zero of a declared local.
	Entry []bool
}

// The value a read or a definition of a local refers to, by number:
// a local's value at entry, a definition, a phi, or the undefined value
// unreachable code reads.
type value = int32

type block struct {
	preds  []int
	sealed bool
	// The phis created while the block was not sealed, by local, whose
	// operands are added when it is.
	incomplete map[int]value
}

type splitter struct {
	numLocals int
	blocks    []block
	// The current value of each local in each block, by block<<32|local.
	current map[uint64]value
	parent  []value // the union-find forest of values
	local   []int   // each value's local
	entry   []bool  // whether each value is a local's entry value
}

func (s *splitter) newValue(local int, entry bool) value {
	v := value(len(s.parent))
	s.parent = append(s.parent, v)
	s.local = append(s.local, local)
	s.entry = append(s.entry, entry)
	return v
}

func (s *splitter) find(v value) value {
	for s.parent[v] != v {
		s.parent[v] = s.parent[s.parent[v]]
		v = s.parent[v]
	}
	return v
}

func (s *splitter) union(a, b value) {
	a, b = s.find(a), s.find(b)
	if a != b {
		s.parent[b] = a
	}
}

func (s *splitter) newBlock(sealed bool) int {
	s.blocks = append(s.blocks, block{sealed: sealed})
	return len(s.blocks) - 1
}

func (s *splitter) edge(from, to int) {
	s.blocks[to].preds = append(s.blocks[to].preds, from)
}

func key(b, local int) uint64 { return uint64(b)<<32 | uint64(local) }

func (s *splitter) write(b, local int, v value) { s.current[key(b, local)] = v }

func (s *splitter) read(b, local int) value {
	if v, ok := s.current[key(b, local)]; ok {
		return v
	}
	return s.readRecursive(b, local)
}

func (s *splitter) readRecursive(b, local int) value {
	blk := &s.blocks[b]
	var v value
	switch {
	case !blk.sealed:
		v = s.newValue(local, false)
		if blk.incomplete == nil {
			blk.incomplete = map[int]value{}
		}
		blk.incomplete[local] = v
	case len(blk.preds) == 0:
		// The entry block reads the value at entry; a block nothing
		// branches to is unreachable, and reads a value of its own.
		v = s.newValue(local, b == 0)
	case len(blk.preds) == 1:
		v = s.read(blk.preds[0], local)
	default:
		v = s.newValue(local, false)
		s.write(b, local, v)
		s.addOperands(b, local, v)
	}
	s.write(b, local, v)
	return v
}

func (s *splitter) addOperands(b, local int, phi value) {
	for _, p := range s.blocks[b].preds {
		s.union(phi, s.read(p, local))
	}
}

func (s *splitter) seal(b int) {
	blk := &s.blocks[b]
	if blk.sealed {
		return
	}
	blk.sealed = true
	for local, phi := range blk.incomplete {
		s.addOperands(b, local, phi)
	}
	blk.incomplete = nil
}

// A frame of the control stack.
type frame struct {
	loop   bool
	header int // a loop's first block, the target of branches to it
	end    int // the block after the construct's end
	ifFrom int // an if's block before the if, for its else, or -1
	isIf   bool
}

// Analyze splits the locals of the function whose code (its body after the
// declarations of its locals) is code, with numLocals locals, parameters
// included. It refuses code that uses exception handling.
func Analyze(code []byte, numLocals int) (*Split, error) {
	s := &splitter{numLocals: numLocals, current: map[uint64]value{}}
	cur := s.newBlock(true) // the entry block
	exit := s.newBlock(false)
	stack := []frame{{end: exit, ifFrom: -1}}
	target := func(depth uint32) (int, error) {
		if int(depth) >= len(stack) {
			return 0, fmt.Errorf("a branch to label %d is outside the function's %d", depth, len(stack))
		}
		f := &stack[len(stack)-1-int(depth)]
		if f.loop {
			return f.header, nil
		}
		return f.end, nil
	}
	var refs []value // the value of each local instruction, in order
	r := wasmcode.NewReader(code)
	for !r.Ended() {
		in, err := r.Next()
		if err != nil {
			return nil, err
		}
		switch {
		case in.Prefix != 0:
		case in.Is(0x02): // block
			stack = append(stack, frame{end: s.newBlock(false), ifFrom: -1})
		case in.Is(0x03): // loop
			header := s.newBlock(false)
			s.edge(cur, header)
			cur = header
			stack = append(stack, frame{loop: true, header: header, end: s.newBlock(false), ifFrom: -1})
		case in.Is(0x04): // if
			then := s.newBlock(true)
			s.edge(cur, then)
			stack = append(stack, frame{end: s.newBlock(false), ifFrom: cur, isIf: true})
			cur = then
		case in.Is(0x05): // else
			f := &stack[len(stack)-1]
			s.edge(cur, f.end)
			els := s.newBlock(true)
			s.edge(f.ifFrom, els)
			f.ifFrom = -1
			cur = els
		case in.Is(0x0b): // end
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			s.edge(cur, f.end)
			if f.isIf && f.ifFrom >= 0 { // an if without else
				s.edge(f.ifFrom, f.end)
			}
			if f.loop {
				s.seal(f.header)
			}
			s.seal(f.end)
			cur = f.end
		case in.Is(0x0c): // br
			t, err := target(in.Index)
			if err != nil {
				return nil, err
			}
			s.edge(cur, t)
			cur = s.newBlock(true)
		case in.Is(0x0d): // br_if
			t, err := target(in.Index)
			if err != nil {
				return nil, err
			}
			s.edge(cur, t)
			next := s.newBlock(true)
			s.edge(cur, next)
			cur = next
		case in.Is(0x0e): // br_table
			for _, d := range in.Targets {
				t, err := target(d)
				if err != nil {
					return nil, err
				}
				s.edge(cur, t)
			}
			cur = s.newBlock(true)
		case in.Is(0x0f), in.Is(0x00), in.Is(0x12), in.Is(0x13), in.Is(0x15): // return, unreachable, tail calls
			s.edge(cur, exit)
			cur = s.newBlock(true)
		case in.Is(0x08), in.Is(0x0a), in.Is(0x1f), in.Is(0xd5), in.Is(0xd6):
			return nil, fmt.Errorf("instruction 0x%02x: the analysis of locals does not handle exception handling or typed function references", in.Op)
		case in.Is(0x20): // local.get
			if int(in.Index) >= numLocals {
				return nil, fmt.Errorf("local.get %d of a function with %d locals", in.Index, numLocals)
			}
			refs = append(refs, s.read(cur, int(in.Index)))
		case in.Is(0x21), in.Is(0x22): // local.set, local.tee
			if int(in.Index) >= numLocals {
				return nil, fmt.Errorf("local.set %d of a function with %d locals", in.Index, numLocals)
			}
			v := s.newValue(int(in.Index), false)
			s.write(cur, int(in.Index), v)
			refs = append(refs, v)
		}
	}
	if r.Pos() != len(code) {
		return nil, fmt.Errorf("the function's code goes on after its body ends")
	}
	// Number the webs of the values the code refers to, in the order
	// the code first refers to them.
	split := &Split{Web: make([]int, len(refs))}
	webs := map[value]int{}
	for i, v := range refs {
		root := s.find(v)
		w, ok := webs[root]
		if !ok {
			w = len(split.Local)
			webs[root] = w
			split.Local = append(split.Local, s.local[root])
			split.Entry = append(split.Entry, false)
		}
		split.Web[i] = w
	}
	// A web holds the entry value if any value joined to it is one.
	for v := range s.parent {
		if s.entry[v] {
			if w, ok := webs[s.find(value(v))]; ok {
				split.Entry[w] = true
			}
		}
	}
	return split, nil
}
