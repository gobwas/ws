package wsflate

import (
	"io"
)

// Decompressor is an interface holding deflate decompression implementation.
type Decompressor interface {
	io.Reader
}

// ReadResetter is an optional interface that Decompressor can implement.
type ReadResetter interface {
	Reset(io.Reader)
}

// Reader implements decompression from an io.Reader object using Decompressor.
// Essentially Reader is a thin wrapper around Decompressor interface to meet
// PMCE specs.
//
// After all data has been written client should call Flush() method.
// If any error occurs after reading from Reader, all subsequent calls to
// Read() or Close() will return the error.
//
// Reader might be reused for different io.Reader objects after its Reset()
// method has been called.
type Reader struct {
	src  io.Reader
	ctor func(io.Reader) Decompressor
	d    Decompressor
	sr   suffixedReader
	err  error
}

// NewReader returns a new Reader.
func NewReader(r io.Reader, ctor func(io.Reader) Decompressor) *Reader {
	ret := &Reader{
		src:  r,
		ctor: ctor,
		sr: suffixedReader{
			suffix: compressionReadTail,
		},
	}
	ret.Reset(r)
	return ret
}

// Reset resets Reader to decompress data from src.
func (r *Reader) Reset(src io.Reader) {
	r.err = nil
	r.src = src
	r.sr.reset(src)

	if x, ok := r.d.(ReadResetter); ok {
		x.Reset(r.sr.iface())
	} else {
		r.d = r.ctor(r.sr.iface())
	}
}

// Read implements io.Reader.
func (r *Reader) Read(p []byte) (n int, err error) {
	if r.err != nil {
		return 0, r.err
	}
	return r.d.Read(p)
}

// Close closes Reader and a Decompressor instance used under the hood (if it
// implements io.Closer interface).
func (r *Reader) Close() error {
	if r.err != nil {
		return r.err
	}
	if c, ok := r.d.(io.Closer); ok {
		r.err = c.Close()
	}
	return r.err
}

// Err returns an error happened during any operation.
func (r *Reader) Err() error {
	return r.err
}
