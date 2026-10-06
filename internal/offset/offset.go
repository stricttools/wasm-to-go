package offset

import (
	"bufio"
	"io"
)

type Reader struct {
	r *bufio.Reader
	n uint64
}

func NewReader(r io.Reader) *Reader {
	var new Reader
	if b, ok := r.(*bufio.Reader); !ok {
		new.r = bufio.NewReader(r)
	} else {
		new.r = b
	}
	return &new
}

// NewReaderAt returns a Reader of r whose offset starts at n: a reader of
// part of a larger input, at the part's offset in it.
func NewReaderAt(r io.Reader, n uint64) *Reader {
	new := NewReader(r)
	new.n = n
	return new
}

func (r *Reader) Read(p []byte) (n int, err error) {
	n, err = r.r.Read(p)
	r.n += uint64(n)
	return
}

func (r *Reader) ReadByte() (b byte, err error) {
	b, err = r.r.ReadByte()
	if err == nil {
		r.n++
	}
	return
}

func (r *Reader) Offset() uint64 { return r.n }
