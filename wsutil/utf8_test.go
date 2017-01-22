package wsutil

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"testing"
)

func TestUTF8ReaderReadFull(t *testing.T) {
	for i, test := range []struct {
		hex      string
		errRead  bool
		errClose bool
		n        int
		chop     int
	}{
		{
			hex:      "cebae1bdb9cf83cebcceb5eda080656469746564",
			errClose: true,
			errRead:  true,
			n:        12,
		},
		{
			hex:      "cebae1bdb9cf83cebcceb5eda080656469746564",
			errRead:  true,
			errClose: true,
			n:        12,
			chop:     1,
		},
		{
			hex:      "7f7f7fdf",
			errRead:  false,
			errClose: true,
			n:        4,
		},
		{
			hex: "dfbf",
			n:   2,
		},
	} {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			bts, err := hex.DecodeString(test.hex)
			if err != nil {
				t.Fatal(err)
			}

			chop := test.chop
			if chop <= 0 {
				chop = len(bts)
			}

			src := bytes.NewReader(bts)
			r := NewUTF8Reader(chopReader{src, chop})

			p := make([]byte, src.Len())
			n, err := io.ReadFull(r, p)

			if test.errRead && err == nil {
				t.Errorf("expected read error; got nil")
			}
			if !test.errRead && err != nil {
				t.Errorf("unexpected read error: %s", err)
			}
			if n != test.n {
				t.Errorf("ReadFull() read %d; want %d", n, test.n)
			}

			err = r.Close()
			if test.errClose && err == nil {
				t.Errorf("expected close error; got nil")
			}
			if !test.errClose && err != nil {
				t.Errorf("unexpected close error: %s", err)
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

		err bool
		at  int
	}{
		{
			data: []byte("hello, world!"),
			chop: 2,
		},
		{
			data: []byte{0x7f, 0xf0},
			err:  true,
			at:   2,
			chop: 1,
		},
		{
			data: []byte{0x7f, 0xf0},
			err:  true,
			at:   2,
			chop: 1,
		},
		{
			hex:  "48656c6c6f2dc2b540c39fc3b6c3a4c3bcc3a0c3a12d5554462d382121",
			chop: 1,
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
			if err == nil {
				err = r.Close()
			}
			if test.err && err == nil {
				t.Errorf("want error; got nil")
				return
			}
			if !test.err && err != nil {
				t.Errorf("unexpected error: %s", err)
				return
			}
			if test.err && err == ErrInvalidUtf8 && i != test.at {
				t.Errorf("received error at %d; want at %d", i, test.at)
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
