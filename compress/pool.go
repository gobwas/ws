package compress

import (
	"compress/flate"
	"io"
	"sync"
)

type FlateWriterPool struct {
	level int
	pool  *sync.Pool
}

func NewFlateWriterPool(level int) *FlateWriterPool {
	return &FlateWriterPool{level: level, pool: &sync.Pool{}}
}

func (wp *FlateWriterPool) Get(w io.Writer) *flate.Writer {
	v := wp.pool.Get()
	if v != nil {
		fw := v.(*flate.Writer)
		fw.Reset(w)
		return fw
	}

	fw, err := flate.NewWriter(w, wp.level)
	if err != nil {
		panic(err)
	}

	return fw
}

func (wp *FlateWriterPool) Put(fw *flate.Writer) {
	// Should reset even if we do Reset() inside Get().
	// This is done to prevent locking underlying io.Writer from GC.
	fw.Reset(nil)
	wp.pool.Put(fw)
}

type FlateReaderPool struct {
	pool *sync.Pool
}

func NewFlateReaderPool() *FlateReaderPool {
	return &FlateReaderPool{pool: &sync.Pool{}}
}

func (rp *FlateReaderPool) Get(r io.Reader) io.ReadCloser {
	v := rp.pool.Get()
	if v != nil {
		v.(flate.Resetter).Reset(r, nil)
		return v.(io.ReadCloser)
	}

	return flate.NewReader(r)
}

func (rp *FlateReaderPool) Put(r io.ReadCloser) {
	// Should reset even if we do Reset() inside Get().
	// This is done to prevent locking underlying io.Reader from GC.
	r.(flate.Resetter).Reset(nil, nil)
	rp.pool.Put(r)
}
