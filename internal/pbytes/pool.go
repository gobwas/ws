// Package pbytes contains tools for pooling byte pool.
// Note that by default it reuse slices with capacity from 128 to 65536 bytes.
package pbytes

import pool "github.com/gobwas/ws/internal"

// defaultPool is used by pacakge level functions.
var defaultPool = New(128, 65536)

// GetLen returns probably reused slice of bytes with at least capacity of n
// and exactly len of n.
// GetLen is a wrapper around defaultPool.GetLen().
func GetLen(n int) []byte { return defaultPool.GetLen(n) }

// Put returns given slice to reuse pool.
// Put is a wrapper around defaultPool.Put().
func Put(p []byte) { defaultPool.Put(p) }

// Pool contains logic of reusing byte slices of various size.
type Pool struct {
	pool *pool.Pool
}

// New creates new Pool that reuses slices which size is in logarithmic range
// [min, max].
//
// Note that it is a shortcut for Custom() constructor with Options provided by
// pool.WithLogSizeMapping() and pool.WithLogSizeRange(min, max) calls.
func New(min, max int) *Pool {
	return &Pool{pool.New(min, max)}
}

// Get returns probably reused slice of bytes with at least capacity of c and
// exactly len of n.
func (p *Pool) Get(n, c int) []byte {
	if n > c {
		panic("requested length is greater than capacity")
	}

	v, x := p.pool.Get(c)
	if v != nil {
		bts := v.([]byte)
		bts = bts[:n]
		return bts
	}

	return make([]byte, n, x)
}

// Put returns given slice to reuse pool.
// It does not reuse bytes whose size is not power of two or is out of pool
// min/max range.
func (p *Pool) Put(bts []byte) {
	p.pool.Put(bts, cap(bts))
}

// GetLen returns probably reused slice of bytes with at least capacity of n
// and exactly len of n.
func (p *Pool) GetLen(n int) []byte {
	return p.Get(n, n)
}
