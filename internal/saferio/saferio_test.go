package saferio

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"testing/iotest"
)

func TestReadN(t *testing.T) {
	const lim = 8
	for _, test := range []struct {
		name  string
		lim   int64
		n     int64
		avail int64 // Bytes available in the reader.
		chunk bool  // Reader returns one byte per Read().
		err   error
	}{
		{
			name:  "zero n",
			lim:   lim,
			n:     0,
			avail: 0,
		},
		{
			name:  "zero n with data",
			lim:   lim,
			n:     0,
			avail: 5,
		},
		{
			name:  "one byte",
			lim:   lim,
			n:     1,
			avail: 1,
		},
		{
			name:  "below lim",
			lim:   lim,
			n:     lim - 1,
			avail: lim - 1,
		},
		{
			name:  "exactly lim",
			lim:   lim,
			n:     lim,
			avail: lim,
		},
		{
			name:  "above lim",
			lim:   lim,
			n:     lim + 1,
			avail: lim + 1,
		},
		{
			name:  "twice lim",
			lim:   lim,
			n:     2 * lim,
			avail: 2 * lim,
		},
		{
			name:  "twice lim plus one",
			lim:   lim,
			n:     2*lim + 1,
			avail: 2*lim + 1,
		},
		{
			name:  "many lim",
			lim:   lim,
			n:     5*lim + 3,
			avail: 5*lim + 3,
		},
		{
			name:  "extra data below lim",
			lim:   lim,
			n:     3,
			avail: 4,
		},
		{
			name:  "extra data above lim",
			lim:   lim,
			n:     lim + 1,
			avail: lim + 2,
		},
		{
			name:  "chunked below lim",
			lim:   lim,
			n:     lim - 1,
			avail: lim - 1,
			chunk: true,
		},
		{
			name:  "chunked above lim",
			lim:   lim,
			n:     5*lim + 3,
			avail: 5*lim + 3,
			chunk: true,
		},

		{
			name:  "no data",
			lim:   lim,
			n:     1,
			avail: 0,
			err:   io.EOF,
		},
		{
			name:  "no data above lim",
			lim:   lim,
			n:     lim + 1,
			avail: 0,
			err:   io.EOF,
		},
		{
			name:  "short by one below lim",
			lim:   lim,
			n:     lim - 1,
			avail: lim - 2,
			err:   io.ErrUnexpectedEOF,
		},
		{
			name:  "short by one at lim",
			lim:   lim,
			n:     lim,
			avail: lim - 1,
			err:   io.ErrUnexpectedEOF,
		},
		{
			name:  "short by one above lim",
			lim:   lim,
			n:     lim + 1,
			avail: lim,
			err:   io.ErrUnexpectedEOF,
		},
		{
			name:  "short at growth",
			lim:   lim,
			n:     5*lim + 3,
			avail: 2 * lim,
			err:   io.ErrUnexpectedEOF,
		},
		{
			name:  "short chunked",
			lim:   lim,
			n:     5*lim + 3,
			avail: 3*lim + 1,
			chunk: true,
			err:   io.ErrUnexpectedEOF,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := make([]byte, test.avail)
			for i := range data {
				data[i] = byte(i + 1)
			}
			src := bytes.NewReader(data)
			r := io.Reader(src)
			if test.chunk {
				r = iotest.OneByteReader(r)
			}

			p, err := ReadN(r, test.lim, test.n)
			if err != test.err {
				t.Fatalf(
					"unexpected error: %v; want %v",
					err, test.err,
				)
			}
			want := test.n
			if test.avail < want {
				want = test.avail
			}
			if !bytes.Equal(p, data[:want]) {
				t.Fatalf(
					"unexpected bytes: %v; want %v",
					p, data[:want],
				)
			}
			if err == nil && int64(cap(p)) != test.n {
				t.Fatalf(
					"capacity is %d; want exactly %d",
					cap(p), test.n,
				)
			}
			if rest := int64(src.Len()); rest != test.avail-want {
				t.Fatalf(
					"%d bytes left unread; want %d",
					rest, test.avail-want,
				)
			}
		})
	}
}

func TestReadNAllocation(t *testing.T) {
	// The buffer must not be allocated on trust: with no data the allocation
	// is bounded by lim however big n is.
	for _, n := range []int64{9, 1 << 20, 1 << 40, 1<<63 - 1} {
		p, err := ReadN(bytes.NewReader(nil), 8, n)
		if err != io.EOF {
			t.Fatalf(
				"n=%d: unexpected error: %v; want %v",
				n, err, io.EOF,
			)
		}
		if cap(p) > 8 {
			t.Fatalf(
				"n=%d: allocated %d bytes; want at most 8",
				n, cap(p),
			)
		}
	}
	// Below lim the payload is allocated at once and exactly.
	data := make([]byte, 8)
	src := bytes.NewReader(data)
	allocs := testing.AllocsPerRun(100, func() {
		src.Reset(data)
		ReadN(src, 8, 8)
	})
	if allocs != 1 {
		t.Fatalf(
			"ReadN() made %.0f allocations; want 1",
			allocs,
		)
	}
}

func TestReadNError(t *testing.T) {
	// Reader errors other than EOF are returned as is, with what was read.
	errTest := errors.New("test error")
	r := io.MultiReader(bytes.NewReader([]byte{1, 2, 3}), iotest.ErrReader(errTest))
	p, err := ReadN(r, 2, 10)
	if err != errTest {
		t.Fatalf(
			"unexpected error: %v; want %v",
			err, errTest,
		)
	}
	if !bytes.Equal(p, []byte{1, 2, 3}) {
		t.Fatalf(
			"unexpected bytes: %v",
			p,
		)
	}
}

func TestReadNPanicsOnLimit(t *testing.T) {
	for _, lim := range []int64{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf(
						"ReadN(lim=%d) did not panic",
						lim,
					)
				}
			}()
			ReadN(bytes.NewReader(make([]byte, 5)), lim, 5)
		}()
	}
}
