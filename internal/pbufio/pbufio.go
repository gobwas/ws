// Package pbufio contains tools for pooling bufio.Reader and bufio.Writers.
package pbufio

import (
	"bufio"
	"io"

	pool "github.com/gobwas/ws/internal"
)

var (
	defaultWriterPool = NewWriterPool(256, 65536)
	defaultReaderPool = NewReaderPool(256, 65536)
)

// GetWriter returns bufio.Writer whose buffer has at least size bytes.
// Note that size could be ceiled to the next power of two.
// GetWriter is a wrapper around defaultWriterPool.Get().
func GetWriter(w io.Writer, size int) *bufio.Writer { return defaultWriterPool.Get(w, size) }

// PutWriter takes bufio.Writer for future reuse.
// It does not reuse bufio.Writer which underlying buffer size is not power of
// PutWriter is a wrapper around defaultWriterPool.Put().
func PutWriter(bw *bufio.Writer) { defaultWriterPool.Put(bw) }

// GetReader returns bufio.Reader whose buffer has at least size bytes. It returns
// its capacity for further pass to Put().
// Note that size could be ceiled to the next power of two.
// GetReader is a wrapper around defaultReaderPool.Get().
func GetReader(w io.Reader, size int) *bufio.Reader { return defaultReaderPool.Get(w, size) }

// PutReader takes bufio.Reader and its size for future reuse.
// It does not reuse bufio.Reader if size is not power of two or is out of pool
// min/max range.
// PutReader is a wrapper around defaultReaderPool.Put().
func PutReader(bw *bufio.Reader) { defaultReaderPool.Put(bw) }

// WriterPool contains logic of *bufio.Writer reuse with various size.
type WriterPool struct {
	pool *pool.Pool
}

// NewWriterPool creates new WriterPool that reuses writers which size is in
// logarithmic range [min, max].
func NewWriterPool(min, max int) *WriterPool {
	return &WriterPool{pool.New(min, max)}
}

// Get returns bufio.Writer whose buffer has at least size bytes.
func (wp *WriterPool) Get(w io.Writer, size int) *bufio.Writer {
	v, n := wp.pool.Get(size)
	if v != nil {
		bw := v.(*bufio.Writer)
		bw.Reset(w)
		return bw
	}
	return bufio.NewWriterSize(w, n)
}

// Put takes ownership of bufio.Writer for further reuse.
func (wp *WriterPool) Put(bw *bufio.Writer) {
	// Should reset even if we do Reset() inside Get().
	// This is done to prevent locking underlying io.Writer from GC.
	bw.Reset(nil)
	wp.pool.Put(bw, bw.Size())
}

// ReaderPool contains logic of *bufio.Reader reuse with various size.
type ReaderPool struct {
	pool *pool.Pool
}

// NewReaderPool creates new ReaderPool that reuses writers which size is in
// logarithmic range [min, max].
func NewReaderPool(min, max int) *ReaderPool {
	return &ReaderPool{pool.New(min, max)}
}

// Get returns bufio.Reader whose buffer has at least size bytes.
func (rp *ReaderPool) Get(r io.Reader, size int) *bufio.Reader {
	v, n := rp.pool.Get(size)
	if v != nil {
		br := v.(*bufio.Reader)
		br.Reset(r)
		return br
	}
	return bufio.NewReaderSize(r, n)
}

// Put takes ownership of bufio.Reader for further reuse.
func (rp *ReaderPool) Put(br *bufio.Reader) {
	// Should reset even if we do Reset() inside Get().
	// This is done to prevent locking underlying io.Reader from GC.
	br.Reset(nil)
	rp.pool.Put(br, br.Size())
}