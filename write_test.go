package ws

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"testing"
)

type writerFunc func([]byte) (int, error)

func (fn writerFunc) Write(p []byte) (int, error) { return fn(p) }

func TestWriteHeaderWriteErrors(t *testing.T) {
	failure := fmt.Errorf("writer failed")
	for _, test := range []struct {
		name     string
		failWith error
		want     error
	}{
		{
			name: "short",
			want: io.ErrShortWrite,
		},
		{
			name:     "error",
			failWith: failure,
			want:     failure,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := writerFunc(func(p []byte) (int, error) { return len(p) - 1, test.failWith })
			for _, write := range []func(io.Writer, Header) error{
				WriteHeader,
				func(w io.Writer, h Header) error { return WriteHeaderBuffer(w, h, make([]byte, MaxHeaderSize)) },
			} {
				h := Header{
					Fin:    true,
					OpCode: OpText,
					Length: 5,
				}
				if err := write(w, h); !errors.Is(err, test.want) {
					t.Fatalf("got %v, want %v", err, test.want)
				}
			}
		})
	}
}

func TestWriteFrameWriteErrors(t *testing.T) {
	failure := fmt.Errorf("writer failed")
	for _, test := range []struct {
		name     string
		retErrAt int
		failWith error
		want     error
	}{
		{
			name:     "header_short",
			retErrAt: 1,
			want:     io.ErrShortWrite,
		},
		{
			name:     "header_error",
			retErrAt: 1,
			failWith: failure,
			want:     failure,
		},
		{
			name:     "payload_short",
			retErrAt: 2,
			want:     io.ErrShortWrite,
		},
		{
			name:     "payload_error",
			retErrAt: 2,
			failWith: failure,
			want:     failure,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			w := writerFunc(func(p []byte) (int, error) {
				calls++
				if calls == test.retErrAt {
					return len(p) - 1, test.failWith
				}
				return len(p), nil
			})
			if err := WriteFrame(w, NewTextFrame([]byte("hello"))); !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
			if calls != test.retErrAt {
				t.Fatalf("got %d writes, want %d", calls, test.retErrAt)
			}
		})
	}
}

func TestWriteHeader(t *testing.T) {
	for i, test := range RWTestCases {
		t.Run(fmt.Sprintf("#%d", i), func(t *testing.T) {
			buf := &bytes.Buffer{}
			err := WriteHeader(buf, test.Header)
			if test.Err && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !test.Err && err != nil {
				t.Errorf("unexpected error: %s", err)
			}
			if test.Err {
				return
			}
			if bts := buf.Bytes(); !bytes.Equal(bts, test.Data) {
				t.Errorf("WriteHeader()\nwrote:\n\t%08b\nwant:\n\t%08b", bts, test.Data)
			}
		})
	}
}

func BenchmarkWriteHeader(b *testing.B) {
	for _, bench := range RWBenchCases {
		b.Run(bench.label, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				if err := WriteHeader(ioutil.Discard, bench.header); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run("reused-buffer-"+bench.label, func(b *testing.B) {
			bts := make([]byte, MaxHeaderSize)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				if err := WriteHeaderBuffer(ioutil.Discard, bench.header, bts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
