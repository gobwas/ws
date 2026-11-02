package ws

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

func TestCipher(t *testing.T) {
	type test struct {
		name   string
		in     []byte
		mask   [4]byte
		offset int
	}
	cases := []test{
		{
			name: "simple",
			in:   []byte("Hello, XOR!"),
			mask: [4]byte{1, 2, 3, 4},
		},
		{
			name: "simple",
			in:   []byte("Hello, XOR!"),
			mask: [4]byte{255, 255, 255, 255},
		},
	}
	for offset := range 4 {
		for tail := range 8 {
			for b64 := range 3 {
				var (
					ln = remain[offset]
					rn = tail
					n  = b64*8 + ln + rn
				)

				p := make([]byte, n)
				rand.Read(p)

				var m [4]byte
				rand.Read(m[:])

				cases = append(cases, test{
					in:     p,
					mask:   m,
					offset: offset,
				})
			}
		}
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			// naive implementation of xor-cipher
			exp := cipherNaive(test.in, test.mask, test.offset)

			res := bytes.Clone(test.in)
			Cipher(res, test.mask, test.offset)

			if !reflect.DeepEqual(res, exp) {
				t.Errorf("Cipher(%v, %v):\nact:\t%v\nexp:\t%v\n", test.in, test.mask, res, exp)
			}
		})
	}
}

func TestCipherChops(t *testing.T) {
	for n := 2; n <= 1024; n <<= 1 {
		t.Run(fmt.Sprintf("%d", n), func(t *testing.T) {
			p := make([]byte, n)
			b := make([]byte, n)
			var m [4]byte

			_, err := rand.Read(p)
			if err != nil {
				t.Fatal(err)
			}
			_, err = rand.Read(m[:])
			if err != nil {
				t.Fatal(err)
			}

			exp := cipherNaive(p, m, 0)

			for i := 1; i <= n; i <<= 1 {
				copy(b, p)
				s := n / i

				for j := s; j <= n; j += s {
					l, r := j-s, j
					Cipher(b[l:r], m, l)
					if !reflect.DeepEqual(b[l:r], exp[l:r]) {
						t.Fatalf("unexpected Cipher([%d:%d]) = %x; want %x", l, r, b[l:r], exp[l:r])
					}
				}
			}

			l := 0
			copy(b, p)
			for l < n {
				r := rand.Intn(n-l) + l + 1
				Cipher(b[l:r], m, l)
				if !reflect.DeepEqual(b[l:r], exp[l:r]) {
					t.Fatalf("unexpected Cipher([%d:%d]):\nact:\t%v\nexp:\t%v\nact:\t%#x\nexp:\t%#x\n\n", l, r, b[l:r], exp[l:r], b[l:r], exp[l:r])
				}
				l = r
			}
		})
	}
}

func cipherNaive(p []byte, m [4]byte, pos int) []byte {
	r := bytes.Clone(p)
	cipherNaiveNoCp(r, m, pos)
	return r
}

func cipherNaiveNoCp(p []byte, m [4]byte, pos int) []byte {
	for i := range p {
		p[i] ^= m[(pos+i)%4]
	}
	return p
}

func BenchmarkCipher(b *testing.B) {
	for _, bench := range []struct {
		size   int
		offset int
	}{
		{
			size:   7,
			offset: 1,
		},
		{
			size: 125,
		},
		{
			size: 1024,
		},
		{
			size: 4096,
		},
		{
			size:   4100,
			offset: 4,
		},
		{
			size:   4099,
			offset: 3,
		},
		{
			size:   (1 << 15) + 7,
			offset: 49,
		},
	} {
		bts := make([]byte, bench.size)
		_, err := rand.Read(bts)
		if err != nil {
			b.Fatal(err)
		}

		var mask [4]byte
		_, err = rand.Read(mask[:])
		if err != nil {
			b.Fatal(err)
		}

		b.Run(fmt.Sprintf("naive_bytes=%d;offset=%d", bench.size, bench.offset), func(b *testing.B) {
			var sink int64
			b.SetBytes(int64(bench.size))
			for b.Loop() {
				r := cipherNaiveNoCp(bts, mask, bench.offset)
				sink += int64(len(r))
			}
			sinkValue(sink)
		})
		b.Run(fmt.Sprintf("bytes=%d;offset=%d", bench.size, bench.offset), func(b *testing.B) {
			var sink int64
			b.SetBytes(int64(bench.size))
			for b.Loop() {
				Cipher(bts, mask, bench.offset)
				sink += int64(len(bts))
			}
			sinkValue(sink)
		})
	}
}

// sinkValue makes variable used and prevents dead code elimination.
func sinkValue(v int64) {
	if r := rand.Float32(); r > 2 {
		panic(fmt.Sprintf("impossible %g: %v", r, v))
	}
}

// TestCipherLarge checks Cipher() against the byte-by-byte cipher on payloads
// long enough to go through the vector kernel several times, at every mask
// offset and with every tail length.
func TestCipherLarge(t *testing.T) {
	data := make([]byte, 4096+64)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{64, 65, 127, 128, 1000, 4096, 4097, 4099, 4159} {
		for offset := range 4 {
			exp := cipherNaive(data[:n], mask, offset)
			act := bytes.Clone(data[:n])
			Cipher(act, mask, offset)
			if !bytes.Equal(act, exp) {
				t.Fatalf(
					"Cipher(%d bytes, offset %d) differs from the naive cipher",
					n, offset,
				)
			}
		}
	}
}
