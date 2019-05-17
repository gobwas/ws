package compress

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"
	"io/ioutil"
	"reflect"
	"testing"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
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
			result: nil,
		},
		{
			label:  "simple",
			level:  6,
			seq:    [][]byte{[]byte("hello world!")},
			result: ws.MustCompileFrame(MustCompressFrame(ws.NewTextFrame([]byte("hello world!")), -1))[2:], // strip header
		},
		{
			label:  "small",
			level:  6,
			seq:    [][]byte{[]byte("hi")},
			result: ws.MustCompileFrame(MustCompressFrame(ws.NewTextFrame([]byte("hi")), -1))[2:],  // strip header
		},
		{
			label:  "multiple_writes",
			level:  6,
			seq:    [][]byte{[]byte("hello "), []byte("world!")},
			result: []byte{0xca, 0x48, 0xcd, 0xc9, 0xc9, 0x57, 0x0, 0x0, 0x2a, 0xcf, 0x2f, 0xca, 0x49, 0x51, 0x4, 0x0},
		},
		{
			label:      "fragmented_writes",
			level:      6,
			fragmented: true,
			seq:        [][]byte{[]byte("hello "), []byte("world!")},
			result:     []byte{0xca, 0x48, 0xcd, 0xc9, 0xc9, 0x57, 0x0, 0x0, 0x0, 0x0, 0xff, 0xff, 0x2a, 0xcf, 0x2f, 0xca, 0x49, 0x51, 0x4, 0x0, 0x0, 0x0, 0xff, 0xff},
		},
	} {
		t.Run(fmt.Sprintf("%s#%d", test.label, i), func(t *testing.T) {
			var err error
			buf := &bytes.Buffer{}
			cw := NewWriter(buf, test.level)
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
			connBuf := &bytes.Buffer{}
			for _, f := range test.seq {
				if err := ws.WriteFrame(connBuf, f); err != nil {
					t.Fatal(err)
				}
			}

			conn := &chopReader{
				src: bytes.NewReader(connBuf.Bytes()),
				sz:  test.chop,
			}

			compressedMessage := false
			mask := [4]byte{}
			masked := false

			bts := make([]byte, 1024)
			pos := 0
			startedContinuation := false
			for {
				header, err := ws.ReadHeader(conn)
				if err == io.EOF {
					break
				} else if err != nil {
					t.Errorf("cannot read header of frame: %v", err)
				}
				if !header.OpCode.IsData() {
					io.ReadFull(conn, make([]byte, header.Length))
					continue
				}

				if header.Rsv1() {
					compressedMessage = true
				}

				if startedContinuation && header.OpCode != ws.OpContinuation {
					if test.err != ws.ErrProtocolContinuationExpected {
						t.Errorf("Continuation frame expected")
					}

					return
				}
				startedContinuation = startedContinuation || !header.Fin

				var tmpBts []byte
				if len(bts) < pos+int(header.Length) {
					tmpBts = make([]byte, header.Length)
				} else {
					tmpBts = bts[pos:pos+int(header.Length)]
				}
				n, err := io.ReadFull(conn, tmpBts)
				if err != nil {
					t.Errorf("cannot read payload of frame: %v", err)
					break
				}

				copy(bts[pos:], tmpBts)
				pos += n

				if header.Masked {
					masked = true
					mask = header.Mask
				}

				if header.Fin {
					var payload []byte

					if compressedMessage {
						r := bytes.NewReader(bts[0:pos])
						compressedReader := NewReader(r, len(bts))
						payload, err = ioutil.ReadAll(compressedReader)
						if err != nil {
							t.Errorf("cannot read message: %v", err)
						}
					} else {
						payload = make([]byte, pos)
						copy(payload, bts[0:pos])
					}

					if masked {
						ws.Cipher(payload, mask, 0)
					}

					if test.err == nil && !bytes.Equal(payload, test.exp) {
						t.Errorf(
							"Read compressed from reader:\nact:\t%#x\nexp:\t%#x\nact:\t%s\nexp:\t%s\n",
							payload, test.exp, string(payload), string(test.exp),
						)
					}

					pos = 0
					compressedMessage = false
					startedContinuation = false
				}

				if header.OpCode == ws.OpClose {
					break
				}
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
		b.Run(fmt.Sprintf("message=%s;repeated=%d;comp=%v", bench.message, bench.repeated, bench.compressed), func(b *testing.B) {
			buf := &bytes.Buffer{}
			for r := bench.repeated; r >= 0; r-- {
				buf.WriteString(bench.message)
			}

			var (
				writer *wsutil.Writer
				cw Writer
				err error
			)

			if bench.compressed {
				cw = NewWriter(ioutil.Discard, flate.BestSpeed)
			} else {
				writer = wsutil.NewWriter(ioutil.Discard, ws.StateServerSide, ws.OpText)
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

type chopReader struct {
	src io.Reader
	sz  int
}

func (c chopReader) Read(p []byte) (n int, err error) {
	sz := c.sz
	if sz == 0 {
		sz = 1
	}
	if sz > len(p) {
		sz = len(p)
	}
	return c.src.Read(p[:sz])
}
