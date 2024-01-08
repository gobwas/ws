package pool

import (
	"sync"
)

var defaultPool = New(128, 65536)

// Get pulls object whose generic size is at least of given size. It also
// returns a real size of x for further pass to Put(). It returns -1 as real
// size for nil x. Size >-1 does not mean that x is non-nil, so checks must be
// done.
//
// Note that size could be ceiled to the next power of two.
//
// Get is a wrapper around defaultPool.Get().
func Get(size int) (interface{}, int) { return defaultPool.Get(size) }

// Put takes x and its size for future reuse.
// Put is a wrapper around defaultPool.Put().
func Put(x interface{}, size int) { defaultPool.Put(x, size) }

// Pool contains logic of reusing objects distinguishable by size in generic
// way.
type Pool struct {
	pool map[int]*sync.Pool
	size func(int) int
}

// New creates new Pool that reuses objects which size is in logarithmic range
// [min, max].
//
// Note that it is a shortcut for Custom() constructor with Options provided by
// WithLogSizeMapping() and WithLogSizeRange(min, max) calls.
func New(min, max int) *Pool {
	return Custom(
		WithLogSizeMapping(),
		WithLogSizeRange(min, max),
	)
}

// Custom creates new Pool with given options.
func Custom(opts ...Option) *Pool {
	p := &Pool{
		pool: make(map[int]*sync.Pool),
		size: identity,
	}

	c := (*poolConfig)(p)
	for _, opt := range opts {
		opt(c)
	}

	return p
}

// Get pulls object whose generic size is at least of given size.
// It also returns a real size of x for further pass to Put() even if x is nil.
// Note that size could be ceiled to the next power of two.
func (p *Pool) Get(size int) (interface{}, int) {
	n := p.size(size)
	if pool := p.pool[n]; pool != nil {
		return pool.Get(), n
	}
	return nil, size
}

// Put takes x and its size for future reuse.
func (p *Pool) Put(x interface{}, size int) {
	if pool := p.pool[size]; pool != nil {
		pool.Put(x)
	}
}

type poolConfig Pool

// AddSize adds size n to the map.
func (p *poolConfig) AddSize(n int) {
	p.pool[n] = new(sync.Pool)
}

// SetSizeMapping sets up incoming size mapping function.
func (p *poolConfig) SetSizeMapping(size func(int) int) {
	p.size = size
}

// Option configures pool.
type Option func(Config)

// Config describes generic pool configuration.
type Config interface {
	AddSize(n int)
	SetSizeMapping(func(int) int)
}

// WithSizeLogRange returns an Option that will add logarithmic range of
// pooling sizes containing [min, max] values.
func WithLogSizeRange(min, max int) Option {
	return func(c Config) {
		logarithmicRange(min, max, func(n int) {
			c.AddSize(n)
		})
	}
}

func WithSizeMapping(sz func(int) int) Option {
	return func(c Config) {
		c.SetSizeMapping(sz)
	}
}

func WithLogSizeMapping() Option {
	return WithSizeMapping(ceilToPowerOfTwo)
}

const (
	bitsize       = 32 << (^uint(0) >> 63)
	maxint        = int(1<<(bitsize-1) - 1)
	maxintHeadBit = 1 << (bitsize - 2)
)

// logarithmicRange iterates from ceiled to power of two min to max,
// calling cb on each iteration.
func logarithmicRange(min, max int, cb func(int)) {
	if min == 0 {
		min = 1
	}
	for n := ceilToPowerOfTwo(min); n <= max; n <<= 1 {
		cb(n)
	}
}

// identity is identity.
func identity(n int) int {
	return n
}

// ceilToPowerOfTwo returns the least power of two integer value greater than
// or equal to n.
func ceilToPowerOfTwo(n int) int {
	if n&maxintHeadBit != 0 && n > maxintHeadBit {
		panic("argument is too large")
	}
	if n <= 2 {
		return n
	}
	n--
	n = fillBits(n)
	n++
	return n
}

func fillBits(n int) int {
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	return n
}
