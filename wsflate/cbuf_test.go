package wsflate

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"testing"
)

func TestSuffixReader(t *testing.T) {
	for chunk := 1; chunk < 100; chunk++ {
		var (
			data = []byte("hello, flate!")
			name = fmt.Sprintf("chunk-%d", chunk)
		)
		t.Run(name, func(t *testing.T) {
			r := suffixedReader{
				r: bytes.NewReader(data),
				suffix: [9]byte{
					1, 2, 3,
					4, 5, 6,
					7, 8, 9,
				},
			}
			var (
				act = make([]byte, 0, len(data)+len(r.suffix))
				p   = make([]byte, chunk)
			)
			for len(act) < cap(act) {
				n, err := r.Read(p)
				act = append(act, p[:n]...)
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("unexpected Read() error: %v", err)
				}
			}
			exp := append(data, r.suffix[:]...)
			if !bytes.Equal(act, exp) {
				t.Fatalf("unexpected bytes read: %#q; want %#q", act, exp)
			}
		})
	}
}

func TestCBuf(t *testing.T) {
	for _, test := range []struct {
		name    string
		stream  [][]byte
		expBody []byte
		expTail []byte
	}{
		{
			stream: [][]byte{
				{1}, {2}, {3}, {4},
			},
			expTail: []byte{1, 2, 3, 4},
		},
		{
			stream: [][]byte{
				{1, 2}, {3, 4},
			},
			expTail: []byte{1, 2, 3, 4},
		},
		{
			stream: [][]byte{
				{1, 2, 3}, {4, 5, 6},
			},
			expBody: []byte{1, 2},
			expTail: []byte{3, 4, 5, 6},
		},
		{
			stream: [][]byte{
				{1, 2, 3, 4}, {5, 6, 7, 8},
			},
			expBody: []byte{1, 2, 3, 4},
			expTail: []byte{5, 6, 7, 8},
		},
		{
			stream: [][]byte{
				{1, 2, 3, 4, 5}, {6, 7, 8, 9, 10},
			},
			expBody: []byte{1, 2, 3, 4, 5, 6},
			expTail: []byte{7, 8, 9, 10},
		},
		{
			stream: [][]byte{
				{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			},
			expBody: []byte{1, 2, 3, 4, 5, 6},
			expTail: []byte{7, 8, 9, 10},
		},
		{
			name: "xxx",
			stream: [][]byte{
				{1, 2, 3, 4, 5}, {6},
			},
			expBody: []byte{1, 2},
			expTail: []byte{3, 4, 5, 6},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := &cbuf{
				dst: &buf,
			}
			for _, bts := range test.stream {
				n, err := w.Write(bts)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if act, exp := n, len(bts); act != exp {
					t.Fatalf(
						"unexpected number of bytes written: %d; want %d",
						act, exp,
					)
				}
			}
			if act, exp := w.buf[:], test.expTail; !bytes.Equal(act, exp) {
				t.Errorf(
					"unexpected tail: %v; want %v",
					act, exp,
				)
			}
			if act, exp := buf.Bytes(), test.expBody; !bytes.Equal(act, exp) {
				t.Errorf(
					"unexpected body: %v; want %v",
					act, exp,
				)
			}
		})
	}
}

func BenchmarkCBuf(b *testing.B) {
	for _, test := range []struct {
		name  string
		chunk []byte
	}{
		{
			chunk: []byte{1, 2, 3, 4, 5},
		},
		{
			chunk: []byte{1, 2, 3, 4},
		},
	} {
		b.Run(test.name, func(b *testing.B) {
			w := &cbuf{
				dst: ioutil.Discard,
			}
			for i := 0; i < b.N; i++ {
				w.Write(test.chunk)
			}
		})
	}
}
