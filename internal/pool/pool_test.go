package pool

import (
	"bufio"
	"bytes"
	"io"
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

func TestPoolReuse(t *testing.T) {
	p := New[*int](128, 1024)

	// An object put under an existing class comes back for any size that
	// rounds to that class.
	x := new(int)
	p.Put(x, 256)
	if y, n := p.Get(200); y != x || n != 256 {
		t.Fatalf(
			"Get(200) = %p, %d; want %p, 256",
			y, n, x,
		)
	}

	// An object put under a size that is not a class is dropped.
	p.Put(x, 300)
	if y, _ := p.Get(300); y != nil {
		t.Fatalf(
			"Get(300) returned an object put under a non-class size",
		)
	}
}

func TestGetBytes(t *testing.T) {
	for _, test := range []struct {
		n      int
		expCap int
	}{
		{
			n:      10,
			expCap: 10,
		},
		{
			n:      200,
			expCap: 256,
		},
		{
			n:      65536,
			expCap: 65536,
		},
		{
			n:      70000,
			expCap: 70000,
		},
	} {
		bp := GetBytes(test.n)
		p := *bp
		if len(p) != test.n {
			t.Errorf(
				"GetBytes(%d) len = %d; want %d",
				test.n, len(p), test.n,
			)
		}
		if cap(p) < test.expCap {
			t.Errorf(
				"GetBytes(%d) cap = %d; want at least %d",
				test.n, cap(p), test.expCap,
			)
		}
		PutBytes(bp)
	}
}

func TestReaderWriterReuse(t *testing.T) {
	var buf bytes.Buffer

	bw := GetWriter(&buf, 1000)
	if n := bw.Size(); n != 1024 {
		t.Fatalf(
			"GetWriter(1000) buffer size = %d; want 1024",
			n,
		)
	}
	bw.WriteString("hello")
	if err := bw.Flush(); err != nil {
		t.Fatal(err)
	}
	PutWriter(bw)

	br := GetReader(&buf, 1000)
	if n := br.Size(); n != 1024 {
		t.Fatalf(
			"GetReader(1000) buffer size = %d; want 1024",
			n,
		)
	}
	p, err := io.ReadAll(br)
	if err != nil || string(p) != "hello" {
		t.Fatalf(
			"read %q, %v; want %q, nil",
			p, err, "hello",
		)
	}
	PutReader(br)

	// A size out of range gives an exact, unpooled buffer.
	bw = GetWriter(io.Discard, 100)
	if n := bw.Size(); n != 100 {
		t.Fatalf(
			"GetWriter(100) buffer size = %d; want 100",
			n,
		)
	}
	PutWriter(bw)

	var _ *bufio.Reader = br
}
