// Package pool reuses objects that are distinguishable by size: byte slices,
// bufio readers and writers and the like. Every power of two in a range has
// its own sync.Pool, and a request is served from the class of the least
// power of two not less than the requested size.
//
// It is the part of github.com/gobwas/pool that ws uses, with generics
// instead of interface{}.
package pool

import (
	"bufio"
	"math/bits"
	"sync"
)

// Pool reuses objects of type T by size class.
type Pool[T any] struct {
	pools map[int]*sync.Pool
}

// New creates a Pool with a size class for every power of two in [lo, hi].
// lo is rounded up to a power of two first; a lo of zero means one.
func New[T any](lo, hi int) *Pool[T] {
	p := &Pool[T]{
		pools: make(map[int]*sync.Pool),
	}
	for n := ceilToPowerOfTwo(max(lo, 1)); n <= hi; n <<= 1 {
		p.pools[n] = new(sync.Pool)
	}
	return p
}

// Get returns a pooled object whose size class is the least power of two not
// less than size, and that class size, to be passed to Put() later and to be
// used as the allocation size when there is nothing to reuse.
//
// The object is the zero value of T when the class is empty or when size is
// out of the pool's range; the returned size is then size itself.
func (p *Pool[T]) Get(size int) (T, int) {
	var zero T
	n := ceilToPowerOfTwo(size)
	pool := p.pools[n]
	if pool == nil {
		return zero, size
	}
	x, _ := pool.Get().(T)
	return x, n
}

// Put stores x for reuse under the size class of size. It drops x if size is
// not one of the pool's classes.
func (p *Pool[T]) Put(x T, size int) {
	if pool := p.pools[size]; pool != nil {
		pool.Put(x)
	}
}

var bytesPool = New[*[]byte](128, 65536)

// GetBytes returns a pointer to a byte slice of length n, reused if possible.
//
// The pointer, not the slice, is what the pool holds: a slice stored in a
// sync.Pool is boxed into an interface, which allocates its header on every
// Put. With a pointer the warm path does not allocate at all.
func GetBytes(n int) *[]byte {
	bp, c := bytesPool.Get(n)
	if bp == nil {
		b := make([]byte, n, c)
		return &b
	}
	*bp = (*bp)[:n]
	return bp
}

// PutBytes stores bp for reuse by GetBytes(). Slices whose capacity is not a
// power of two in the pool's range are dropped.
func PutBytes(bp *[]byte) {
	bytesPool.Put(bp, cap(*bp))
}

var (
	readers = New[*bufio.Reader](256, 65536)
	writers = New[*bufio.Writer](256, 65536)
)

// GetReader returns a bufio.Reader whose buffer has at least size bytes,
// reused if possible. It reads from nothing until the caller calls Reset().
func GetReader(size int) *bufio.Reader {
	br, n := readers.Get(size)
	if br == nil {
		return bufio.NewReaderSize(nil, n)
	}
	return br
}

// PutReader stores br for reuse by GetReader(). It resets br so that the
// pooled reader does not keep its source alive.
func PutReader(br *bufio.Reader) {
	br.Reset(nil)
	readers.Put(br, br.Size())
}

// GetWriter returns a bufio.Writer whose buffer has at least size bytes,
// reused if possible. It writes to nothing until the caller calls Reset().
func GetWriter(size int) *bufio.Writer {
	bw, n := writers.Get(size)
	if bw == nil {
		return bufio.NewWriterSize(nil, n)
	}
	return bw
}

// PutWriter stores bw for reuse by GetWriter(). It resets bw so that the
// pooled writer does not keep its destination alive.
func PutWriter(bw *bufio.Writer) {
	bw.Reset(nil)
	writers.Put(bw, bw.Size())
}

// ceilToPowerOfTwo returns the least power of two not less than n. For n of
// two or less it returns n itself.
func ceilToPowerOfTwo(n int) int {
	if n <= 2 {
		return n
	}
	return 1 << bits.Len(uint(n-1))
}
