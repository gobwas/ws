package ws

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"testing"
)

type writeFunc func([]byte) (int, error)

func (fn writeFunc) Write(p []byte) (int, error) { return fn(p) }

func TestWriteHeaderWriteErrors(t *testing.T) {
	failure := fmt.Errorf("writer failed")
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{"short", nil, io.ErrShortWrite},
		{"error", failure, failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := writeFunc(func(p []byte) (int, error) { return len(p) - 1, test.err })
			for _, write := range []func(io.Writer, Header) error{
				WriteHeader,
				func(w io.Writer, h Header) error { return WriteHeaderBuffer(w, h, make([]byte, MaxHeaderSize)) },
			} {
				if err := write(w, Header{Fin: true, OpCode: OpText, Length: 5}); err != test.want {
					t.Fatalf("got %v, want %v", err, test.want)
				}
			}
		})
	}
}

func TestWriteFrameWriteErrors(t *testing.T) {
	failure := fmt.Errorf("writer failed")
	for _, test := range []struct {
		name string
		call int
		err  error
		want error
	}{
		{"header_short", 1, nil, io.ErrShortWrite},
		{"header_error", 1, failure, failure},
		{"payload_short", 2, nil, io.ErrShortWrite},
		{"payload_error", 2, failure, failure},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			w := writeFunc(func(p []byte) (int, error) {
				calls++
				if calls == test.call {
					return len(p) - 1, test.err
				}
				return len(p), nil
			})
			if err := WriteFrame(w, NewTextFrame([]byte("hello"))); err != test.want {
				t.Fatalf("got %v, want %v", err, test.want)
			}
			if calls != test.call {
				t.Fatalf("got %d writes, want %d", calls, test.call)
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
