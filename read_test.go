package ws

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"testing"
)

func TestReadHeader(t *testing.T) {
	for i, test := range append([]RWCase{
		{
			Data: bits("0000 0000 0 1111111 10000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000"),
			//                              _______________________________________________________________________
			//                                                                 |
			//                                                            Length value
			Err: true,
		},
	}, RWCases...) {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			r := bytes.NewReader(test.Data)
			h, err := ReadHeader(r)
			if test.Err && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !test.Err && err != nil {
				t.Errorf("unexpected error: %s", err)
			}
			if test.Err {
				return
			}
			if !reflect.DeepEqual(h, test.Header) {
				t.Errorf("ReadHeader()\nread:\n\t%#v\nwant:\n\t%#v", h, test.Header)
			}
		})
	}
}

func BenchmarkReadHeader(b *testing.B) {
	for i, bench := range []struct {
		label  string
		header Header
	}{
		{"t", Header{OpCode: OpText, Fin: true}},
		{"t-m", Header{OpCode: OpText, Fin: true, Mask: NewMask()}},
		{"t-m-u16", Header{OpCode: OpText, Fin: true, Length: len16, Mask: NewMask()}},
		{"t-m-u64", Header{OpCode: OpText, Fin: true, Length: len64, Mask: NewMask()}},
	} {
		b.Run(fmt.Sprintf("%s#%d", bench.label, i), func(b *testing.B) {
			bts := MustCompileFrame(Frame{Header: bench.header})
			rds := make([]io.Reader, b.N)
			for i := 0; i < b.N; i++ {
				rds[i] = bytes.NewReader(bts)
			}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := ReadHeader(rds[i])
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
