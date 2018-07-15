package wsutil

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"testing"
	"unicode/utf8"
)

func TestUTF8ReaderReadFull(t *testing.T) {
	for _, test := range []struct {
		hex   string
		err   bool
		valid bool
		n     int
	}{
		{
			hex:   "cebae1bdb9cf83cebcceb5eda080656469746564",
			err:   true,
			valid: false,
			n:     11,
		},
		{
			hex:   "cebae1bdb9cf83cebcceb5eda080656469746564",
			valid: false,
			err:   true,
			n:     11,
		},
		{
			hex:   "7f7f7fdf",
			valid: false,
			err:   false,
			n:     4,
		},
		{
			hex:   "dfbf",
			n:     2,
			valid: true,
			err:   false,
		},
	} {
		t.Run("", func(t *testing.T) {
			bts, err := hex.DecodeString(test.hex)
			if err != nil {
				t.Fatal(err)
			}

			src := bytes.NewReader(bts)
			r := NewUTF8Reader(src)

			p := make([]byte, src.Len())
			n, err := io.ReadFull(r, p)

			if err != nil && !utf8.Valid(bts[:n]) {
				// Should return only number of valid bytes read.
				t.Errorf("read n bytes is actually invalid utf8 sequence")
			}
			if n := r.Accepted(); err == nil && !utf8.Valid(bts[:n]) {
				// Should return only number of valid bytes read.
				t.Errorf("read n bytes is actually invalid utf8 sequence")
			}
			if test.err && err == nil {
				t.Errorf("expected read error; got nil")
			}
			if !test.err && err != nil {
				t.Errorf("unexpected read error: %s", err)
			}
			if n != test.n {
				t.Errorf("ReadFull() read %d; want %d", n, test.n)
			}
			if act, exp := r.Valid(), test.valid; act != exp {
				t.Errorf("Valid() = %v; want %v", act, exp)
			}
		})
	}
}

func TestUTF8Reader(t *testing.T) {
	for i, test := range []struct {
		label string

		data []byte
		// or
		hex string

		chop int

		err   bool
		valid bool
		at    int
	}{
		{
			data:  []byte("hello, world!"),
			valid: true,
			chop:  2,
		},
		{
			data:  []byte{0x7f, 0xf0, 0x00},
			valid: false,
			err:   true,
			at:    2,
			chop:  1,
		},
		{
			hex:   "48656c6c6f2dc2b540c39fc3b6c3a4c3bcc3a0c3a12d5554462d382121",
			valid: true,
			chop:  1,
		},
		{
			hex:   "cebae1bdb9cf83cebcceb5eda080656469746564",
			valid: false,
			err:   true,
			at:    12,
			chop:  1,
		},
	} {
		t.Run(fmt.Sprintf("%s#%d", test.label, i), func(t *testing.T) {
			data := test.data
			if h := test.hex; h != "" {
				var err error
				if data, err = hex.DecodeString(h); err != nil {
					t.Fatal(err)
				}
			}

			cr := &chopReader{
				src: bytes.NewReader(data),
				sz:  test.chop,
			}

			r := NewUTF8Reader(cr)

			bts := make([]byte, 2*len(data))

			var (
				i, n int
				err  error
			)
			for {
				n, err = r.Read(bts[i:])
				i += n
				if err != nil {
					if err == io.EOF {
						err = nil
					}
					bts = bts[:i]
					break
				}
			}
			if test.err && err == nil {
				t.Errorf("want error; got nil")
				return
			}
			if !test.err && err != nil {
				t.Errorf("unexpected error: %s", err)
				return
			}
			if test.err && err == ErrInvalidUTF8 && i != test.at {
				t.Errorf("received error at %d; want at %d", i, test.at)
				return
			}
			if act, exp := r.Valid(), test.valid; act != exp {
				t.Errorf("Valid() = %v; want %v", act, exp)
				return
			}
			if !test.err && !bytes.Equal(bts, data) {
				t.Errorf("bytes are not equal")
			}
		})
	}
}

func BenchmarkUTF8Reader(b *testing.B) {
	for i, bench := range []struct {
		label string
		data  []byte
		chop  int
		err   bool
	}{
		{
			data: bytes.Repeat([]byte("x"), 1024),
			chop: 128,
		},
		{
			data: append(
				bytes.Repeat([]byte("x"), 1024),
				append(
					[]byte{0x7f, 0xf0},
					bytes.Repeat([]byte("x"), 128)...,
				)...,
			),
			err:  true,
			chop: 7,
		},
	} {
		b.Run(fmt.Sprintf("%s#%d", bench.label, i), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				cr := &chopReader{
					src: bytes.NewReader(bench.data),
					sz:  bench.chop,
				}
				r := NewUTF8Reader(cr)
				_, err := ioutil.ReadAll(r)
				if !bench.err && err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
