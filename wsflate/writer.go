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
// After all data of a message has been written client should call Flush(),
// which completes the message as PMCE requires. Close() flushes the Writer,
// if it has not been flushed yet, and closes the Compressor if it implements
// io.Closer. Whatever the Compressor writes while being closed is ignored.
//
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
	dest    io.Writer
	ctor    func(io.Writer) Compressor
	c       Compressor
	cbuf    cbuf
	err     error
	flushed bool
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
	w.flushed = false
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
	w.flushed = false
	n, w.err = w.c.Write(p)
	return n, w.err
}

// Flush writes any pending data into w.Dest and completes the message.
//
// That is, it makes the Compressor emit an empty stored DEFLATE block (sync
// flush) and strips its trailing 0x00 0x00 0xff 0xff from the output, as
// required by RFC 7692 section 7.2.1. The Compressor must end its Flush()
// output with those four bytes; otherwise Flush() returns an error.
func (w *Writer) Flush() error {
	if w.err != nil {
		return w.err
	}
	w.err = w.c.Flush()
	w.checkTail()
	w.flushed = w.err == nil
	return w.err
}

// Close closes the Writer by flushing any unwritten data to the underlying
// io.Writer (see Flush()) and closing the Compressor if it implements
// io.Closer. It does not close the underlying io.Writer.
//
// The bytes the Compressor writes while being closed (usually a final DEFLATE
// block) are not part of a PMCE message and are discarded.
func (w *Writer) Close() error {
	if w.err != nil {
		return w.err
	}
	if !w.flushed {
		if err := w.Flush(); err != nil {
			return err
		}
	}
	if c, ok := w.c.(io.Closer); ok {
		// Keep the sync-flush marker held in cbuf while closing the
		// compressor, so that whatever it writes can not push any of those
		// bytes to the destination.
		dst := w.cbuf.dst
		w.cbuf.dst = io.Discard
		w.err = c.Close()
		w.cbuf.dst = dst
	}
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
