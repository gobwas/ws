package pool

import (
	"bufio"
	"fmt"
	"testing"
)

func TestCeilToPowerOfTwo(t *testing.T) {
	for _, test := range []struct {
		n   int
		exp int
	}{
		{
			n:   0,
			exp: 0,
		},
		{
			n:   1,
			exp: 1,
		},
		{
			n:   2,
			exp: 2,
		},
		{
			n:   3,
			exp: 4,
		},
		{
			n:   4,
			exp: 4,
		},
		{
			n:   5,
			exp: 8,
		},
		{
			n:   127,
			exp: 128,
		},
		{
			n:   128,
			exp: 128,
		},
		{
			n:   129,
			exp: 256,
		},
		{
			n:   65535,
			exp: 65536,
		},
	} {
		if act := ceilToPowerOfTwo(test.n); act != test.exp {
			t.Errorf(
				"ceilToPowerOfTwo(%d) = %d; want %d",
				test.n, act, test.exp,
			)
		}
	}
}

func TestPoolGet(t *testing.T) {
	p := New[*int](128, 1024)
	for _, test := range []struct {
		name string
		size int
		exp  int
	}{
		{
			name: "below range",
			size: 10,
			exp:  10,
		},
		{
			name: "min",
			size: 128,
			exp:  128,
		},
		{
			name: "rounded up",
			size: 129,
			exp:  256,
		},
		{
			name: "max",
			size: 1024,
			exp:  1024,
		},
		{
			name: "above range",
			size: 1025,
			exp:  1025,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			x, n := p.Get(test.size)
			if x != nil {
				t.Fatalf(
					"Get(%d) returned an object from an empty pool",
					test.size,
				)
			}
			if n != test.exp {
				t.Fatalf(
					"Get(%d) size = %d; want %d",
					test.size, n, test.exp,
				)
			}
		})
	}
}

func testGetSize[T any](
	t *testing.T,
	get func(int) T,
	put func(T),
	sizeOf func(T) int,
) {
	// Sizes between the two pools' lower bounds (128 and 256) are left out
	// so that one table serves all kinds.
	for _, test := range []struct {
		n   int
		exp int
	}{
		{
			// Below both pools, above the 16-byte minimum of bufio.Reader.
			n:   64,
			exp: 64,
		},
		{
			n:   1000,
			exp: 1024,
		},
		{
			n:   4096,
			exp: 4096,
		},
		{
			n:   4097,
			exp: 8192,
		},
		{
			n:   65536,
			exp: 65536,
		},
		{
			n:   70000,
			exp: 70000,
		},
	} {
		name := fmt.Sprintf(
			"%d->%d",
			test.n, test.exp,
		)
		t.Run(name, func(t *testing.T) {
			obj := get(test.n)
			defer put(obj)
			if act := sizeOf(obj); act != test.exp {
				t.Errorf(
					"size for %d = %d; want %d",
					test.n, act, test.exp,
				)
			}
		})
	}
}

func TestGetBytes(t *testing.T) {
	testGetSize(t, GetBytes, PutBytes, func(p *[]byte) int {
		// The length is always the request; the capacity is what the pool gave.
		return cap(*p)
	})
}
func TestGetReader(t *testing.T) {
	testGetSize(t, GetReader, PutReader, func(br *bufio.Reader) int {
		return br.Size()
	})
}
func TestGetWriter(t *testing.T) {
	testGetSize(t, GetWriter, PutWriter, func(bw *bufio.Writer) int {
		return bw.Size()
	})
}
