package ws

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"testing"
)

func TestReadHeader(t *testing.T) {
	for i, test := range append([]RWTestCase{
		{
			Data: bits("0000 0000 0 1111111 10000000 00000000 00000000 00000000 00000000 00000000 00000000 00000000"),
			//                              _______________________________________________________________________
			//                                                                 |
			//                                                            Length value
			Err: true,
		},
	}, RWTestCases...) {
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
	for i, bench := range RWBenchCases {
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
