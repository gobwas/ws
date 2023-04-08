package wsflate

import (
	"io"
)

// cbuf is a tiny proxy-buffer that writes all but 4 last bytes to the
// destination.
type cbuf struct {
	buf [4]byte
	n   int
	dst io.Writer
	err error
}

// Write implements io.Writer interface.
func (c *cbuf) Write(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	head, tail := c.split(p)
	n := c.n + len(tail)
	if n > len(c.buf) {
		x := n - len(c.buf)
		c.flush(c.buf[:x])
		copy(c.buf[:], c.buf[x:])
		c.n -= x
	}
	if len(head) > 0 {
		c.flush(head)
	}
	copy(c.buf[c.n:], tail)
	c.n = min(c.n+len(tail), len(c.buf))
	return len(p), c.err
}

func (c *cbuf) flush(p []byte) {
	if c.err == nil {
		_, c.err = c.dst.Write(p)
	}
}

func (c *cbuf) split(p []byte) (head, tail []byte) {
	if n := len(p); n > len(c.buf) {
		x := n - len(c.buf)
		head = p[:x]
		tail = p[x:]
		return head, tail
	}
	return nil, p
}

func (c *cbuf) reset(dst io.Writer) {
	c.n = 0
	c.err = nil
	c.buf = [4]byte{0, 0, 0, 0}
	c.dst = dst
}

type suffixedReader struct {
	r      io.Reader
	pos    int // position in the suffix.
	suffix [9]byte

	rx struct{ io.Reader }
}

func (r *suffixedReader) iface() io.Reader {
	if _, ok := r.r.(io.ByteReader); ok {
		// If source io.Reader implements io.ByteReader, return full set of
		// methods from suffixedReader struct (Read() and ReadByte()).
		// This actually is an optimization needed for those Decompressor
		// implementations (such as default flate.Reader) which do check if
		// given source is already "buffered" by checking if source implements
		// io.ByteReader. So without this checks we will always result in
		// double-buffering for default decompressors.
		return r
	}
	// Source io.Reader doesn't support io.ByteReader, so we should cut off the
	// ReadByte() method from suffixedReader struct. We use r.srx field to
	// avoid allocations.
	r.rx.Reader = r
	return &r.rx
}

func (r *suffixedReader) Read(p []byte) (n int, err error) {
	if r.r != nil {
		n, err = r.r.Read(p)
		if err == io.EOF {
			err = nil
			r.r = nil
		}
		return n, err
	}
	if r.pos >= len(r.suffix) {
		return 0, io.EOF
	}
	n = copy(p, r.suffix[r.pos:])
	r.pos += n
	return n, nil
}

func (r *suffixedReader) ReadByte() (b byte, err error) {
	if r.r != nil {
		br, ok := r.r.(io.ByteReader)
		if !ok {
			panic("wsflate: internal error: incorrect use of suffixedReader")
		}
		b, err = br.ReadByte()
		if err == io.EOF {
			err = nil
			r.r = nil
		}
		return b, err
	}
	if r.pos >= len(r.suffix) {
		return 0, io.EOF
	}
	b = r.suffix[r.pos]
	r.pos++
	return b, nil
}

func (r *suffixedReader) reset(src io.Reader) {
	r.r = src
	r.pos = 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
