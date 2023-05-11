//go:build pool_sanitize
// +build pool_sanitize

package pbytes

import (
	"crypto/rand"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestPoolSanitize(t *testing.T) {
	for _, test := range []struct {
		len int
		cap int
	}{
		{0, 10},
		{1000, 1024},
		{syscall.Getpagesize(), syscall.Getpagesize() * 5},
	} {
		name := strconv.Itoa(test.cap)
		t.Run(name, func(t *testing.T) {
			p := New(0, test.cap)
			bts := p.Get(test.len, test.cap)
			if n := cap(bts); n < test.cap {
				t.Fatalf(
					"unexpected capacity of slice returned from Get(): %d; want at least %d",
					n, test.cap,
				)
			}
			if n := len(bts); n != test.len {
				t.Fatalf(
					"unexpected length of slice returned from Get(): %d; want %d",
					n, test.len,
				)
			}

			// Ensure that bts are readable and writable.
			n, err := rand.Read(bts[:test.cap])
			if err != nil {
				t.Fatal(err)
			}
			if n != test.cap {
				t.Fatalf("rand.Read() = %d; want %d", n, test.cap)
			}
			for _, b := range bts[:test.cap] {
				_ = b
			}

			// Return bts to pool. After this point all actions on bts are
			// prohibited.
			p.Put(bts)
			// p.Put(bts)
			bts = nil

			runtime.GC()
			time.Sleep(time.Millisecond)
			runtime.GC()
		})
	}
}
