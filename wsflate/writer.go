package wsflate

import (
	"fmt"
	"io"
)

var (
	compressionTail = [4]byte{
		0, 0, 0xff, 0xff,
	}
	compressionReadTail = [9]byte{
		0, 0, 0xff, 0xff,
		1,
		0, 0, 0xff, 0xff,
	}
)

// Compressor is an interface holding deflate compression implementation.
type Compressor interface {
	io.Writer
	Flush() error
}

// WriteResetter is an optional interface that Compressor can implement.
type WriteResetter interface {
	Reset(io.Writer)
}

// Writer implements compression for an io.Writer object using Compressor.
// Essentially Writer is a thin wrapper around Compressor interface to meet
// PMCE specs.
//
// After all data has been written client should call Flush() method.
// If any error occurs after writing to or flushing a Writer, all subsequent
// calls to Write(), Flush() or Close() will return the error.
//
// Writer might be reused for different io.Writer objects after its Reset()
// method has been called.
type Writer struct {
	// NOTE: Writer uses compressor constructor function instead of field to
	// reach these goals:
	// 	1. To shrink Compressor interface and make it easier to be implemented.
	//	2. If used as a field (and argument to the NewWriter()), Compressor object
	//	will probably be initialized twice - first time to pass into Writer, and
	//	second time during Writer initialization (which does Reset() internally).
	// 	3. To get rid of wrappers if Reset() would be a part of	Compressor.
	// 	E.g. non conformant implementations would have to provide it somehow,
	// 	probably making a wrapper with the same constructor function.
	// 	4. To make Reader and Writer API the same. That is, there is no Reset()
	// 	method for flate.Reader already, so we need to provide it as a wrapper
	// 	(see point #3), or drop the Reader.Reset() method.
	dest io.Writer
	ctor func(io.Writer) Compressor
	c    Compressor
	cbuf cbuf
	err  error
}

// NewWriter returns a new Writer.
func NewWriter(w io.Writer, ctor func(io.Writer) Compressor) *Writer {
	// NOTE: NewWriter() is chosen against structure with exported fields here
	// due its Reset() method, which in case of structure, would change
	// exported field.
	ret := &Writer{
		dest: w,
		ctor: ctor,
	}
	ret.Reset(w)
	return ret
}

// Reset resets Writer to compress data into dest.
// Any not flushed data will be lost.
func (w *Writer) Reset(dest io.Writer) {
	w.err = nil
	w.cbuf.reset(dest)
	if x, ok := w.c.(WriteResetter); ok {
		x.Reset(&w.cbuf)
	} else {
		w.c = w.ctor(&w.cbuf)
	}
}

// Write implements io.Writer.
func (w *Writer) Write(p []byte) (n int, err error) {
	if w.err != nil {
		return 0, w.err
	}
	n, w.err = w.c.Write(p)
	return n, w.err
}

// Flush writes any pending data into w.Dest.
func (w *Writer) Flush() error {
	if w.err != nil {
		return w.err
	}
	w.err = w.c.Flush()
	w.checkTail()
	return w.err
}

// Close closes Writer and a Compressor instance used under the hood (if it
// implements io.Closer interface).
func (w *Writer) Close() error {
	if w.err != nil {
		return w.err
	}
	if c, ok := w.c.(io.Closer); ok {
		w.err = c.Close()
	}
	w.checkTail()
	return w.err
}

// Err returns an error happened during any operation.
func (w *Writer) Err() error {
	return w.err
}

func (w *Writer) checkTail() {
	if w.err == nil && w.cbuf.buf != compressionTail {
		w.err = fmt.Errorf(
			"wsflate: bad compressor: unexpected stream tail: %#x vs %#x",
			w.cbuf.buf, compressionTail,
		)
	}
}
