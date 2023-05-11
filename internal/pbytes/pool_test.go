//go:build !pool_sanitize
// +build !pool_sanitize

package pbytes

import (
	"crypto/rand"
	"reflect"
	"strconv"
	"testing"
	"unsafe"
)

func TestPoolGet(t *testing.T) {
	for _, test := range []struct {
		min      int
		max      int
		len      int
		cap      int
		exactCap int
	}{
		{
			min:      0,
			max:      64,
			len:      10,
			cap:      24,
			exactCap: 32,
		},
		{
			min:      0,
			max:      0,
			len:      10,
			cap:      24,
			exactCap: 24,
		},
	} {
		t.Run("", func(t *testing.T) {
			p := New(test.min, test.max)
			act := p.Get(test.len, test.cap)
			if n := len(act); n != test.len {
				t.Errorf(
					"Get(%d, _) retured %d-len slice; want %[1]d",
					test.len, n,
				)
			}
			if c := cap(act); c < test.cap {
				t.Errorf(
					"Get(_, %d) retured %d-cap slice; want at least %[1]d",
					test.cap, c,
				)
			}
			if c := cap(act); test.exactCap != 0 && c != test.exactCap {
				t.Errorf(
					"Get(_, %d) retured %d-cap slice; want exact %d",
					test.cap, c, test.exactCap,
				)
			}
		})
	}
}

func TestPoolPut(t *testing.T) {
	p := New(0, 32)

	miss := make([]byte, 5, 5)
	rand.Read(miss)
	p.Put(miss) // Should not reuse.

	hit := make([]byte, 8, 8)
	rand.Read(hit)
	p.Put(hit) // Should reuse.

	b := p.GetLen(5)
	if data(b) == data(miss) {
		t.Fatalf("unexpected reuse")
	}
	if data(b) != data(hit) {
		t.Fatalf("want reuse")
	}
}

func data(p []byte) uintptr {
	hdr := (*reflect.SliceHeader)(unsafe.Pointer(&p))
	return hdr.Data
}

func BenchmarkPool(b *testing.B) {
	for _, size := range []int{
		1 << 4,
		1 << 5,
		1 << 6,
		1 << 7,
		1 << 8,
		1 << 9,
	} {
		b.Run(strconv.Itoa(size)+"(pool)", func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					p := GetLen(size)
					Put(p)
				}
			})
		})
		b.Run(strconv.Itoa(size)+"(make)", func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					_ = make([]byte, size)
				}
			})
		})
	}
}
