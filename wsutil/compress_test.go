package wsutil

import (
	"bytes"
	"compress/flate"
	"fmt"
	"github.com/gobwas/ws"
	"io"
	"io/ioutil"
	"reflect"
	"testing"
)

func TestCompressWriter(t *testing.T) {
	for i, test := range []struct {
		label      string
		level      int
		fragmented bool
		seq        [][]byte
		result     []byte
	}{
		{
			label:  "empty",
			level:  6,
			seq:    [][]byte{nil},
			result: ws.MustCompileFrame(MustCompressFrame(ws.NewTextFrame(nil), flate.BestSpeed)),
		},
		{
			label:  "simple",
			level:  6,
			seq:    [][]byte{[]byte("hello world!")},
			result: ws.MustCompileFrame(MustCompressFrame(ws.NewTextFrame([]byte("hello world!")), -1)),
		},
		{
			label:  "small",
			level:  6,
			seq:    [][]byte{[]byte("hi")},
			result: ws.MustCompileFrame(MustCompressFrame(ws.NewTextFrame([]byte("hi")), -1)),
		},
		{
			label:  "multiple_writes",
			level:  6,
			seq:    [][]byte{[]byte("hello "), []byte("world!")},
			result: []byte{0xc1, 0x8, 0xca, 0x48, 0xcd, 0xc9, 0xc9, 0x57, 0x0, 0x0, 0xc1, 0x8, 0x2a, 0xcf, 0x2f, 0xca, 0x49, 0x51, 0x4, 0x0},
		},
		{
			label:      "fragmented_writes",
			level:      6,
			fragmented: true,
			seq:        [][]byte{[]byte("hello "), []byte("world!")},
			result:     []byte{0x41, 0xc, 0xca, 0x48, 0xcd, 0xc9, 0xc9, 0x57, 0x0, 0x0, 0x0, 0x0, 0xff, 0xff, 0x0, 0xc, 0x2a, 0xcf, 0x2f, 0xca, 0x49, 0x51, 0x4, 0x0, 0x0, 0x0, 0xff, 0xff},
		},
	} {
		t.Run(fmt.Sprintf("%s#%d", test.label, i), func(t *testing.T) {
			buf := &bytes.Buffer{}
			cw, err := WithCompressor(NewWriter(buf, ws.StateServerSide|ws.StateExtended, ws.OpText), test.level)
			if err != nil {
				t.Errorf("unexpected error: %s", err)
				return
			}
			for i, b := range test.seq {
				_, err = cw.Write(b)
				if err != nil {
					t.Errorf("cannot write data: %s", err)
					return
				}
				if test.fragmented && i <= len(test.seq)-1 {
					err = cw.FlushFragment()
				} else {
					err = cw.Flush()
				}
				if err != nil {
					t.Errorf("cannot flush data: %s", err)
					return
				}
			}

			if !reflect.DeepEqual(buf.Bytes(), test.result) {
				t.Errorf("write data is not equal:\n\tact:\t%#v\n\texp:\t%#v\n", buf.Bytes(), test.result)
				return
			}
		})
	}
}

func TestCompressReader(t *testing.T) {
	for _, test := range []struct {
		name string
		seq  []ws.Frame
		chop int
		exp  []byte
		err  error
	}{
		{
			name: "empty",
			seq:  []ws.Frame{},
			err:  io.EOF,
		},
		{
			name: "single",
			seq: []ws.Frame{
				ws.NewTextFrame([]byte("Привет, Мир!")),
			},
			exp: []byte("Привет, Мир!"),
		},
		{
			name: "single_compressed",
			seq: []ws.Frame{
				{
					Header: ws.Header{
						Fin:    true,
						Rsv:    0x04,
						OpCode: ws.OpText,
						Length: 14,
					},
					Payload: []byte{0xca, 0x48, 0xcd, 0xc9, 0xc9, 0x57, 0x28, 0xcf, 0x2f, 0xca, 0x49, 0x51, 0x04, 0x0},
				},
			},
			exp: []byte("hello world!"),
		},
		{
			name: "fragmented_compressed",
			seq: []ws.Frame{
				{
					Header: ws.Header{
						Fin:    false,
						Rsv:    0x04,
						OpCode: ws.OpText,
						Length: 7,
					},
					Payload: []byte{0xca, 0x48, 0xcd, 0xc9, 0xc9, 0x57, 0x28},
				},
				{
					Header: ws.Header{
						Fin:    true,
						Rsv:    0x00,
						OpCode: ws.OpContinuation,
						Length: 7,
					},
					Payload: []byte{0xcf, 0x2f, 0xca, 0x49, 0x51, 0x04, 0x0},
				},

				ws.NewTextFrame([]byte("Hello, Brave New World!")),
			},
			exp: []byte("hello world!"),
		},
		{
			name: "fragmented_compressed_multiple_flushed",
			seq: []ws.Frame{
				{
					Header: ws.Header{
						Fin:    false,
						Rsv:    0x04,
						OpCode: ws.OpText,
						Length: 12,
					},
					Payload: []byte{0xca, 0x48, 0xcd, 0xc9, 0xc9, 0x57, 0x0, 0x0, 0x0, 0x0, 0xff, 0xff},
				},
				{
					Header: ws.Header{
						Fin:    true,
						Rsv:    0x00,
						OpCode: ws.OpContinuation,
						Length: 8,
					},
					Payload: []byte{0x2a, 0xcf, 0x2f, 0xca, 0x49, 0x51, 0x4, 0x0},
				},

				ws.NewTextFrame([]byte("Hello, Brave New World!")),
			},
			exp: []byte("hello world!"),
		},
		{
			name: "fragmented_compressed_broken",
			seq: []ws.Frame{
				{
					Header: ws.Header{
						Fin:    false,
						Rsv:    0x04,
						OpCode: ws.OpText,
						Length: 7,
					},
					Payload: []byte{0xca, 0x48, 0xcd, 0xc9, 0xc9, 0x57, 0x28},
				},

				ws.NewTextFrame([]byte("Hello, Brave New World!")),
			},
			exp: []byte("hello world!"),
			err: ws.ErrProtocolContinuationExpected,
		},
		{
			name: "fragmented_compressed_control",
			seq: []ws.Frame{
				{
					Header: ws.Header{
						Fin:    false,
						Rsv:    0x04,
						OpCode: ws.OpText,
						Length: 7,
					},
					Payload: []byte{0xca, 0x48, 0xcd, 0xc9, 0xc9, 0x57, 0x28},
				},
				ws.NewFrame(ws.OpPing, true, nil),
				ws.NewFrame(ws.OpPing, true, nil),
				{
					Header: ws.Header{
						Fin:    true,
						Rsv:    0x00,
						OpCode: ws.OpContinuation,
						Length: 7,
					},
					Payload: []byte{0xcf, 0x2f, 0xca, 0x49, 0x51, 0x04, 0x0},
				},
				ws.NewFrame(ws.OpPing, true, nil),
				ws.NewFrame(ws.OpPing, true, []byte("ping info")),
			},
			exp: []byte("hello world!"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Prepare input.
			buf := &bytes.Buffer{}
			for _, f := range test.seq {
				if err := ws.WriteFrame(buf, f); err != nil {
					t.Fatal(err)
				}
			}

			conn := &chopReader{
				src: bytes.NewReader(buf.Bytes()),
				sz:  test.chop,
			}

			var bts []byte
			compressedReader, err := WithDecompressor(NewReader(conn, ws.StateClientSide|ws.StateExtended))
			if err != nil {
				t.Errorf("unexpected error; cannot create decompressed reader")
			}
			_, err = compressedReader.NextFrame()
			if err == nil {
				bts, err = ioutil.ReadAll(compressedReader)
			}

			if err != test.err {
				t.Errorf("unexpected error; got %v; want %v", err, test.err)
				return
			}
			if test.err == nil && !bytes.Equal(bts, test.exp) {
				t.Errorf(
					"Read compressed from reader:\nact:\t%#x\nexp:\t%#x\nact:\t%s\nexp:\t%s\n",
					bts, test.exp, string(bts), string(test.exp),
				)
			}
		})
	}
}

func BenchmarkCompressWriter(b *testing.B) {
	for _, bench := range []struct {
		compressed bool
		message    string
		repeated   int
	}{
		{
			message: "hello world",
		},
		{
			message:  "hello world",
			repeated: 1000,
		},
		{
			message:  "hello world",
			repeated: 10000,
		},
		{
			message: "hello world",
			compressed: true,
		},
		{
			message:  "hello world",
			repeated: 1000,
			compressed: true,
		},
		{
			message:  "hello world",
			repeated: 10000,
			compressed: true,
		},
	} {
		b.Run(fmt.Sprintf("message=%s;repeated=%d", bench.message, bench.repeated), func(b *testing.B) {
			buf := &bytes.Buffer{}
			for r := bench.repeated; r >= 0; r-- {
				buf.WriteString(bench.message)
			}
			writer := NewWriter(ioutil.Discard, ws.StateServerSide, ws.OpText)
			var (
				cw CompressWriter
				err error
			)

			if bench.compressed {
				cw, err = NewCompressWriter(writer, flate.DefaultCompression)
				if err != nil {
					b.Errorf("unexpected error: %s", err)
					return
				}
			}

			for i := 0; i < b.N; i++ {
				if cw != nil {
					_, err = cw.Write(buf.Bytes())
					if err == nil {
						err = cw.Flush()
					}
				} else {
					_, err = writer.Write(buf.Bytes())
					if err == nil {
						err = writer.Flush()
					}
				}
				if err != nil {
					b.Errorf("cannot write: %s", err)
					return
				}
			}
		})
	}
}
