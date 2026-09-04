package ws

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"reflect"
	"testing"
)

func TestReadFrameRejectsOversizedPayload(t *testing.T) {
	hdr := make([]byte, 10)
	hdr[0] = 0x82
	hdr[1] = 127
	binary.BigEndian.PutUint64(hdr[2:], uint64(MaxFramePayloadSize)+1)
	_, err := ReadFrame(bytes.NewReader(hdr))
	if err != ErrFrameTooLarge {
		t.Fatalf("ReadFrame() = %v, want ErrFrameTooLarge", err)
	}
}

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
	setup := func(header Header, n int) (rds []io.Reader) {
		bts := MustCompileFrame(Frame{Header: header})
		rds = make([]io.Reader, n)
		for i := 0; i < n; i++ {
			rds[i] = bytes.NewReader(bts)
		}

		return
	}

	for i, bench := range RWBenchCases {
		b.Run(fmt.Sprintf("%s#%d", bench.label, i), func(b *testing.B) {
			rds := setup(bench.header, b.N)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := ReadHeader(rds[i])
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("reused-buffer-%s#%d", bench.label, i), func(b *testing.B) {
			rds := setup(bench.header, b.N)
			bts := make([]byte, MaxHeaderSize)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := ReadHeaderBuffer(rds[i], bts)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
