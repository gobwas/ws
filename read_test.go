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
				t.Errorf(
					"expected error, got nil",
				)
			}
			if !test.Err && err != nil {
				t.Errorf(
					"unexpected error: %s",
					err,
				)
			}
			if test.Err {
				return
			}
			if !reflect.DeepEqual(h, test.Header) {
				t.Errorf(
					"ReadHeader()\nread:\n\t%#v\nwant:\n\t%#v",
					h, test.Header,
				)
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

func TestReadFrameDeclaredLength(t *testing.T) {
	// Peer-declared payload length must not be trusted for allocation: with
	// no payload bytes available ReadFrame() must fail without allocating
	// more than payloadPreallocLimit.
	for _, length := range []int64{
		payloadPreallocLimit + 1,
		1 << 34, // 16 GiB
		1<<63 - 1,
	} {
		t.Run(fmt.Sprintf("len=%d", length), func(t *testing.T) {
			var buf bytes.Buffer
			err := WriteHeader(&buf, Header{
				Fin:    true,
				OpCode: OpBinary,
				Length: length,
			})
			if err != nil {
				t.Fatalf(
					"unexpected WriteHeader() error: %v",
					err,
				)
			}
			f, err := ReadFrame(&buf)
			if err != io.EOF {
				t.Fatalf(
					"unexpected ReadFrame() error: %v; want %v",
					err, io.EOF,
				)
			}
			if n := int64(cap(f.Payload)); n > payloadPreallocLimit {
				t.Fatalf(
					"ReadFrame() allocated %d bytes; want at most %d",
					n, payloadPreallocLimit,
				)
			}
		})
	}
}

func TestReadFramePayloadGrowth(t *testing.T) {
	lim := int(payloadPreallocLimit)
	for _, size := range []int{
		0,
		1,
		lim - 1,
		lim,
		lim + 1,
		3*lim + 7,
	} {
		t.Run(fmt.Sprintf("size=%d", size), func(t *testing.T) {
			data := make([]byte, size)
			for i := range data {
				data[i] = byte(i)
			}
			raw := MustCompileFrame(NewBinaryFrame(data))

			f, err := ReadFrame(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf(
					"unexpected ReadFrame() error: %v",
					err,
				)
			}
			if !bytes.Equal(f.Payload, data) {
				t.Fatalf(
					"unexpected payload: %d bytes; want %d",
					len(f.Payload), len(data),
				)
			}
			if size > 0 && cap(f.Payload) != size {
				t.Fatalf(
					"payload capacity is %d; want exactly %d",
					cap(f.Payload), size,
				)
			}

			// Truncated payload must be reported as unexpected EOF, as before.
			// (Cutting the only byte of a 1-byte payload is a clean EOF.)
			if size > 1 {
				_, err = ReadFrame(bytes.NewReader(raw[:len(raw)-1]))
				if err != io.ErrUnexpectedEOF {
					t.Fatalf(
						"unexpected truncated ReadFrame() error: %v; want %v",
						err, io.ErrUnexpectedEOF,
					)
				}
			}
		})
	}
}
